# Evidence Redacted: Public Path Endpoint Split

## Status

- Batch ID: `agent_perf_auction_public_path_20260607_endpoint_300qps_800ws_v2`
- Environment: `single_source_public_path_remote_host_endpoint_split`
- Entry: public HTTPS/WSS domain, exact address omitted.
- Stage: `300 QPS / 400 logical WS / 800 physical WSS / 3 min`
- Request mix: `core_bid_80_ranking_10_item_10`
- Cleanup: `closed_ws=800 cancel_item=ok end_room=ok delete_users_attempted=421`

## Stage Summary

- Target QPS: `300.00`
- Actual QPS: `298.59`
- Total HTTP requests: `53746`
- Success: `51604`
- HTTP failures: `1`
- Timeouts: `1`
- Business failures: `0`
- Expected business rejects: `2141`, all expected low-price rejects.
- Overall client E2E P50/P95/P99/max: `1.665ms / 10.393ms / 16.728ms / 43.264ms`

## Client E2E By Endpoint

| Endpoint | Total | Success | HTTP failures | Timeouts | Expected rejects | P50 | P95 | P99 | Max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `bid` | 42998 | 40856 | 1 | 1 | 2141 | 1.708ms | 10.716ms | 16.860ms | 43.264ms |
| `item_detail` | 5374 | 5374 | 0 | 0 | 0 | 1.211ms | 8.944ms | 15.719ms | 31.932ms |
| `ranking` | 5374 | 5374 | 0 | 0 | 0 | 1.306ms | 9.448ms | 15.446ms | 38.955ms |

## Server HTTP By Route

Prometheus route-filtered HTTP histograms, stage window max of 30s range samples.

| Route | RPS max | Server P95 max | Server P99 max |
| --- | ---: | ---: | ---: |
| `/api/v1/items/{item_id}/bids` | 239.022/s | 4.110ms | 7.678ms |
| `/api/v1/items/{item_id}/ranking` | 29.956/s | 2.883ms | 4.966ms |
| `/api/v1/items/{item_id}` | 29.911/s | 1.994ms | 4.795ms |

## WebSocket

- Physical WSS connected: `800`
- WSS connect failures: `0`
- WSS connect P50/P95/P99/max: `24.750ms / 43.481ms / 52.088ms / 71.989ms`
- `time_sync` count: `48400`
- control `time_sync` arrival delay P50/P95/P99: `8.857ms / 17.746ms / 21.675ms`
- WS write P95 max: `0.967ms`
- WS time sync write lag P95 max: `4.883ms`

## Backend / Redis / MySQL

- Backend restarts: `0`.
- Strict backend log marker count for `panic|fatal|oomkilled|killed`: `0`.
- Post-run backend pods: all running, restart `0`.
- Post-run resource sample:
  - backend pods: `7m/103Mi`, `105m/110Mi`, `20m/101Mi`.
  - MySQL: `9m/871Mi`.
  - Redis: `272m/434Mi`.

## Interpretation

- Per-route server-side HTTP latency is low; the previous global server HTTP P95/P99 around `97-99ms` should not be used as a business-endpoint conclusion.
- At this stage, public client E2E P99 is about `15-17ms` across bid, ranking, and item detail.
- Redis/MySQL do not show a latency bottleneck in the HTTP per-route metrics for this stage.
