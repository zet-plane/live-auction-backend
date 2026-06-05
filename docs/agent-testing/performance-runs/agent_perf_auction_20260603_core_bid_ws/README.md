# Performance Run: agent_perf_auction_20260603_core_bid_ws

This directory contains the redacted, replayable runner asset for the approved core bid path performance probe.

Run shape:

- Environment: `single_source_online`
- Entrypoint: supplied through `PERF_BASE_URL`
- Prometheus: supplied through `PERF_PROMETHEUS_URL`
- Human monitor: supplied through `PERF_HUMAN_MONITOR`
- Batch ID: `agent_perf_auction_20260603_core_bid_ws`
- HTTP mix: `POST /api/v1/items/{item_id}/bids` 80%, `GET /api/v1/items/{item_id}/ranking` 10%, `GET /api/v1/items/{item_id}` 10%
- Stages: `10/20 WS`, `30/60 WS`, `50/100 WS`, `70/140 WS`, `100/200 WS`, `130/260 WS`, `150/300 WS`, each 3 minutes
- WebSocket: target connections are established before each stage and kept in the same room. `time_sync` count and interval summaries are recorded.
- Diagnostics: stage output includes WebSocket connect duration percentiles, WS read-loop processing duration percentiles, and host CPU/network samples when the load source exposes Linux `/proc` counters. Unsupported hosts print `HOST_SAMPLE: not_supported`.

Example:

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260603_core_bid_ws \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=<service-url> \
  PERF_PROMETHEUS_URL=<prometheus-url> \
  PERF_HUMAN_MONITOR=<human-monitor-name> \
  PERF_USER_COUNT=320 \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=700 \
  PERF_OBSERVABILITY_STEP=30s \
  PERF_STOP_FILE=docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/STOP \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

## WebSocket Upgrade Smoothing

The runner can smooth split-stream WebSocket upgrades so online comparison runs can separate immediate fan-out pressure from staged connection pressure.

- `PERF_WS_UPGRADE_MODE`: default `immediate`; values `immediate`, `jittered`, `priority_jittered`
- `PERF_WS_CONTROL_JITTER_MIN`: default `0`; minimum delay before control wave
- `PERF_WS_CONTROL_JITTER_MAX`: default `0`; reserved upper bound for random jitter; deterministic tests use minimum
- `PERF_WS_MARKET_JITTER_MIN`: default `0`; minimum delay before market wave
- `PERF_WS_MARKET_JITTER_MAX`: default `0`; reserved upper bound for random jitter; deterministic tests use minimum
- `PERF_WS_UPGRADE_BATCH_SIZE`: default `0`; users per connection wave; `0` means one wave
- `PERF_WS_UPGRADE_BATCH_INTERVAL`: default `0`; delay between waves of same stream

Recommended online comparison commands, after real-dependency approval, use the same batch shape and vary only upgrade mode:

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260603_core_bid_ws_immediate \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=immediate \
  PERF_BASE_URL=<service-url> \
  PERF_PROMETHEUS_URL=<prometheus-url> \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260603_core_bid_ws_jittered \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_BASE_URL=<service-url> \
  PERF_PROMETHEUS_URL=<prometheus-url> \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260603_core_bid_ws_priority_jittered \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=priority_jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_BASE_URL=<service-url> \
  PERF_PROMETHEUS_URL=<prometheus-url> \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Do not write production URLs, tokens, DSNs, Redis passwords, or reusable credentials into this directory.
