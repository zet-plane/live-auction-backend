# Performance Run: agent_perf_auction_20260602_qps60_100

This directory contains the redacted, replayable runner asset for the approved online single-source performance probe.

Run shape:

- Environment: `single_source_online`
- Entrypoint: public Ingress, supplied through `PERF_BASE_URL`
- Human monitor: supplied through `PERF_HUMAN_MONITOR`
- Batch ID: `agent_perf_auction_20260602_qps60_100`
- Stages: `smoke 10 QPS / 20 WS / 1m`, then `60`, `70`, `80`, `90`, `100 QPS / 160 WS / 3m`
- WebSocket model: target online users and WebSocket connections are one-to-one. The load stages use `160 users / 160 WS`.

Runner semantics:

- Bid requests first read item detail, then submit `current_price + bid_increment`. This approximates a frontend price view updated by WebSocket events before the user bids.
- `40003 price too low` is counted as `EXPECTED_BUSINESS_REJECTS`, not system error. It represents expected atomic bid conflict when concurrent bidders race the same price.
- WebSocket setup is parallel and bounded by `PERF_WS_CONNECT_CONCURRENCY`, `PERF_WS_CONNECT_TIMEOUT`, and `PERF_WS_CONNECT_MAX_ATTEMPTS`. Failed users are retried without assigning duplicate connections to successful users. The default connection concurrency is `8`; higher values may create a handshake burst that public ingress closes with EOF.
- Stage latency fields named `CLIENT_E2E_*` are measured from the runner and include client/network/Ingress/service time. They are advisory for server-side optimization. If `PERF_PROMETHEUS_URL` is supplied, each stage also prints a Prometheus `query_range` timeline for observed online metrics; `server_http_p95/server_http_p99` are the primary latency signals for service-side HTTP interface performance.

Example:

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260602_qps60_100 \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=<public-ingress-url> \
  PERF_HUMAN_MONITOR=<human-monitor-name> \
  PERF_PROMETHEUS_URL=<prometheus-url> \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=320 \
  PERF_OBSERVABILITY_STEP=30s \
  PERF_END_QPS=60 \
  PERF_STOP_FILE=docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100/STOP \
  PERF_EVIDENCE_PATH=docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100/evidence-redacted.md \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100/main.go
```

Do not write production URLs, tokens, DSNs, Redis passwords, or reusable credentials into this directory.
