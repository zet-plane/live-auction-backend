# Evidence Redacted: Public Path Limit Probe From 500 Users

## Status

- Parent batch: `agent_perf_auction_public_path_20260607_limit_500u`
- Environment: `single_source_public_path_remote_host_endpoint_split_limit`
- Entry: public HTTPS/WSS domain, exact address omitted.
- Request mix: `core_bid_80_ranking_10_item_10`
- WS mode: `control_market`
- Logical users per stage: `500`
- Physical WSS per stage: `1000`
- Stop reason: `1100 QPS` stage failed actual-QPS target ratio (`86.8% < 95%`).

## Stage Summary

| Stage | Target QPS | Actual QPS | Actual/Target | Physical WSS | HTTP failures | Timeouts | Unexpected business failures | Load CPU |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `500qps_1000wss` | 500 | 489.20 | 97.8% | 1000 | 2 | 2 | 0 | 65.64% |
| `700qps_1000wss` | 700 | 679.18 | 97.0% | 1000 | 11 | 11 | 1 unparsed | 73.60% |
| `900qps_1000wss_retry1` | 900 | 869.39 | 96.6% | 1000 | 2 | 2 | 0 | 79.82% |
| `1100qps_1000wss` | 1100 | 954.44 | 86.8% | 1000 | 1 | 1 | 0 | 83.02% |

## Client E2E By Endpoint

| Stage | Endpoint | P95 | P99 | Max |
| --- | --- | ---: | ---: | ---: |
| `500qps_1000wss` | `bid` | 15.089ms | 23.571ms | 56.156ms |
| `500qps_1000wss` | `item_detail` | 13.929ms | 21.930ms | 43.169ms |
| `500qps_1000wss` | `ranking` | 13.935ms | 22.113ms | 47.043ms |
| `700qps_1000wss` | `bid` | 18.457ms | 29.515ms | 92.990ms |
| `700qps_1000wss` | `item_detail` | 16.836ms | 27.637ms | 65.904ms |
| `700qps_1000wss` | `ranking` | 17.013ms | 27.318ms | 63.789ms |
| `900qps_1000wss_retry1` | `bid` | 23.192ms | 37.102ms | 133.469ms |
| `900qps_1000wss_retry1` | `item_detail` | 21.515ms | 34.141ms | 101.517ms |
| `900qps_1000wss_retry1` | `ranking` | 21.848ms | 35.109ms | 102.630ms |
| `1100qps_1000wss` | `bid` | 23.322ms | 37.359ms | 168.879ms |
| `1100qps_1000wss` | `item_detail` | 21.634ms | 34.592ms | 117.719ms |
| `1100qps_1000wss` | `ranking` | 21.845ms | 35.427ms | 105.190ms |

## Server HTTP By Route

Stage-window max of Prometheus route-filtered range samples.

| Stage | Route | RPS max | Server P95 max | Server P99 max |
| --- | --- | ---: | ---: | ---: |
| `500qps_1000wss` | `/api/v1/items/{item_id}/bids` | 391.689/s | 5.370ms | 9.983ms |
| `500qps_1000wss` | `/api/v1/items/{item_id}/ranking` | 49.067/s | 4.141ms | 9.205ms |
| `500qps_1000wss` | `/api/v1/items/{item_id}` | 48.933/s | 4.079ms | 8.359ms |
| `700qps_1000wss` | `/api/v1/items/{item_id}/bids` | 543.778/s | 8.236ms | 19.711ms |
| `700qps_1000wss` | `/api/v1/items/{item_id}/ranking` | 68.111/s | 4.232ms | 8.429ms |
| `700qps_1000wss` | `/api/v1/items/{item_id}` | 68.000/s | 4.377ms | 8.970ms |
| `900qps_1000wss_retry1` | `/api/v1/items/{item_id}/bids` | 695.244/s | 9.377ms | 20.857ms |
| `900qps_1000wss_retry1` | `/api/v1/items/{item_id}/ranking` | 87.511/s | 4.963ms | 9.911ms |
| `900qps_1000wss_retry1` | `/api/v1/items/{item_id}` | 87.089/s | 4.986ms | 9.764ms |
| `1100qps_1000wss` | `/api/v1/items/{item_id}/bids` | 764.067/s | 9.839ms | 22.309ms |
| `1100qps_1000wss` | `/api/v1/items/{item_id}/ranking` | 95.667/s | 7.469ms | 14.710ms |
| `1100qps_1000wss` | `/api/v1/items/{item_id}` | 95.756/s | 7.759ms | 14.740ms |

## WebSocket

| Stage | Connected physical WSS | Connect failures | Connect P95 | Connect P99 | Control time_sync arrival P95 | WS write P95 max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `500qps_1000wss` | 1000 | 0 | 59.652ms | 69.715ms | 24.365ms | 0.970ms |
| `700qps_1000wss` | 1000 | 0 | 61.394ms | 73.794ms | 28.363ms | 1.317ms |
| `900qps_1000wss_retry1` | 1000 | 0 | 63.055ms | 75.007ms | not separately persisted | 0.976ms |
| `1100qps_1000wss` | 1000 | 0 | 56.588ms | 69.786ms | 30.819ms | 0.985ms |

## Invalid / Cleanup Notes

- `agent_perf_auction_public_path_20260607_limit_500u_900qps_1000wss` was invalid because the SSH session closed before stage output and cleanup evidence.
- Cleanup-only was run for the invalid batch. It reported `merchant_login=err`; no valid stage conclusion was recorded from this batch.
- `900qps_1000wss_retry1` is the valid 900 QPS stage.

## Post-Run Health

- Backend pods: all `Running`, restart `0`.
- Strict backend log marker count for `panic|fatal|oomkilled|killed`: `0`.
- Post-run resource sample:
  - backend pods around `6-7m CPU / 95-106Mi`.
  - MySQL around `9m / 870Mi`.
  - Redis around `12m / 591Mi`.

## Interpretation

- Stable public single-source result for this load source: `900 target QPS / 500 logical users / 1000 physical WSS`, with actual `869.39 QPS` (`96.6%`) and healthy per-endpoint and per-route latency.
- First failed stage: `1100 target QPS / 500 logical users / 1000 physical WSS`, because actual QPS reached only `954.44` (`86.8%`).
- At the failed stage, server-side route latency stayed healthy and Redis/MySQL/backend did not show bottleneck symptoms. The limiting signal is more consistent with single-source public-path/load-source throughput than backend service or database saturation.
