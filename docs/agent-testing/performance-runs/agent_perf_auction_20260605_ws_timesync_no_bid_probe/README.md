# agent_perf_auction_20260605_ws_timesync_no_bid_probe

## Purpose

Diagnostic WebSocket `time_sync` regression probe. This run keeps the same online entry, 300 WebSocket connections, and 70 QPS shape as the prior mixed bid run, but uses `item_only` requests so no `bid_success` fanout is produced during the measured stage.

## Replay

Run with environment variables rather than hard-coded addresses or tokens. Do not write the full online entrypoint, reusable credentials, or tokens into this directory.

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260605_ws_timesync_no_bid_probe \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=<redacted-online-entry> \
  PERF_PROMETHEUS_URL=<redacted-prometheus-entry> \
  PERF_OBSERVABILITY_STEP=30s \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=item_only \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260605_ws_timesync_no_bid_probe/main.go
```

## Evidence

See `evidence-redacted.md` and `docs/agent-testing/reports/20260605-015529-auction-ws-timesync-no-bid-regression.md`.
