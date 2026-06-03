# Performance Run: agent_perf_auction_20260602155512

This directory contains the redacted, replayable runner asset for the approved online single-source performance probe.

Run shape:

- Environment: `single_source_online`
- Entrypoint: public Ingress, supplied through `PERF_BASE_URL`
- Human monitor: supplied through `PERF_HUMAN_MONITOR`
- Batch ID: `agent_perf_auction_20260602155512`
- Stages: `20 QPS / 40 WS / 1m`, `30 QPS / 60 WS / 2m`, `40 QPS / 80 WS / 3m`, `60 QPS / 120 WS / 5m`, `70 QPS / 160 WS / 5m`

Runner semantics:

- Bid requests first read item detail, then submit `current_price + bid_increment`. This approximates a frontend price view updated by WebSocket events before the user bids.
- `40003 price too low` is counted as `EXPECTED_BUSINESS_REJECTS`, not system error. It represents expected atomic bid conflict when concurrent bidders race the same price.

Example:

```bash
rtk env \
  PERF_BATCH_ID=agent_perf_auction_20260602155512 \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=<public-ingress-url> \
  PERF_HUMAN_MONITOR=<human-monitor-name> \
  PERF_STOP_FILE=docs/agent-testing/performance-runs/agent_perf_auction_20260602155512/STOP \
  PERF_EVIDENCE_PATH=docs/agent-testing/performance-runs/agent_perf_auction_20260602155512/evidence-redacted.md \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260602155512/main.go
```

Do not write production URLs, tokens, DSNs, Redis passwords, or reusable credentials into this directory.
