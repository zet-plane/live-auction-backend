# Auction Realtime Sync Protocol

## Authority

The server is the only authority for bid acceptance, ranking, auction extension, and auction ending. Client countdowns are display-only.

## Streams

- `all`: default room entry stream; receives every event and is enough for ordinary rooms or low-pressure viewing.
- `control`: `auction_snapshot`, `time_sync`, `auction_started`, `auction_extended`, `auction_ended`, `auction_cancelled`, `user_outbid`, `order_created`.
- `market`: `bid_success` as latest market state after server coalescing.
- `user`: reserved future logical stream; Phase 1 clients do not open it, and `?stream=user` is treated as `control`.

## Adaptive Stream Mode

Clients start with one `all` connection. Millisecond auction sync is supported by default; clients automatically upgrade to split mode when room pressure or auction timing requires it.

Upgrade trigger examples:

- at least 5 `bid_success` or market updates per second for 3 consecutive seconds.
- the auction enters the last 30 seconds.

Upgrade sequence:

1. Keep `all` open.
2. Open `control` and wait for `auction_snapshot` or the next `time_sync`.
3. Open `market`.
4. Close `all` after `control` and `market` are healthy.

Downgrade sequence:

1. Open `all`.
2. Wait for `auction_snapshot`.
3. Close `control` and `market`.

During upgrade and downgrade overlap, clients must dedupe by `auction_version` and event type.

## Client Ordering

Clients must keep the largest `auction_version` seen for each `item_id`. A message with a lower `auction_version` must not overwrite current price, leader, ranking, or end time.

## Countdown

Clients compute:

```text
server_now = Date.now() + server_offset_ms
remaining_ms = end_time_unix_ms - server_now
```

`time_sync` corrects clock drift; it does not drive rendering one tick at a time.

## Reconnect

On reconnect, the client must:

1. Fetch room detail.
2. Fetch item detail.
3. Fetch ranking.
4. Request fresh WS tickets.
5. Reconnect in the mode active before disconnect: `all` for ordinary mode, or `control` then `market` for millisecond mode.
6. Apply `auction_snapshot` only if its `auction_version` is greater than or equal to the local version.
