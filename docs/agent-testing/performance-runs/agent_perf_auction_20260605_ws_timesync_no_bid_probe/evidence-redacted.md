# Evidence Redacted

## Stage

- Window: 2026-06-05 01:55:29 +0800 to 01:58:29 +0800.
- Target / actual QPS: 70.00 / 70.00.
- Target / connected WS: 300 / 300.
- WS connect failures: 0.
- Request mix: `item_only`.
- Total / success: 12,600 / 12,569.
- HTTP failures / timeouts: 31 / 31.
- Error rate / timeout rate: 0.25% / 0.25%.
- Client E2E P50 / P95 / P99 / max: 309ms / 366ms / 480ms / 1.269s.

## WebSocket

- Stage WS event counts: `auction_snapshot=1`, `time_sync=53,701`.
- Reconcile WS event counts: `auction_snapshot=300`, `time_sync=60,994`.
- `time_sync` count during stage: 53,701.
- `time_sync` P50 / P95 / P99 / max: 999.7ms / 1.059s / 1.113s / 1.823s.
- No `bid_success` or `user_outbid` events were observed during the stage.

## Prometheus

- Server HTTP P95 / P99 max: 77.969ms / 95.594ms.
- HTTP RPS max: 70.467/s.
- Auction bid RPS max: 0/s.
- Bid broadcast RPS max: 0/s.
- WS active max: 300.
- WS delivery RPS max: 300/s.
- WS write P95 max: 0.976ms.
- WS send queue depth P95 max: 4.5.
- `ws_time_sync_write_lag_p95` max: 4.813ms.
- `ws_time_sync_overwrite_rps`: no series.
- Backend restarts: 0.

## Reconcile And Cleanup

- Reconcile: item detail OK, ranking OK, room OK, connected WS 300, bid attempts 0.
- Cleanup: closed WS 300, cancel item OK, end room OK, delete users attempted 341.
- Post-run health: OK, MySQL OK, Redis OK.
- Post-run resource sample: backend 9m CPU / 37Mi, MySQL 13m / 670Mi, Redis 9m / 85Mi.
