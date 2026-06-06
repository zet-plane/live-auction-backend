# Evidence Redacted: skip-TLS auction performance

## Batch IDs

- `agent_perf_auction_20260606_skip_tls_70qps_500ws`
- `agent_perf_auction_20260606_skip_tls_ramp_500qps_1000users` (invalid: local SSH tunnel dropped)
- `agent_perf_auction_20260606_remote_skip_tls_ramp_500qps_1000users`

## Valid 70 QPS Stage

- Actual QPS: `69.72`
- Physical WS connected: `500` (`250 control + 250 market`)
- WS connect failures: `0`
- WS connect P95/P99/max: `244.767ms / 262.766ms / 276.987ms`
- Total requests: `12,550`
- Success: `10,979`
- HTTP failures / timeouts: `4 / 4`
- Expected business rejects: `1,567`
- Client E2E P95/P99/max: `116.036ms / 163.141ms / 542.653ms`
- Server HTTP P95/P99 max from Prometheus: `95.888ms / 99.178ms`
- WS active max: `500`
- WS delivery RPS max: `2,050/s`
- WS write P95 max: `0.969ms`
- time_sync write lag P95 max: `4.289ms`
- Reconcile: item detail OK, ranking OK, room OK, WS connected `500`
- Cleanup: closed WS `500`, cancel item OK, end room OK, delete users attempted `321`

## Invalid Local Ramp

The local SSH tunnel dropped during `150 QPS / 1000 physical WS`; results are invalid for backend capacity.

- Runner reported `connection refused` to local forwarded ports.
- Stop reason: `error_rate_gt_3_percent`.
- Cleanup after tunnel recovery: cancel item OK, end room OK, user delete OK `1100/1100`, merchant delete OK.

## Valid Remote Service-Path Ramp

The remote-host runner stdout did not fully return through SSH. Per-stage metrics below are reconstructed from Prometheus stable windows.

| Stage | Window CST | HTTP RPS avg/max | bid RPS avg/max | WS active | server P95 max | server P99 max | DB ops max | WS delivery max | WS write P95 max | time_sync write lag P95 max | status code RPS at end |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 150 QPS / 1000 physical WS | 04:10:25-04:12:25 | 150.00 / 150.00 | 120.00 / 120.00 | 1000 | 1.994ms | 6.747ms | 160.04/s | 5,155.93/s | 0.956ms | 6.084ms | 200=149.82/s, 400=0.18/s |
| 300 QPS / 1500 physical WS | 04:13:55-04:15:55 | 296.64 / 297.00 | 237.30 / 237.58 | 1500 | 3.786ms | 9.125ms | 307.04/s | 7,810.02/s | 0.959ms | 12.628ms | 200=292.60/s, 400=4.02/s |
| 500 QPS / 2000 physical WS | 04:17:25-04:18:55 | 485.06 / 485.56 | 388.04 / 388.44 | 2000 | 4.775ms | 17.276ms | 495.56/s | 10,471.09/s | 0.959ms | 16.563ms | 200=468.07/s, 400=17.11/s |

## Resource Samples

- 70 QPS stage peak sample: backend `166m / 53Mi`, MySQL `49m / 674Mi`, Redis `45m / 80Mi`.
- Remote ramp high sample: backend `707m / 128Mi`, MySQL `137m / 681Mi`, Redis `93m / 107Mi`.
- Node high sample: `31% CPU / 64% memory`.
- Final resource sample: backend `7m / 86Mi`, MySQL `10m / 681Mi`, Redis `10m / 127Mi`, node `4% CPU / 61% memory`.

## Safety Checks

- Backend restart count after runs: `0`.
- Strict `panic|fatal|oom|killed` log marker count: `0`.
- Remote temporary runner binary and temporary logs removed from `/tmp`.
- Residual public state check for remote ramp: test item is `cancelled`; room is `idle`, `current_item_id=""`, `online_count=0`, item queue length `0`.
