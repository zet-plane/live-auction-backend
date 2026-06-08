# Local Authority Failover Design

## Background

The online environment already connects to managed MySQL and Redis/Valkey HA endpoints. Those cloud services provide primary/standby switching inside the provider, but the backend still needs a plan for the case where backend pods cannot reach the cloud endpoints at all.

The goal is not to make dependency failures look nicer through error codes. The goal is to keep the backend process serving traffic, keep auction authority explicit, and move into a recoverable local-authority mode when cloud Redis or cloud MySQL is unreachable.

Current code has useful foundations:

- Multi-pod backend support.
- Redis Lua as the auction hot-state authority.
- Redis Stream bid-log handoff.
- Cron leases.
- WebSocket reconnect snapshots.
- `/livez`, `/readyz`, and `/health` endpoints.

Current code also has conflicts with this availability goal:

- Redis client creation pings Redis at startup.
- Server startup exits when Redis connection fails.
- `/readyz` fails on Redis errors.
- The backend deployment has a blocking `wait-for-redis` initContainer.
- Business paths return internal errors for Redis/MySQL failures instead of switching service mode.

## Goals

1. Keep backend pods alive when cloud Redis or cloud MySQL is unreachable.
2. Use the always-running local Redis as the temporary auction real-time authority when cloud Redis is unreachable and MySQL is still available.
3. Rebuild local Redis auction state from MySQL in controlled batches while preventing cache breakdown, penetration, and avalanche.
4. Continue accepting bids for at most 10 seconds when MySQL is unreachable but Redis is still available.
5. Pause settlement, order creation, and deposit refund side effects until MySQL backlog is persisted and verified.
6. Prevent split-brain across multiple backend pods by using one shared local control-plane state.
7. Emit internal health, metrics, logs, and alerts for local Redis mode, MySQL buffering, protected auctions, rebuild failures, and stale control-plane state.

## Non-Goals

- Do not build a custom in-memory auction database inside backend pods.
- Do not use local memory as the authority across pods.
- Do not accept writes to cloud Redis and local Redis for the same auction item at the same time.
- Do not claim Redis rebuild from MySQL is lossless when cloud Redis may have accepted bids that were not yet persisted to MySQL.
- Do not make the shared state file carry high-frequency business data.
- Do not solve multi-node Kubernetes shared control-plane storage in the first version. This design targets the current single-node k3s environment.

## Operating Modes

The backend uses these internal modes:

| Mode | Meaning | Write Behavior |
| --- | --- | --- |
| `normal_cloud` | Cloud Redis and cloud MySQL are reachable. | Auction writes use cloud Redis. Durable writes use cloud MySQL. |
| `local_redis_switching` | Cloud Redis is unavailable and local Redis is being rebuilt from MySQL. | Item writes are allowed only after the item becomes `ready` on local Redis. |
| `local_redis_active` | Local Redis is the active real-time authority. | Ready item writes use local Redis. |
| `mysql_buffering` | MySQL is unavailable but Redis is available. | Redis can accept bids for at most 10 seconds. Settlement side effects pause. |
| `auction_protected` | A global or item-level safety condition blocks bids. | Affected items reject new bids until recovery completes. |

Mode changes are represented by a monotonically increasing `epoch`. Any request or Lua script that writes auction state must use the current epoch.

## Shared Control Plane

Redis must not be the control plane for deciding Redis authority because Redis itself can fail. In the current single-node k3s deployment, the control plane is a host-local shared file mounted into every backend pod.

Host path:

```text
/var/lib/live-auction/availability/state.json
```

Container path:

```text
/availability/state.json
```

Example content:

```json
{
  "version": 1,
  "mode": "normal_cloud",
  "epoch": 12,
  "active_redis": "cloud",
  "mysql_state": "healthy",
  "updated_at_unix_ms": 1710000000000,
  "reason": "probe_ok"
}
```

Fields:

| Field | Purpose |
| --- | --- |
| `version` | Schema version for compatibility checks. |
| `mode` | Current global operating mode. |
| `epoch` | Monotonic authority epoch. |
| `active_redis` | `cloud` or `local`. |
| `mysql_state` | `healthy`, `down`, `buffering`, or `recovering`. |
| `updated_at_unix_ms` | Last successful control-plane write time. |
| `reason` | Safe internal reason for mode changes. No credentials or DSNs. |

All backend pods watch this file with fsnotify or polling and keep an in-memory snapshot. Business hot paths read only the in-memory snapshot, not the file.

If the file is missing, malformed, stale, has an unsupported version, or has an epoch lower than the last seen epoch, pods fail closed:

- Do not write cloud Redis.
- Do not write local Redis.
- Do not accept new bids for affected items.
- Continue safe read paths where possible.
- Report `control_plane_stale` or `control_plane_invalid` internally.

## Control-Plane Writer

Only one controller may write the state file at a time. The first implementation can be an in-process controller in every backend pod guarded by a host-level file lock. A later version can move this into a dedicated sidecar or controller process.

Write protocol:

1. Acquire an exclusive file lock.
2. Re-read the current file and validate epoch.
3. Decide the next state.
4. Write a temporary file in the same directory.
5. `fsync` the temporary file.
6. Atomically rename the temporary file over `state.json`.
7. `fsync` the directory when supported.
8. Release the lock.

This prevents torn writes and keeps all pods on one shared authority decision.

## Redis Failure Path

Normal mode uses cloud Redis as auction real-time authority. When cloud Redis is unreachable past the configured protection threshold, the controller switches authority to local Redis.

Local Redis is always running, but the first version does not mirror every cloud Redis hot-state mutation into local Redis. It is a warm standby process with rebuild-on-failover semantics, not a continuously synchronized Redis replica.

Flow:

1. Probe detects cloud Redis unavailable.
2. Controller writes `mode=local_redis_switching`, `active_redis=local`, and increments `epoch`.
3. All pods observe the new epoch and stop writing cloud Redis.
4. A rebuild worker scans MySQL for active auction and room data.
5. Local Redis is rebuilt in bounded batches.
6. Each item transitions independently from `rebuilding` to `ready` or `protected`.
7. Once enough active items are rebuilt, the controller may write `mode=local_redis_active`.

The system must not accept writes to both Redis authorities for the same item.

An item may become `ready` only if rebuild can prove MySQL contains the complete accepted bid history needed for that item. If cloud Redis may have accepted bids that have not yet reached MySQL, that item must enter `protected` until recovery or manual verification resolves the gap.

If cloud Redis is unavailable and local Redis is also unavailable, the service stays alive but auction writes enter protection. Safe reads may continue from MySQL where possible, and `/health` reports both Redis authorities unavailable.

## Local Redis Rebuild

Local Redis rebuild uses MySQL as source of truth. It rebuilds:

- `auction:item:{item_id}:state`
- `auction:item:{item_id}:ranking`
- `auction:item:{item_id}:bidder_names`
- `auction:ending`
- `auction:room:{room_id}:state`
- `auction:room:{room_id}:item_queue`
- `auction:room:{room_id}:current_item`

Rebuild is item-scoped. Each item has local Redis metadata:

```text
auction:item:{item_id}:authority_state
auction:item:{item_id}:authority_epoch
auction:item:{item_id}:rebuild_token
auction:item:{item_id}:rebuild_lock
```

Item states:

| State | Meaning | Bid Behavior |
| --- | --- | --- |
| `rebuilding` | Local Redis state is being reconstructed. | Short wait, then retry or reject. |
| `ready` | Local Redis state is rebuilt and valid for current epoch. | Bid Lua may execute. |
| `protected` | Rebuild failed or data freshness is unsafe. | Bids pause. |
| `ended` | Auction is already ended. | Bids reject as normal business invalid. |

Rebuild commit rules:

- The rebuild worker writes a unique `rebuild_token`.
- It writes rebuilt state under the current epoch.
- It commits `authority_state=ready` only if the epoch and token still match.
- It never overwrites a ready state that has accepted bids in the same epoch.

## Cache Breakdown, Penetration, And Avalanche Protection

Cloud Redis failure can push many requests toward local Redis and MySQL. The rebuild path must explicitly protect MySQL and local Redis.

Controls:

- Batch rebuild active items with a fixed batch size.
- Limit rebuild workers.
- Use item-level rebuild locks so only one worker rebuilds an item at a time.
- Use local singleflight inside each process.
- For request-time misses, do not directly query MySQL from every request.
- Requests that see `rebuilding` wait briefly, then re-read local Redis.
- TTLs include jitter to avoid synchronized expiry.
- Empty or missing MySQL results write a short negative cache marker.
- Rebuild failures set a cooldown marker to avoid repeated immediate rebuilds.
- MySQL query concurrency for rebuild has a separate limiter from normal business queries.

For hot items, correctness is more important than instant acceptance. If local Redis is not ready, bids wait briefly or pause instead of stampeding MySQL.

## Bid During Rebuild

Preheating and bidding must not write the same item concurrently.

Bid flow:

1. Read the in-memory control-plane snapshot.
2. If the snapshot is invalid or stale, pause bidding.
3. Select active Redis from the snapshot.
4. Read item authority state from the active Redis.
5. If item state is `ready` and epoch matches, execute bid Lua.
6. If item state is `rebuilding`, wait 100-300 ms and re-read.
7. If the item is still not ready after the short wait, return a recoverable business response.
8. If item state is `protected`, reject bidding for that item until recovery.

The bid Lua must validate:

- `authority_epoch` matches the request epoch.
- `authority_state` is `ready`.
- auction status is `ongoing`.

This prevents a request using an old authority decision from writing after failover.

## MySQL Failure Path

When MySQL is unreachable but Redis remains available, Redis may continue as real-time authority for a short buffering window.

Flow:

1. Probe detects MySQL unavailable.
2. Controller writes `mode=mysql_buffering`, `mysql_state=buffering`.
3. Redis continues accepting bids for at most 10 seconds.
4. Accepted bids remain in Redis Stream/backlog as not-yet-persisted durable records.
5. Settlement, order creation, deposit refund, and compensation side effects pause.
6. If MySQL recovers within 10 seconds, workers drain the Redis Stream/backlog to MySQL.
7. Recovery verifies Redis current price, leader, bid count, and MySQL bid logs.
8. Only after backlog drain and consistency verification may settlement resume.
9. If MySQL remains unavailable for more than 10 seconds, all ongoing auction items enter `protected`.
10. Alerts fire for `mysql_buffering_timeout` and `auction_protected`.

Hard rule:

```text
Any bid accepted while MySQL is unavailable must be persisted to MySQL and verified before settlement, order creation, deposit refunds, or final winner confirmation.
```

## Settlement And Side Effects

Settlement is more sensitive than bidding because it creates irreversible business side effects.

Settlement must pause when:

- `mode=mysql_buffering`
- `mysql_state=down`
- bid-log backlog is above threshold
- bid-log worker has continuous failures
- item authority state is not `ready`
- item has unverified Redis-only bids

When MySQL recovers:

1. Resume bid-log workers.
2. Drain pending and new stream entries.
3. Retry dead-letterable messages when safe.
4. Compare Redis item state with MySQL bid logs.
5. Mark items settlement-safe.
6. Resume settlement cron only for settlement-safe items.

If a single item fails verification, protect only that item where possible.

## Health Semantics

### `/livez`

Liveness answers whether the process should stay alive.

- Do not ping Redis.
- Do not ping MySQL.
- Return `200` unless the process is unrecoverable.

### `/readyz`

Readiness answers whether the pod can receive ordinary traffic.

The first implementation should avoid removing every backend pod from service solely because cloud Redis is down. If the control-plane file is valid and the app can serve safe reads or local Redis mode, `/readyz` should remain `200`.

`/readyz` may return `503` when:

- the control-plane state is invalid or stale and safe reads cannot be served
- the process cannot load required configuration
- local runtime is unhealthy

MySQL unavailability should be reflected in mode and health details. It should not automatically kill liveness.

### `/health`

Health is operator-facing and detailed.

It should include:

- control-plane file status
- mode
- epoch
- active Redis authority
- cloud Redis probe status
- local Redis probe status
- MySQL probe status
- rebuild queue status
- protected item count
- bid-log backlog and pending count
- settlement paused status

It must not include credentials, DSNs, full tickets, or secret values.

## Kubernetes Changes

Deployment changes:

- Mount hostPath `/var/lib/live-auction/availability` into backend pods.
- Remove or make non-blocking the Redis wait initContainer.
- Keep `/livez` for startup and liveness probes.
- Use `/readyz` for readiness.
- Keep rolling update `maxUnavailable: 0` and `maxSurge: 1`.

The state file path should be configurable, with a production default matching the hostPath mount.

This design assumes a single-node k3s environment where hostPath is shared by all backend pods. A future multi-node deployment must move the control plane to a shared durable system such as Kubernetes Lease, etcd, a dedicated operator, or another consensus-backed store.

## Observability And Alerts

Metrics:

- control-plane mode gauge
- control-plane epoch gauge
- control-plane read errors
- control-plane stale/invalid count
- cloud Redis probe latency and errors
- local Redis probe latency and errors
- MySQL probe latency and errors
- Redis authority switch count
- item rebuild count by result
- item rebuild duration
- item rebuild queue depth
- protected item count
- bid rejects by availability reason
- MySQL buffering duration
- bid-log stream backlog
- bid-log worker error count
- settlement paused count

Alerts:

- control-plane file invalid or stale
- local Redis switching lasts too long
- local Redis active mode enabled
- MySQL buffering exceeds 10 seconds
- protected item count greater than zero
- rebuild failure count above threshold
- bid-log backlog above threshold
- settlement paused too long
- controller cannot write state file

Logs should include mode, epoch, item ID, room ID, and safe reason labels. They must not include secrets, credentials, tokens, or DSNs.

## Recovery Back To Cloud Redis

Switchback from local Redis to cloud Redis must not happen immediately after a successful ping.

Switchback flow:

1. Cloud Redis must be healthy for a stable window.
2. Stop accepting writes for the target item or briefly hold them behind item protection.
3. Copy verified local Redis state for that item to cloud Redis.
4. Compare local Redis, cloud Redis, and MySQL bid logs.
5. Increment epoch.
6. Mark the item ready on cloud Redis.
7. Move items back gradually.

If local Redis accepted bids during the outage, local Redis is the recovery source. Cloud Redis is stale until rebuilt from local Redis and verified.

Switchback should be item-scoped when possible. One inconsistent item must not block safe items from returning to cloud Redis.

## Testing Strategy

Unit tests:

- State file parser rejects stale, malformed, unsupported, and regressing epoch files.
- State file writer uses atomic replace and preserves monotonic epoch.
- Bid service rejects writes when control-plane state is invalid.
- Bid service waits briefly for `rebuilding` and proceeds only after `ready`.
- Bid Lua rejects mismatched epoch and non-ready item state.
- Rebuild worker commits only when token and epoch match.
- MySQL buffering pauses settlement.
- MySQL buffering beyond 10 seconds protects ongoing items.

Integration-style tests with fakes:

- Cloud Redis unavailable triggers local Redis switching.
- Local Redis rebuild warms active items from MySQL with bounded concurrency.
- Concurrent requests for the same missing item cause one rebuild and no MySQL stampede.
- MySQL unavailable for less than 10 seconds accepts Redis bids, drains backlog, verifies, then settles.
- MySQL unavailable for more than 10 seconds protects ongoing items and emits alert metrics.

Agent/online tests:

- Remove cloud Redis connectivity from backend path; service remains alive and switches to local Redis mode.
- Confirm ready rebuilt items can accept bids through local Redis.
- Confirm protected items reject bids without taking down the service.
- Remove MySQL connectivity; confirm bids continue within 10 seconds and settlement pauses.
- Restore MySQL; confirm backlog drains before settlement resumes.
- Confirm reports do not contain secrets, DSNs, passwords, tokens, or reusable credentials.

## Rollout Plan

1. Add control-plane state file reader, watcher, and validation.
2. Add atomic state file writer and single-writer lock.
3. Adjust startup so Redis connection failure does not exit the server.
4. Adjust `/livez`, `/readyz`, and `/health` semantics.
5. Add active Redis selection to cache wiring.
6. Add item authority state and epoch checks to bid path.
7. Add local Redis rebuild worker with batch limits, locks, cooldown, negative cache, and jitter.
8. Add MySQL buffering mode and 10-second protection timer.
9. Pause settlement and side effects while MySQL backlog is unverified.
10. Add observability and alerts.
11. Update Kubernetes deployment with hostPath mount and non-blocking Redis startup.
12. Add recovery and switchback workflow.

## First-Version Defaults

| Setting | Default |
| --- | --- |
| Control-plane path | `/availability/state.json` |
| Host control-plane path | `/var/lib/live-auction/availability/state.json` |
| Control-plane stale threshold | 5 seconds |
| Redis failover protection threshold | 3 seconds |
| MySQL buffering window | 10 seconds |
| Rebuild batch size | 50 items |
| Rebuild worker count | 2 |
| Bid wait while rebuilding | 100-300 ms |
| Negative cache TTL | 1-2 seconds |
| Rebuild cooldown TTL | 1-2 seconds |
| TTL jitter | +/- 10% |

These values should be configurable after the first implementation proves the workflow.
