# Bid Hot Path Performance Design

## Context

The 2026-06-04 `bid_only` performance probe isolated the HTTP bid write path by disabling WebSocket fanout. The system remained stable at 150 and 300 QPS, then showed a clear latency knee around 500 QPS:

- 500 QPS target, 485.90 actual QPS.
- Server P95 max 849.6ms, P99 max 1.479s.
- DB operations max about 1617.8/s.
- Backend restart count stayed 0, and resources recovered after stop.

The current `PlaceBid` hot path reads item/rule from MySQL before every bid, executes Redis Lua, then synchronously inserts a `bid_logs` row for every non-idempotent successful bid. This makes both rejected bids and successful bids pay avoidable MySQL cost.

## Goal

Move ongoing auction bidding to a Redis-centered hot path:

1. Avoid MySQL item/rule reads on every bid.
2. Avoid synchronous MySQL `bid_logs` inserts before returning HTTP success.
3. Keep Redis as the real-time source of truth during an ongoing auction.
4. Preserve eventual MySQL bid log durability with retry, metrics, and reconciliation.
5. Keep failure behavior explicit enough to test and operate.

## Non-Goals

- Do not redesign auction rules, deposit semantics, or ranking semantics.
- Do not replace Redis Lua as the atomic bid decision point.
- Do not make MySQL ranking the real-time source during ongoing auctions.
- Do not introduce external queues or new infrastructure beyond Redis and the existing Go service.
- Do not run more online load tests as part of the design step.

## Design Summary

Extend `auction:item:{item_id}:state` so it contains all hot bid fields needed by `PlaceBid`:

```text
status
room_id
current_price
deal_price
leader_user_id
end_time_unix
end_time_unix_ms
bid_count
participant_count
is_extended
extend_count
total_extended_sec
end_reason
bid_increment
price_cap
deposit_amount
extend_trigger_sec
auto_extend_sec
max_extend_count
max_total_extend_sec
```

`StartItem` writes these fields into Redis when the item becomes `ongoing`. `CancelItem`, price-cap settlement, time settlement, room end, and final cleanup remove, clear, or expire the relevant Redis state as they do today.

`PlaceBid` changes from:

```text
MySQL FindItemWithRule
Redis PlaceBidLua
MySQL CreateBidLog
broadcast
HTTP response
```

to:

```text
Redis hot state lookup
optional deposit check when deposit_amount > 0
Redis PlaceBidLua
Redis Stream append for successful bid log
broadcast enqueue
HTTP response
```

A background worker consumes the Redis Stream and writes `bid_logs` to MySQL in batches.

## Real-Time Source Of Truth

During `ongoing`, Redis is the real-time truth for:

- current price
- leader
- end time and anti-sniping extension counters
- bid count and participant count
- ranking and bidder names
- auction ended by price cap inside Lua

MySQL remains the durable catalog and final audit store:

- item/rule metadata outside hot bidding
- final item status, winner, deal price
- durable `bid_logs`
- fallback or historical ranking when Redis state is unavailable after the auction lifecycle

HTTP bid success means:

1. Redis state and ranking were updated atomically.
2. The bid log event was durably appended to Redis Stream.
3. MySQL `bid_logs` may lag until the worker consumes the stream.

If Redis Lua succeeds but Redis Stream append fails, the HTTP request must return an internal error and record a high-priority metric. This avoids acknowledging a bid that has no durable log handoff.

## Redis Hot State

Add cache methods:

```go
type AuctionHotConfig struct {
    ItemID             string
    RoomID             string
    Status             string
    BidIncrement       int64
    PriceCap           int64
    DepositAmount      int64
    ExtendTriggerSec   int
    AutoExtendSec      int
    MaxExtendCount     int
    MaxTotalExtendSec  int
    EndTimeUnixMS      int64
}

GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error)
InitAuctionState(ctx context.Context, itemID string, state AuctionState) error
```

`AuctionState` can be extended with the hot fields rather than creating a second Redis key. Keeping one hash avoids split-brain between `state` and `config`.

Hot miss behavior:

1. `PlaceBid` calls `GetAuctionHotConfig`.
2. If missing, load `FindItemWithRule` once from MySQL.
3. If item is not `ongoing`, return the same invalid/not-found behavior as today.
4. If item is `ongoing`, rebuild Redis hot state from item/rule and continue.
5. Record `auction.hot_state_rebuild` metrics.

Negative cache may be added for missing or non-ongoing items with a short TTL, but only after the main design lands. It is not required for the first implementation.

## Redis Lua Changes

`PlaceBidLua` should receive fewer rule arguments from Go once the state hash carries the hot fields. The Lua script reads:

- `bid_increment`
- `price_cap`
- anti-sniping policy fields
- current state fields

The script continues to enforce:

- state exists
- status is ongoing
- not ended by time
- price > current price
- price increment alignment
- idempotency
- ranking update
- participant count
- bid count
- anti-sniping extension
- price-cap ending

For compatibility, the first implementation may keep existing ARGV fields while also storing hot fields in Redis. Once tests pass, Lua can be simplified to source rule fields from the hash. The important behavior is that `PlaceBid` no longer reads item/rule from MySQL for every request.

## Async Bid Log Stream

Use Redis Stream:

```text
stream: auction:bid_log:stream
group:  bid-log-writers
```

Each successful non-idempotent Lua result appends:

```text
bid_id
item_id
room_id
user_id
price
created_at_unix_ms
```

Idempotent Lua code `1` does not append a new stream event.

Append must happen after Lua success and before HTTP success. This means a successful HTTP bid always has a Redis state update and a durable handoff to the worker.

## Bid Log Worker

Add a worker owned by the item module lifecycle.

Behavior:

1. Ensure consumer group exists.
2. `XREADGROUP` batches from `auction:bid_log:stream`.
3. Convert stream entries to `model.BidLog`.
4. Batch insert to MySQL.
5. `XACK` only after MySQL batch insert succeeds.
6. Periodically `XAUTOCLAIM` old pending entries.
7. Move poison entries to a dead-letter stream after repeated failures.

Suggested defaults:

```text
batch_size: 200
flush_interval: 100ms
pending_idle_timeout: 30s
max_attempts: 5
dead_stream: auction:bid_log:dead
```

Add DAO method:

```go
CreateBidLogs(logs []*model.BidLog) error
```

The batch insert must be idempotent by primary key. Duplicate delivery should not create duplicate rows or fail the whole batch. Use the local GORM pattern for an "insert ignore" or "on duplicate do nothing" equivalent.

## Metrics And Alerts

Add metrics for the new hot path:

```text
auction.hot_state.lookup.count{result=hit|miss|error|rebuilt}
auction.hot_state.lookup.duration
auction.bid_log.stream.append.count{result=success|error}
auction.bid_log.stream.append.duration
auction.bid_log.worker.batch.count{result=success|error}
auction.bid_log.worker.batch.size
auction.bid_log.worker.persist.duration
auction.bid_log.worker.pending.count
auction.bid_log.worker.lag.seconds
auction.bid_log.worker.dead_letter.count
```

Alert when:

- stream append errors occur
- worker lag exceeds the agreed threshold
- pending entries grow continuously
- dead-letter count increases
- MySQL batch persist duration stays high

## Failure Semantics

| Failure | Behavior |
| --- | --- |
| Hot state miss and DB rebuild succeeds | Rebuild Redis state and continue |
| Hot state miss and DB says item not ongoing | Return invalid request/not found as today |
| Hot state miss and DB errors | Return error; record rebuild error |
| Redis unavailable | Return internal error; bidding cannot safely continue |
| Redis Lua rejects low price | Return `40003`; no MySQL read, no stream append |
| Redis Lua rejects invalid increment | Return `40004`; no MySQL read, no stream append |
| Redis Lua succeeds but stream append fails | Return internal error; record critical metric |
| Stream append succeeds but worker lags | HTTP success remains valid; alert and catch up |
| Worker writes duplicate bid ID | Treat as idempotent success for that row |
| Worker poison entry | Move to dead stream after max attempts and alert |

## Consistency And Reconciliation

Reconciliation checks compare:

- Redis state `bid_count`
- Redis ranking size and top entry
- MySQL `bid_logs` count for the item after worker catch-up
- highest MySQL bid versus Redis current price

During an active auction, MySQL may lag Redis. Reports and APIs that need live state should continue to prefer Redis. After settlement or after a catch-up wait, MySQL should match the successful stream events.

## Testing Strategy

Local unit tests:

- `PlaceBid` hot state hit avoids `FindItemWithRule`.
- Hot state miss rebuilds from store once.
- Low-price rejection does not call store or stream append.
- Successful bid appends one stream event.
- Idempotent retry does not append another stream event.
- Stream append failure returns error.
- Worker batch insert acknowledges only after DAO success.
- Worker duplicate bid IDs are idempotent.
- Worker claims old pending messages.

Local integration tests with Redis/MySQL when explicitly approved:

- Start item writes hot fields.
- Bid-only load generates Redis Stream entries.
- Worker drains entries into MySQL.
- Reconciliation passes after worker catch-up.

Performance regression gates:

- `bid_only` 300 QPS short probe: server P95 below 500ms, timeout rate below 1%, backend restart 0.
- Optional guarded 500 QPS probe after optimization: compare P95/P99, DB ops, MySQL CPU, worker lag, and stream append duration against 2026-06-04 baseline.

## Rollout Plan

1. Extend Redis state with hot fields while keeping current MySQL read path.
2. Change `PlaceBid` to use hot state and rebuild on miss.
3. Add stream append after successful non-idempotent Lua result, still optionally keep synchronous bid log behind a temporary feature switch during local verification.
4. Add worker and batch DAO insert.
5. Remove synchronous bid log from the HTTP path after unit and local integration coverage passes.
6. Run local `bid_only` performance probe.
7. Run guarded online probe only after reviewing local evidence.

## Open Operational Choice

The design uses Redis Stream rather than an in-process queue. An in-process queue would be lower latency but loses accepted bid logs on process crash. Redis Stream gives durable handoff and replay, which matches the chosen HTTP success semantics.
