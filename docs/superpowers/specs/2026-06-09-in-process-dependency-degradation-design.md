# In-Process Dependency Degradation Design

## Background

The backend still needs dependency degradation, but it does not need a shared control plane or consensus for Redis authority.

The deployment assumption is:

- All backend pods can reach the cloud Redis endpoint at the same time, or none of them can.
- All backend pods can reach the cloud MySQL endpoint at the same time, or none of them can.
- A local Redis is available as a temporary real-time auction authority when cloud Redis is unreachable.

Because pod-level observations are not expected to split, every pod can run the same local probe loop and reach the same operating mode without `/availability/state.json`, file locks, or a separate controller.

## Goals

1. Keep backend pods alive when cloud Redis or cloud MySQL becomes unreachable after startup.
2. Switch auction real-time state from cloud Redis to local Redis when cloud Redis is down.
3. Rebuild local Redis auction state from MySQL bid logs, accepting that the rebuild may roll back bids that were accepted by cloud Redis but not yet persisted to MySQL.
4. Allow Redis to buffer bids for a short window when MySQL is down.
5. Reject new auction writes after the MySQL buffering window expires.
6. Keep WebSocket ticket, presence, pub/sub, cron lease, bid stream, and auction cache users behind the same active Redis runtime.
7. Expose health, metrics, and logs that make cloud Redis failover, local Redis mode, MySQL buffering, and protected auctions visible.

## Non-Goals

- Do not build a shared control plane for Redis authority.
- Do not use a state file, Kubernetes lease, etcd, or another consensus mechanism to decide active Redis.
- Do not handle a case where some pods can reach cloud Redis and other pods cannot.
- Do not merge cloud Redis auction state back after local Redis has accepted writes.
- Do not guarantee lossless Redis failover for bids that were accepted only in cloud Redis and not yet persisted to MySQL.
- Do not accept auction writes indefinitely while MySQL is unavailable.

## Operating Modes

The runtime keeps an in-memory mode in each backend pod.

| Mode | Meaning | Write behavior |
| --- | --- | --- |
| `normal_cloud` | Cloud Redis and MySQL are reachable. | Auction writes use cloud Redis. Durable writes use MySQL. |
| `local_redis_switching` | Cloud Redis is down, local Redis is reachable, and local auction state is being rebuilt. | An item can accept writes only after its local Redis state is ready. |
| `local_redis_active` | Local Redis is the active real-time authority. | Ready items accept writes through local Redis. |
| `mysql_buffering` | Redis is reachable but MySQL is down and the buffering window has not expired. | Redis can continue accepting bids temporarily. MySQL side effects are deferred. |
| `auction_protected` | Redis is unavailable, MySQL buffering expired, or an item cannot be safely rebuilt. | Affected writes return 503. |

These modes are local process decisions produced by identical probe inputs, not externally coordinated state.

## Redis Failure

When cloud Redis is healthy, the backend uses cloud Redis for:

- Auction hot state and Lua writes.
- Bid log stream.
- WebSocket event bus.
- WebSocket ticket authority.
- Presence store.
- Cron leases.
- Ranking rebuild locks.

When cloud Redis fails:

1. A probe loop marks cloud Redis unhealthy after `redis_failover_threshold`.
2. If local Redis is reachable, the runtime switches to `local_redis_switching`.
3. All active Redis consumers begin using local Redis through `Availability.ActiveRedis()`.
4. Each ongoing auction item is rebuilt from MySQL item/rule/bid log data before accepting writes.
5. Rebuilt items become ready on local Redis and can continue accepting bids.
6. Items that cannot be rebuilt become protected and reject bids.

The rebuild source of truth is MySQL. If cloud Redis accepted several bids that only existed in Redis Stream and had not been persisted to MySQL before cloud Redis became unreachable, those bids are not replayed into local Redis.

This is accepted degradation behavior. Users may experience it as a short time rollback: current price, leader, bid count, and timer can return to the last durable MySQL point.

When cloud Redis recovers after local Redis has become active, do not automatically switch active auctions back to cloud Redis. The first version keeps local Redis sticky until active auctions end or an operator restarts/switches during a maintenance window. This avoids merging two divergent Redis histories.

## MySQL Failure

When MySQL is down but the active Redis is still reachable:

1. The runtime enters `mysql_buffering`.
2. New bids may continue for `mysql_buffering_window`.
3. Accepted bids continue to update Redis through Lua.
4. Bid log events continue to enter the active Redis Stream.
5. The bid log worker fails to persist to MySQL and must not ack messages it has not durably written.
6. Settlement, order creation, deposit refunds, and other MySQL side effects pause or return a protected error.

When MySQL recovers before the buffering window expires:

1. The bid log worker drains pending and new stream messages.
2. Durable bid logs catch up in MySQL.
3. Settlement and side effects resume after the backlog is persisted.

When MySQL remains unavailable past the buffering window:

- Reject new bids with `ErrAvailabilityUnavailable`.
- Reject starting new auctions.
- Pause settlement and order/deposit side effects.
- Keep safe reads and WebSocket reconnects where possible.
- Report degraded readiness.

If MySQL is down and Redis is also lost before buffered stream data is persisted, the buffered writes may be lost. This is outside the tolerated single-dependency degradation path.

## Runtime Shape

Replace the file-backed availability watcher with an in-process dependency runtime.

```go
type Runtime struct {
    cloudRedis *redis.Client
    localRedis *redis.Client
    db         *gorm.DB
    snapshot   atomic.Value
}
```

The runtime exposes:

```go
Snapshot() Snapshot
ActiveRedis() (*redis.Client, Snapshot, bool)
Run(ctx context.Context)
```

`Run` periodically probes:

- Cloud Redis with `PING`.
- Local Redis with `PING`.
- MySQL with `sqlDB.PingContext`.

The probe loop updates only local memory. Business hot paths read the current snapshot and never perform control-plane I/O.

## Item Authority

Do not use a global control-plane epoch.

Keep item-level authority fields because they are useful for Lua safety and rebuilds:

- `AuctionState.AuthorityEpoch`
- `AuctionState.AuthorityState`
- `BidLog.AuthorityEpoch`
- `BidLog.AuctionVersion`

When rebuilding an item on local Redis:

1. Load the item and rule from MySQL.
2. Load persisted bid logs for the latest known item authority epoch.
3. Reconstruct price, leader, bid count, participant count, and auction version.
4. Write local Redis state with `AuthorityState=ready`.
5. Reject the item if continuity cannot be established.

Accepted rollback means the latest known durable MySQL epoch/version is enough. The system does not need to recover cloud Redis-only bids.

## Health Semantics

`/livez` remains process liveness only.

`/readyz` and `/health` should report:

- `mysql`
- `cloud_redis`
- `local_redis`
- `active_redis`
- `availability_mode`
- `presence`

Suggested readiness behavior:

| Condition | Readiness |
| --- | --- |
| `normal_cloud` | 200 |
| `local_redis_active` | 200 with degraded status |
| `mysql_buffering` within window | 200 with degraded status |
| MySQL buffering expired | 503 |
| Cloud and local Redis both down | 503 |

## Config

Keep an `availability` section, but remove control-plane fields.

```yaml
availability:
  redis_probe_interval: 1s
  redis_failover_threshold: 3s
  redis_recover_threshold: 30s
  mysql_probe_interval: 1s
  mysql_buffering_window: 10s
  local_redis:
    addr: redis:6379
    password: ""
    db: 0
```

Remove:

- `state_path`
- `stale_threshold`
- shared file mount requirements
- file lock settings
- control-plane writer settings

## Code Change Plan

1. Replace `internal/core/availability` watcher/state-file code with the in-process probe runtime.
2. Keep `ActiveRedis()` as the integration contract for item cache, WebSocket bus, ticket authority, presence, and subscriber loops.
3. Update `cmd/server/server.go` to create cloud Redis, local Redis, and the runtime directly.
4. Update health handlers to report runtime probe data instead of control-plane status.
5. Update item service checks:
   - Redis unavailable means reject auction writes.
   - MySQL down within window means allow bids but pause side effects.
   - MySQL down after window means reject new writes.
6. Make local Redis rebuild explicitly tolerate rollback to MySQL persisted bid logs.
7. Remove deployment/config references to `/availability/state.json`.

## Tests

Add focused unit tests for:

- Runtime switches from cloud to local after cloud Redis exceeds the failover threshold.
- Runtime enters `redis_down`/protected when both Redis clients are unavailable.
- Runtime enters and exits `mysql_buffering`.
- MySQL buffering allows bids inside the window.
- MySQL buffering rejects bids after the window.
- Local Redis rebuild uses MySQL bid logs and accepts rollback to the durable point.
- WebSocket ticket and bus use the active Redis client.
- Health returns 200 degraded for local Redis mode and 503 for expired buffering or no Redis authority.

Tests must use fake probe functions, fake stores, and fake caches. They must not connect to real MySQL, Redis, HTTP services, WebSocket, or external systems.
