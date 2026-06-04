# Redis Resilience and Degradation Design

## Context

The backend currently depends on Redis for several different kinds of behavior:

- Process startup verifies Redis with `Ping`; failure aborts the server.
- Ongoing auction state, bid ranking, idempotency, anti-sniping extension, and settlement coordination are stored in Redis and updated by Lua.
- Room online count, room item queue, current item pointers, and WebSocket presence use Redis as a real-time state layer.
- WebSocket tickets are issued and consumed through Redis.
- Some read paths already tolerate Redis errors by falling back to MySQL or default values.

These dependencies should not share one failure policy. Redis failure must not make the whole service look dead, but high-value auction writes must not proceed when the real-time authority is unknown.

## Goals

1. Keep the backend process alive when Redis is unavailable.
2. Preserve non-auction or read-only functionality during Redis incidents where MySQL is healthy.
3. Protect auction correctness by pausing risky writes when Redis state is unavailable, lagging, or recovering.
4. Expose Redis and auction degradation clearly through health endpoints and metrics.
5. Support Redis HA failover while still handling failover windows, replication lag, and split-brain risk.

## Non-Goals

- Do not replace Redis Lua as the real-time auction serialization point.
- Do not implement MySQL as a full real-time bidding fallback in the first version.
- Do not introduce Redis Cluster in the first version.
- Do not guarantee zero data loss from Redis async replication alone.
- Do not redesign the whole deployment topology in this design.

## Design Summary

Use three layers together:

1. Redis HA reduces outage duration.
2. An application Redis circuit breaker prevents request threads from repeatedly blocking on Redis timeouts.
3. Health endpoints expose process, traffic readiness, and degraded component status separately.

During Redis failure or recovery:

- The process remains alive.
- MySQL-backed read and order paths continue where possible.
- Auction writes such as `PlaceBid`, `StartItem`, and primary settlement fail fast or pause.
- Redis-backed read enrichment falls back to MySQL or safe defaults.
- Recovery does not reopen auction writes until Redis is reachable and active auction state is validated or rebuilt.

## Redis HA Strategy

The recommended first HA shape is Redis Sentinel or a managed Redis HA endpoint, not Redis Cluster.

Redis Cluster is not the first choice because current auction Lua touches multiple keys in one operation:

- `auction:item:{item_id}:state`
- `auction:item:{item_id}:ranking`
- `auction:item:{item_id}:bidder_names`
- `auction:item:{item_id}:idempotency:{key}`
- `auction:ending`

Redis Cluster would require careful hash-tag design so all Lua keys for one operation land in the same hash slot. That is a scaling design, not the minimum HA design.

For Sentinel or managed HA, configure Redis to reduce split-brain write windows:

```text
replica-read-only yes
min-replicas-to-write 1
min-replicas-max-lag 1
```

These settings do not make Redis strongly consistent. They limit how long an isolated old primary can continue accepting writes when it cannot see healthy replicas.

For high-value auction write acknowledgement, successful bid handling should eventually require:

```text
Redis Lua success
replica acknowledgement through WAIT or managed HA equivalent
durable bid log handoff
HTTP success response
```

The first implementation may keep synchronous MySQL `bid_logs` writes. If the bid hot path later moves to Redis Stream, the stream append becomes the durable handoff and must also be covered by the same failover safety policy.

## Redis Circuit Breaker

Add a process-local Redis circuit breaker in `internal/core/cache` or a nearby runtime package. It tracks Redis availability for business decisions.

States:

| State | Meaning | Business behavior |
| --- | --- | --- |
| `healthy` | Redis commands are succeeding | Normal operation |
| `suspect` | Recent errors or slow commands detected | Continue limited attempts; record metrics |
| `unavailable` | Consecutive failures crossed threshold | Auction writes fail fast; reads use fallback |
| `recovering` | Redis ping succeeds after outage | Keep auction writes paused; validate/rebuild state |

Transitions:

```text
healthy -> suspect
  on Redis command timeout/error or high latency.

suspect -> healthy
  after a short success window.

suspect -> unavailable
  after N consecutive failures or a connection pool timeout burst.

unavailable -> recovering
  when background probe can ping Redis and a simple read/write probe succeeds.

recovering -> healthy
  after active auction state validation or rebuild succeeds.

recovering -> unavailable
  if probes or validation fail.
```

Suggested initial thresholds:

```text
failure_threshold: 3 consecutive failures
success_threshold: 3 consecutive successful probes
probe_interval: 1s
redis_command_timeout: 100-300ms for auction writes
recovering_min_duration: 2s
```

The exact values should be config-driven later. The first version can hard-code conservative defaults with tests.

## Business Policies By Path

| Path | Redis unavailable | Redis recovering | Notes |
| --- | --- | --- | --- |
| `PlaceBid` | Fail fast with retryable auction-unavailable error | Fail fast | Do not accept bids while real-time state may be stale |
| `StartItem` | Reject with retryable auction-unavailable error | Reject | Starting an auction requires Redis state and ending schedule |
| `SettleDueAuctions` | Pause Redis-driven settlement | Validate before resuming | MySQL fallback compensation may continue cautiously |
| `EndExpiredAuctions` fallback | May scan MySQL but must not double-settle | May repair only safe cases | Avoid conflicting with Redis state during recovery |
| `GetRanking` | MySQL `bid_logs` fallback | MySQL fallback | Existing behavior mostly matches |
| `GetItem` / item lists | MySQL DTO fallback | MySQL or rebuilt Redis state | Real-time fields may be stale or absent |
| `Room` queries | Return `online_count=0`, empty queue, or MySQL current item | Same | Room Redis is enrichment, not durable truth |
| WebSocket ticket | Optional local-memory ticket fallback | Optional local-memory ticket fallback | Single-instance only unless routed consistently |
| WebSocket fanout | Continue local hub delivery | Continue | Presence sync soft-fails |
| Login, users, orders, payments | Continue if MySQL is healthy | Continue | Must not be blocked by Redis circuit state unless they use Redis directly |

Auction write errors should use a stable service-boundary error, for example:

```text
HTTP 503
code: 50301
message: auction service temporarily unavailable
```

This distinguishes retryable infrastructure degradation from invalid bids.

## Health Endpoints

Split current health behavior into three endpoint concepts.

### `/livez`

Liveness only answers whether the process is alive enough that Kubernetes should not restart it.

Rules:

- Does not ping Redis.
- Does not fail because Redis is unavailable.
- May fail only for unrecoverable process-level conditions.

Typical response:

```json
{"status":"ok"}
```

### `/readyz`

Readiness answers whether this instance should receive general traffic.

First-version policy for the current monolith:

- MySQL unavailable: return `503`.
- Redis unavailable: return `200` as long as MySQL is healthy.
- Redis degradation details are exposed through `/health`, not through `/readyz`.

Reason: in the current monolith, Redis failure should not remove all traffic if MySQL-backed browsing, order, and account features can still work.

If the auction write path is later split into its own service, that service's `/readyz` may return `503` when Redis circuit state is not `healthy`.

### `/health`

Detailed component health for monitoring and humans.

Example degraded response:

```json
{
  "status": "degraded",
  "components": {
    "mysql": {"status": "ok", "latency": "8ms"},
    "redis": {"status": "error", "error": "i/o timeout"},
    "redis_circuit": {"status": "unavailable"},
    "auction_write": {"status": "unavailable"},
    "auction_read": {"status": "degraded"},
    "ws_ticket": {"status": "degraded"}
  }
}
```

This endpoint can return `503` when degraded if monitoring expects non-2xx alerts. Kubernetes liveness should not use it.

## Recovery And State Validation

Redis becoming reachable is not enough to resume auction writes. The app must enter `recovering` first.

Recovery steps:

1. Ping Redis and run a short read/write probe.
2. List active ongoing items from MySQL.
3. For each ongoing item:
   - Check `auction:item:{id}:state`.
   - If state exists and is internally consistent, keep it.
   - If missing or stale, rebuild from MySQL item/rule plus durable `bid_logs`.
   - Rebuild ranking and bidder names from `bid_logs` when needed.
   - Rebuild `auction:ending` score from the authoritative end time.
4. Check room current item pointers from MySQL and repair Redis room state opportunistically.
5. Mark circuit `healthy` only after validation completes.

If validation fails for any active auction, keep auction writes unavailable and expose `recovering` or `unavailable` in `/health`.

## Rebuild Rules

All Redis keys must be classified as either rebuildable or authoritative.

| Key | Classification | Rebuild source |
| --- | --- | --- |
| `auction:item:{id}:state` | Real-time working state; must be rebuildable after incident | MySQL item/rule + `bid_logs` |
| `auction:item:{id}:ranking` | Rebuildable projection | `bid_logs` grouped by user max price |
| `auction:item:{id}:bidder_names` | Rebuildable projection | users table joined from `bid_logs` |
| `auction:item:{id}:idempotency:{key}` | Fast-path guard; needs durable backup | future unique index on `bid_logs` |
| `auction:ending` | Rebuildable scheduler index | ongoing item state end times |
| `auction:room:{id}:state` | Rebuildable room projection | MySQL room + local presence default |
| `auction:room:{id}:item_queue` | Rebuildable projection | published items in room |
| `ws:ticket:{ticket}` | Ephemeral | no rebuild; local fallback may exist |

The important durable record is `bid_logs`. Redis state can be reconstructed only if every acknowledged bid has durable evidence.

## Idempotency Hardening

Redis idempotency keys are not enough across failover because the key can be lost if the replica was behind.

Add a durable idempotency field in `bid_logs` in a later implementation:

```text
item_id
user_id
idempotency_key
```

Add a unique index over these fields. Redis remains the fast-path idempotency check; MySQL prevents duplicate acknowledged bids after failover or retries.

## Observability

Add metrics and logs around degradation:

- Redis command error count by command or operation.
- Redis command latency P95/P99.
- Redis circuit state gauge.
- Redis circuit transition counter.
- Auction write rejected count by reason: `redis_unavailable`, `redis_recovering`.
- Redis state rebuild count and duration.
- Redis/MySQL auction state mismatch count.
- Settlement paused count.
- Bid log persistence failure count.
- Health endpoint component status.

Logs must include operation, item ID or room ID where safe, and circuit state. Do not log secrets, Redis credentials, or tokens.

## Deployment Notes

Current k3s production shape is single-node. Running multiple Redis pods on the same node can protect against Redis process or pod failure, but not node, disk, or network failure.

Production-grade Redis HA requires either:

- managed Redis HA outside the single-node k3s cluster, or
- a multi-node Kubernetes cluster with Redis Sentinel/HA scheduled across nodes.

The application-level degradation design is still required in both cases because failover windows and replication lag still exist.

## Testing Strategy

Unit tests:

- Circuit transitions from healthy to suspect to unavailable after consecutive failures.
- Circuit transitions from unavailable to recovering to healthy only after validation success.
- `PlaceBid` fails fast when circuit is unavailable or recovering.
- Ranking and item reads fall back when cache returns errors.
- Room service handles nil/noop cache or cache errors without panics.
- Health handlers return correct live, ready, and degraded responses.

Integration or agent tests:

- Start backend with Redis unavailable; process stays alive and `/livez` succeeds.
- Redis unavailable: non-auction read path succeeds; `PlaceBid` returns retryable 503.
- Redis returns after outage: circuit enters recovering, rebuilds active auction state, then resumes writes.
- Redis state missing but MySQL `bid_logs` present: rebuild ranking and state.
- Simulated Redis failover during active bidding does not produce two winners or duplicate orders.

Failure drills:

- Kill Redis primary pod.
- Isolate Redis primary network path.
- Restart backend while Redis is unavailable.
- Induce Redis timeout latency without killing Redis.
- Fail MySQL bid log write after Redis Lua success and confirm no HTTP success is acknowledged.

## Rollout Plan

1. Add `/livez`, `/readyz`, and richer `/health`.
2. Change startup so Redis connection failure does not abort the whole server.
3. Introduce Redis circuit breaker and expose it through health.
4. Wire `PlaceBid`, `StartItem`, and settlement to fail fast or pause when circuit is not healthy.
5. Add fallback/noop cache behavior for room and WebSocket presence paths.
6. Add recovery validation and Redis state rebuild for active auctions.
7. Add Redis HA deployment or managed Redis endpoint.
8. Add durable bid idempotency hardening.

## Decisions

1. `/readyz` remains `200` when Redis is unavailable if MySQL is healthy. `/health` reports the Redis and auction-write degradation.
2. WebSocket ticket fallback is not part of the first implementation. Redis-unavailable ticket issuance and WS upgrade should report degraded/unavailable instead of silently switching to process-local tickets. Process-local tickets can be added later only with sticky routing or single-instance deployment.
3. Production should prefer managed Redis HA when available. Self-hosted Sentinel is acceptable for cost control or learning, but must be tested with failover drills.
4. Acknowledged bid success should eventually require `PlaceBidLua` success, replica acknowledgement through `WAIT` or equivalent, and durable bid log persistence before HTTP success. In the current synchronous bid-log design, the order is Lua, replica acknowledgement, MySQL `bid_logs`, then HTTP success.
