# Redacted Evidence: agent_perf_auction_20260602155512

Status: completed, stopped by runner threshold.

Runtime secrets and full online addresses are intentionally omitted.

## Preflight

- Environment: `single_source_online`
- Entrypoint: public Ingress, full address omitted
- Human monitor: user
- Backend image: `ghcr.io/zet-plane/live-auction-backend:91c9a696`
- Rollout: deployment successfully rolled out
- Health: HTTP 200, MySQL ok, Redis ok
- Initial node: CPU 4%, memory 48%
- Initial backend pod: 2m CPU, 16Mi memory, restart count 0

## Runner

- Batch ID: `agent_perf_auction_20260602155512`
- Data created: one batch-scoped merchant, one room, one item, 160 batch-scoped users
- Request mix: room detail 20%, item detail 25%, ranking 25%, bid 15%, ws-ticket 5%, merchant room 5%, health 5%
- WebSocket model: sustained long connections per stage

## Stage Results

| Stage | Target QPS | Actual QPS | Target WS | Connected WS | Total | Success | HTTP Failures | Business Fails | Timeouts | Error Rate | Timeout Rate | P50 | P95 | P99 | Max | Codes | Result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| step_20qps_40ws | 20 | 19.98 | 40 | 40 | 1199 | 1189 | 7 | 3 | 7 | 0.83% | 0.58% | 386ms | 570ms | 694ms | 817ms | 200=1189, 40003=3 | completed |
| step_30qps_60ws | 30 | 30.00 | 60 | 60 | 3600 | 3517 | 15 | 68 | 15 | 2.31% | 0.42% | 598ms | 1.057s | 1.445s | 2.024s | 200=3518, 40003=67, unparsed=1 | completed |
| step_40qps_80ws | 40 | 39.99 | 80 | 80 | 7199 | 6946 | 20 | 233 | 20 | 3.51% | 0.28% | 775ms | 1.361s | 1.886s | 2.758s | 200=6946, 40003=233 | stopped |

## Reconcile

- Item detail: HTTP 200
- Ranking: HTTP 200
- Room detail: HTTP 200
- WebSocket connected at stop: 80
- Bid attempts: 1800

## Observability Summary

- During setup: backend 153m CPU / 19Mi, node CPU 8%, node memory 44%
- During 20 QPS: backend 18m CPU / 21Mi, backend restart count 0
- During 30 QPS: backend 53m CPU / 24Mi, backend restart count 0
- During 40 QPS: backend 58m CPU / 26Mi
- After cleanup: backend 2m CPU / 31Mi, node CPU 4%, node memory 44%, backend restart count 0
- HTTP 500 log count in sampled window: 0
- Panic/OOM/fatal log count in sampled window: 0
- `PlaceBid failed` log count in sampled window: 168

## Stop Event

- Stage: `step_40qps_80ws`
- Trigger: `error_rate_gt_3_percent`
- Main contributor: business code `40003 price too low`
- Note: this is a conservative runner stop because expected business rejections were counted into the aggregate error rate.

## Cleanup

- Closed WebSocket connections: 80
- Cancel item: ok
- End room: ok
- Delete users attempted: 161
- Runner code retained: true
