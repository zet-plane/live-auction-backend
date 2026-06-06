# Evidence Redacted: public-domain auction performance

## Status

- Batch ID: `agent_perf_auction_20260606_public_domain_local`
- Status: executed and stopped by threshold.
- Runner asset: `main.go`
- Plan: `performance-plan.md`

## Runner Readiness

- The runner compiles with `go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260606_public_domain_local`.
- Added per-route client latency output for `bid`, `ranking`, and `item_detail`.
- Added `bid_success` client arrival delay output.
- Existing `time_sync` arrival and interval output is retained.

## Execution Evidence

Full online URLs, tokens, DSNs, Redis credentials, full WebSocket tickets, and reusable credentials are omitted.

## Preflight

- Public health: HTTP 200; MySQL and Redis components reported ok.
- Backend image: commit-tagged image ending in `5359b9f3`.
- Baseline resources: backend about `8m / 103Mi`; MySQL about `10m / 681Mi`; Redis about `10m / 127Mi`; node about `4% CPU / 69% memory`.
- Backend restart count before and after: `0`.
- Strict `panic|fatal|OOM|killed` log marker count after run: `0`.
- Prometheus readiness: ok. Runner-side Prometheus timeline was not configured because no local Prometheus entrypoint was available.

## Setup Attempts

- Original batch failed in sandbox before load because DNS resolution for the public entry was blocked; no load was sent.
- Original batch external retry created partial setup and timed out at user registration 30; cleanup-only removed 31 users, 1 merchant, cancelled 1 item, and ended the room.
- Reusing the exact batch account after cleanup returned a registration 500, likely because deleted accounts are not immediately reusable. Execution switched to traceable sub-batches with the same parent prefix.

## Smoke: run3, 50 QPS, 100 logical / 200 physical WS

- Actual QPS: `47.84`.
- WS connected: `200` (`100 control + 100 market`); connect failures `0`.
- WS connect P50/P95/P99/max: `1.925s / 3.095s / 3.942s / 12.310s`.
- Total requests: `8,611`; success `6,265`; expected business rejects `2,257`.
- HTTP failures / timeouts: `89 / 89`; error rate `1.03%`; timeout rate `1.03%`.
- Client E2E P50/P95/P99/max: `888ms / 1.558s / 2.385s / 3.554s`.
- Bid route client P95/P99/max: `1.585s / 2.385s / 3.554s`.
- `bid_success` arrival P50/P95/P99/max: `233ms / 874ms / 1.359s / 12.419s`.
- `time_sync` interval P50/P95/P99/max: `999ms / 1.440s / 1.863s / 4.650s`.
- Reconcile: item detail ok, ranking ok, room ok, WS connected `200`.
- Cleanup: closed WS `200`, cancel item ok, end room ok, delete users attempted `121`.

## A Group: run4, fixed 400 logical / 800 physical WS

| Stage | Actual QPS | WS | HTTP fail rate | Timeout rate | Client P95 | Client P99 | Bid route P99 | bid_success P99 | time_sync interval P99 | Stop |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 150 target | 128.60 | 800 | 1.29% | 1.28% | 1.915s | 2.734s | 2.734s | 1.437s | 1.899s | no |
| 300 target | 209.17 | 800 | 1.91% | 1.91% | 2.708s | 3.829s | 3.828s | 1.573s | 1.943s | no, yellow |
| 500 target | 203.07 | 800 | 3.63% | 3.24% | 8.830s | 20.921s | 20.921s | 13.835s | 7.429s | yes, `error_rate_gt_3_percent` |

- A/150 WS connect P99/max: `4.179s / 30.002s`; one ticket timeout.
- A/500 `bid_success` arrival P95/P99/max: `7.225s / 13.835s / 57.511s`.
- A/500 status codes: `200=15,805`, `400=19,464`; business codes include `0=15,782`, expected `40003=19,445`, `unparsed=42`.
- A group cleanup: closed WS `800`, cancel item ok, end room ok, delete users attempted `421`.

## B Group: run5, fixed 300 target QPS

| Stage | Logical WS | Physical WS | Actual QPS | HTTP fail rate | Timeout rate | WS connect P99 | Client P95 | Client P99 | bid_success P99 | Stop |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| ws_400 | 400 | 800 | 225.79 | 1.71% | 1.71% | 14.892s | 2.087s | 3.964s | 1.394s | manual threshold stop requested |
| ws_600 | 600 | 1200 | 248.70 | 2.02% | 0.00% | 3.710s | 2.092s | 3.056s | 2.095s | stopped by STOP file after about 1 minute |

- B/ws_400 crossed the plan stop condition: public WSS connect P99 exceeded `10s` for the stage.
- STOP file was created after ws_400 output. Runner had already entered ws_600, so ws_600 is a partial stage and not a full 3-minute result.
- B group cleanup: closed WS `1200`, cancel item ok, end room ok, delete users attempted `1021`.

## Resource And Safety Samples

- Smoke resource sample: backend about `92m / 106Mi`; MySQL `44m / 681Mi`; Redis `20m / 129Mi`; node `12% CPU / 66% memory`.
- A/300 resource sample: backend about `293m / 113Mi`; MySQL `88m / 682Mi`; Redis `99m / 135Mi`; backend restart `0`.
- A/500 near-stop resource sample: backend had already fallen back to about `9m / 105Mi`; restart `0`.
- B/ws execution sample: backend about `250m / 126Mi`; MySQL `72m / 682Mi`; Redis `34m / 143Mi`.
- Final resource sample: backend about `7m / 103Mi`; MySQL `10m / 682Mi`; Redis `10m / 145Mi`.

## Cleanup Summary

- `agent_perf_auction_20260606_public_domain_local`: partial setup cleanup removed 31 users, 1 merchant, cancelled 1 item, ended 1 room.
- `agent_perf_auction_20260606_public_domain_local_run2`: invalid WS target run cleaned 240 WS, cancelled item, ended room, delete users attempted `121`.
- `agent_perf_auction_20260606_public_domain_local_run3`: smoke cleaned 200 WS, cancelled item, ended room, delete users attempted `121`.
- `agent_perf_auction_20260606_public_domain_local_run4`: A group cleaned 800 WS, cancelled item, ended room, delete users attempted `421`.
- `agent_perf_auction_20260606_public_domain_local_run5`: B group cleaned 1200 WS, cancelled item, ended room, delete users attempted `1021`.
