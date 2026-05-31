# WebSocket Countdown and Settlement Design

## Goal

Make auction countdown and settlement correct under high-concurrency bidding, while giving every connected or reconnecting client the same authoritative view of the countdown and final deal result.

The backend must treat Redis as the real-time authority for an ongoing auction. WebSocket is the synchronization channel for clients, not the source of truth. MySQL remains the durable record for final item and order state.

## Current Problems

The current implementation has three gaps:

1. `EndExpiredAuctions` runs every minute and scans MySQL `auction_rules.end_time`, so settlement is not aligned with the real-time countdown.
2. Anti-sniping extension updates Redis `end_time_unix`, but MySQL `auction_rules.end_time` is unchanged, so the current cron can settle too early after an extension.
3. Expired auctions delete `auction:item:{item_id}:state`, which makes reconnect and post-deal detail rendering depend on MySQL only and loses the live final snapshot.

## Chosen Approach

Use Redis as the authoritative live auction state and settlement coordination layer:

- `auction:item:{item_id}:state` stores the current auction status, countdown, leader, and deal price.
- `auction:ending` is a Redis sorted set of active item IDs scored by authoritative end time in Unix milliseconds.
- Bidding and settlement both use Redis Lua scripts so high-concurrency bid requests and the settlement worker cannot produce split-brain results.
- WebSocket broadcasts `time_sync`, `auction_snapshot`, and `auction_ended` events derived from Redis state.
- MySQL is updated after Redis atomically claims the final result.

This replaces the current MySQL-time cron as the primary settlement path. A slower compensation job may still scan MySQL for durability gaps, but it must not be the live countdown mechanism.

## Redis State

`auction:item:{item_id}:state` should use these canonical fields:

| Field | Meaning |
| --- | --- |
| `status` | `ongoing` or `ended` |
| `leader_user_id` | Current leading user while ongoing; final deal user when ended |
| `deal_price` | Current highest valid price while ongoing; final deal price when ended |
| `end_time_unix_ms` | Authoritative scheduled ending time in Unix milliseconds |
| `ended_at_unix_ms` | Actual settlement time in Unix milliseconds; empty while ongoing |
| `bid_count` | Total accepted bid count |
| `participant_count` | Distinct bidder count |
| `is_extended` | Whether the latest accepted bid triggered extension |
| `extend_count` | Number of extensions |
| `total_extended_sec` | Total extension seconds |
| `end_reason` | `time_expired` or `price_cap` after settlement |

For compatibility during migration, existing code may continue to write/read `current_price`, but DTOs and WebSocket payloads should expose `deal_price` as the frontend-facing price field. If both fields exist during rollout, `deal_price` is canonical and `current_price` is a legacy alias.

Do not delete this Redis state immediately after settlement. Mark it `ended` and keep it with a TTL, initially 24 hours. MySQL remains the permanent record.

## Countdown Synchronization

Connected clients must render countdown from server-authoritative timestamps, not from independent client clocks.

The backend broadcasts a room-level `time_sync` event at a fixed cadence, initially every second:

```json
{
  "type": "time_sync",
  "payload": {
    "item_id": "item_1",
    "server_time_unix_ms": 1760000000000,
    "end_time_unix_ms": 1760000030000,
    "status": "ongoing"
  }
}
```

The frontend displays:

```text
remaining = end_time_unix_ms - corrected_client_now
```

where `corrected_client_now` is based on the latest `server_time_unix_ms` offset. This keeps all users aligned to the same end time while allowing smooth local rendering between WebSocket messages.

On reconnect, the backend sends an immediate `auction_snapshot`:

```json
{
  "type": "auction_snapshot",
  "payload": {
    "item_id": "item_1",
    "status": "ongoing",
    "server_time_unix_ms": 1760000000000,
    "end_time_unix_ms": 1760000030000,
    "leader_user_id": "user_1",
    "deal_price": 1200,
    "bid_count": 4,
    "participant_count": 2
  }
}
```

If the auction is already ended, the snapshot uses the same fields with `status=ended`, `ended_at_unix_ms`, and `end_reason`.

## Bidding Under Concurrency

`PlaceBidLua` remains the serialization point for bid acceptance. It must:

1. Read `status` and reject if not `ongoing`.
2. Reject when `now_unix_ms >= end_time_unix_ms`.
3. Validate price and bid increment.
4. Update `leader_user_id`, `deal_price`, bid counters, and ranking atomically.
5. If anti-sniping extension triggers, update `end_time_unix_ms` and also update `auction:ending` score for the item.
6. If `price_cap > 0 && price >= price_cap`, atomically mark `status=ended`, set `ended_at_unix_ms`, set `end_reason=price_cap`, and remove the item from `auction:ending`.

The HTTP response for successful bids should include:

```json
{
  "bid_id": "bid_1",
  "leader_user_id": "user_1",
  "deal_price": 1200,
  "end_time_unix_ms": 1760000030000,
  "status": "ongoing"
}
```

If the bid reaches the price cap, the response should return `status=ended` and the final `leader_user_id` / `deal_price`.

## Settlement Worker

Replace live settlement by MySQL cron with a Redis-driven worker.

The worker loop:

1. Reads due item IDs from `auction:ending` where score `<= now_unix_ms`.
2. Calls a settlement Lua script per item or in small batches.
3. The Lua script claims only an item with `status=ongoing` and `end_time_unix_ms <= now_unix_ms`.
4. The Lua script writes final fields:
   - `status=ended`
   - `leader_user_id` unchanged from the final leader
   - `deal_price` unchanged from final highest bid
   - `ended_at_unix_ms=now_unix_ms`
   - `end_reason=time_expired`
5. The script removes the item from `auction:ending`.
6. The Go service persists the final result to MySQL.
7. The Go service clears room current item, creates the order if `leader_user_id` is non-empty, and broadcasts WebSocket events.

Only the script that successfully changes `status` from `ongoing` to `ended` owns the settlement. Duplicate workers or retries should see `status=ended` and no-op.

The first implementation can use a one-second worker interval. Later optimization may use a shorter interval or blocking scheduling, but correctness must remain Redis-state based.

## WebSocket Events After Settlement

On final settlement, broadcast `auction_ended` to the room:

```json
{
  "type": "auction_ended",
  "payload": {
    "item_id": "item_1",
    "leader_user_id": "user_1",
    "deal_price": 16800,
    "ended_at_unix_ms": 1760000030123,
    "end_reason": "time_expired"
  }
}
```

For price-cap settlement, `end_reason` is `price_cap`.

If an order is created, also broadcast the existing `order_created` event to the room and unicast it to the winner.

Frontend behavior:

- `status=ongoing`: show countdown, current leader, and current deal price.
- `auction_ended` or `snapshot.status=ended`: stop countdown and show final deal user and final deal price using `leader_user_id` and `deal_price`.
- Reconnect: replace local state with `auction_snapshot` before applying later events.

## HTTP DTOs

Item list/detail DTOs should expose the same canonical semantics:

- `leader_user_id`: current leader or final deal user.
- `deal_price`: current highest valid price or final deal price.
- `remaining_ms`: computed from Redis `end_time_unix_ms` when `status=ongoing`; otherwise `0`.
- `status`: Redis status for ongoing/ended live state, with MySQL as fallback.

During migration, `current_price` can remain for compatibility. New frontend code should prefer `deal_price`.

## Error Handling and Recovery

Redis settlement succeeds but MySQL update fails:

- Keep Redis state as `ended`.
- Log the failure with item ID and final result.
- A compensation job retries persisting ended Redis snapshots or ended MySQL items without orders.

MySQL update succeeds but order creation fails:

- Keep item `ended`.
- Log failure.
- Existing order compensation creates missing orders for ended items with non-empty `leader_user_id` / `winner_id`.

WebSocket broadcast fails:

- Do not roll back settlement.
- Reconnecting clients get the final result from `auction_snapshot`.

Service restart:

- Active auctions are recovered from Redis `auction:ending` and item state.
- Ended Redis snapshots remain available until TTL.
- MySQL remains the durable fallback.

## Testing

Unit tests:

- Bid Lua rejects bids after `end_time_unix_ms`.
- Bid Lua updates `auction:ending` when extension occurs.
- Bid Lua marks `status=ended` for price-cap bids.
- Settlement Lua only claims `ongoing` due items and no-ops for already ended items.
- HTTP DTO maps Redis `leader_user_id` / `deal_price` correctly.

Service tests:

- Time-expired settlement persists `status=ended`, winner, and deal price to MySQL.
- Price-cap settlement clears current item and keeps Redis final snapshot.
- Reconnect snapshot returns ongoing countdown fields before settlement.
- Reconnect snapshot returns final deal fields after settlement.

Concurrency tests:

- Many concurrent bids near the end produce one final leader and one final deal price.
- Settlement racing with a valid last-moment bid either accepts the bid before end or rejects it after end, but never produces two final results.
- Multiple settlement workers cannot create duplicate finalization or duplicate orders.

Integration tests:

- Three clients receive aligned `time_sync` values.
- A disconnected client reconnects and receives a current `auction_snapshot`.
- All clients receive `auction_ended` with the same `leader_user_id` and `deal_price`.

## Implementation Notes

The current code paths most affected are:

- `internal/app/item/cache/cache.go`
- `internal/app/item/cache/bid.go`
- `internal/app/item/service/bid_service.go`
- `internal/app/item/service/service.go`
- `internal/app/item/dto/item.go`
- `internal/app/item/dto/bid.go`
- `internal/app/item/dto/events.go`
- `internal/app/ws/hub`

The existing one-minute `EndExpiredAuctions` cron should become a fallback or be replaced by the Redis settlement worker for live behavior. It must no longer settle from MySQL `auction_rules.end_time` when Redis has an authoritative live state.
