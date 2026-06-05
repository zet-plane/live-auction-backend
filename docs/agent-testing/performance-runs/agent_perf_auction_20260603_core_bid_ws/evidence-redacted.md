# Redacted Evidence: agent_perf_auction_20260603_core_bid_ws

Status: completed, stopped by runner threshold.

Runtime secrets and full online addresses are intentionally omitted.

## Approval And Scope

- Approved by user conversation.
- Environment: `single_source_online`.
- Entrypoint: online service, full address omitted.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:ba7098c5`.
- HTTP mix: bids 80%, ranking 10%, item detail 10%.
- WebSocket target: same-room connections per stage, max 300.
- `time_sync`: record per-stage receive counts and interval summary.

## Preflight

- Public health before runner: HTTP 200.
- Runner preflight health inside sandbox failed due sandbox DNS/localhost restrictions; rerun outside sandbox was approved.
- Runner preflight outside sandbox: Prometheus readiness HTTP 200; public health timed out at runner 10s timeout, but setup continued and service accepted test data creation.
- Core pods were Running.
- Initial backend restart count: 0.
- Initial resource sample: backend about 3m CPU / 53Mi; node about 4% CPU / 55% memory.

## Runner

- Runner path: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`.
- Local verification: `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws` passed.
- Data created: one batch-scoped merchant, one room, one item, and 320 users.
- HTTP request mix: bids 80%, ranking 10%, item detail 10%.
- WebSocket model: one test user per same-room WebSocket connection.

## Stage Results

| Stage | Target QPS | Actual QPS | Target WS | Connected WS | Total | Success | HTTP Failures | Business Fails | Expected Rejects | Timeouts | Error Rate | Timeout Rate | Client P95 | Client P99 | time_sync count | time_sync P95 | time_sync P99 | Prom server P99 max | Prom bid RPS max | Prom DB ops max | Result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| smoke_10qps_20ws | 10 | 10.00 | 20 | 20 | 1800 | 1752 | 8 | 0 | 40 | 8 | 0.44% | 0.44% | 944ms | 1.201s | 3600 | 1.266s | 1.445s | unavailable | unavailable | unavailable | completed |
| step_30qps_60ws | 30 | 29.77 | 60 | 60 | 5358 | 4744 | 26 | 0 | 588 | 26 | 0.49% | 0.49% | 1.196s | 1.562s | 10850 | 1.311s | 1.598s | unavailable | unavailable | unavailable | completed |
| step_50qps_100ws | 50 | 49.99 | 100 | 100 | 8999 | 7502 | 32 | 0 | 1465 | 32 | 0.36% | 0.36% | 1.160s | 1.423s | 17977 | 1.394s | 1.858s | 24ms | 40.13 | 168.09 | completed |
| step_70qps_140ws | 70 | 67.01 | 140 | 140 | 12062 | 8985 | 48 | 1 | 3028 | 48 | 0.41% | 0.40% | 1.447s | 1.902s | 21615 | 2.193s | 2.946s | 41ms | 57.22 | 229.71 | completed with warning |
| step_100qps_200ws | 100 | 88.34 | 200 | 200 | 15901 | 12333 | 229 | 0 | 3339 | 229 | 1.44% | 1.44% | 1.914s | 3.527s | 17318 | 4.602s | 13.248s | 136ms | 80.00 | 327.29 | stopped |

## Stop Event

- Stage: `step_100qps_200ws`.
- Reason: `time_sync_missing_or_low_rate`.
- WebSocket active metric during stage: max 201, last 42.
- `time_sync` P95 interval: 4.602s.
- `time_sync` P99 interval: 13.248s.
- Runner actual QPS fell to 88.34 against target 100.
- Backend restart count remained 0.

## Observability Summary

- 50 QPS resource sample: backend about 253m CPU / 49Mi; MySQL about 77m CPU / 548Mi; Redis about 19m CPU / 13Mi.
- Post-test resource sample: backend about 5m CPU / 52Mi; MySQL about 14m CPU / 558Mi; Redis about 8m CPU / 16Mi; node about 5% CPU / 50% memory.
- Backend restart count after test: 0.
- Logs showed expected `PlaceBid failed` warnings consistent with `40003 price too low` atomic bid conflicts; no sampled panic/OOM/fatal signal.
- Prometheus query_range was unstable for smoke and 30 QPS because the SSH tunnel timed out/refused; 50 QPS onward produced usable Prometheus summaries.

## Reconcile

- Item detail: HTTP 200.
- Ranking: HTTP 200.
- Room detail: HTTP 200.
- WebSocket connected at reconcile: 200.
- Bid attempts: 35296.
- Total observed WS events: `auction_snapshot=200`, `bid_success=2605007`, `time_sync=78323`, `user_outbid=8071`.

## Cleanup

- Closed WebSocket connections: 200.
- Cancel item: ok.
- End room: ok.
- Delete users attempted: 321.
- Cleanup caveat: runner records delete attempts, not per-user delete success counts.
- Prometheus SSH tunnel: closed after run.
- Runner code retained: true.

---

# Redacted Evidence: 2026-06-05 Task 9 Millisecond Auction Sync Regression

Status: completed, regression failed split-stream acceptance thresholds.

Runtime secrets and full online addresses are intentionally omitted.

## Approval And Scope

- Approved by user conversation: `批准执行 Task 9 线上回归`.
- Environment: `single_source_online`.
- Entrypoint: online service, full address omitted.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:916015f0`.
- Runner path: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`.
- HTTP mix: bids 80%, ranking 10%, item detail 10%.
- Stage override: `PERF_STAGE_QPS=70`, `PERF_STAGE_WS=300`, `PERF_USER_COUNT=340`.
- Compared stream modes: `all` and `control_market`.
- Prometheus was queried through a temporary local tunnel; tunnel was closed after the run.

## Preflight

- Online `/health`: HTTP 200 for both runs.
- Prometheus readiness: HTTP 200 for both runs.
- STOP file was absent before both runs and absent after final cleanup.
- Backend deployment rollout was successful.
- Backend pod stayed Running with restart count 0.
- Pre-run resource sample: node about 5% CPU / 62% memory; backend about 8m CPU / 14Mi; MySQL about 11m CPU / 671Mi; Redis about 11m CPU / 56Mi.

## Runner Results

| Batch | Stream mode | Target QPS | Actual QPS | Target WS | Connected WS | Control WS | Market WS | WS connect fails | Total | Success | HTTP failures | Business fails | Expected rejects | Timeouts | Error rate | Timeout rate | Client P95 | Client P99 | Result |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `agent_perf_auction_20260605_all_stream_regression` | `all` | 70 | 69.63 | 300 | 300 | 0 | 0 | 0 | 12533 | 10851 | 55 | 0 | 1627 | 55 | 0.44% | 0.44% | 1.317s | 1.969s | completed |
| `agent_perf_auction_20260605_split_stream_regression` | `control_market` | 70 | 68.61 | 300 | 600 | 300 | 300 | 31 | 12350 | 9188 | 39 | 0 | 3123 | 39 | 0.32% | 0.32% | 1.919s | 2.742s | completed, failed acceptance |

## WebSocket Timing

| Stream mode | time_sync count | time_sync P50 | time_sync P95 | time_sync P99 | control arrival P50 | control arrival P95 | control arrival P99 | control interval P95 | control interval P99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `all` | 53998 | 999ms | 1.174s | 1.464s | n/a | n/a | n/a | n/a | n/a |
| `control_market` | 53983 | 991ms | 1.922s | 3.069s | 493ms | 1.851s | 2.933s | 1.922s | 3.069s |

Task 9 split-stream acceptance expected:

- `CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95 < 150ms`: failed, observed 1.851s.
- `CONTROL_TIME_SYNC_ARRIVAL_DELAY_P99 < 300ms`: failed, observed 2.933s.
- `CONTROL_TIME_SYNC_INTERVAL_P95 < 1.05s`: failed, observed 1.922s.
- `CONTROL_TIME_SYNC_INTERVAL_P99 < 1.20s`: failed, observed 3.069s.
- `WS connect failures = 0`: failed, observed 31 `dial:EOF` attempts, although final control and market connected counts reached 300 each.

## Prometheus Summary

| Metric | all max/last | control_market max/last |
| --- | --- | --- |
| `server_http_p95` | max 84ms, last 4.9ms | max 82ms, last 18ms |
| `server_http_p99` | max 97ms, last 13ms | max 202ms, last 70ms |
| `http_rps` | max/last 70.91 | max 69.80, last 68.64 |
| `auction_bid_rps` | max/last 56.73 | max 55.84, last 54.91 |
| `ws_active` | max/last 300 | max/last 599 |
| `ws_write_p95` | max 0.992ms | max 0.974ms |
| `ws_time_sync_write_lag_p95` | max/last 4.979ms | max 4.792ms, last 4.698ms |
| `backend_restarts` | max/last 0 | max/last 0 |

Prometheus did not show server-side `time_sync` write lag as the bottleneck. The split-stream failure is observed as client-side control stream arrival/interval delay and connection retry evidence.

## Reconcile

- `all`: item detail HTTP 200, ranking HTTP 200, room HTTP 200, connected WS 300, bid attempts 10027, observed WS events included `auction_snapshot=300`, `bid_success=364800`, `time_sync=61201`, `user_outbid=7449`.
- `control_market`: item detail HTTP 200, ranking HTTP 200, room HTTP 200, connected WS 600, bid attempts 9880, observed WS events included `auction_snapshot=300`, `bid_success=300495`, `time_sync=72434`, `user_outbid=5961`.

## Monitor Summary

- Monitor subagent did not create STOP.
- Resource samples stayed below severe pressure:
  - sample 1: node about 5% CPU / 63% memory; backend about 13m CPU / 36Mi.
  - sample 2: node about 5% CPU / 58% memory; backend about 8m CPU / 38Mi.
  - sample 3: node about 10% CPU / 58% memory; backend about 182m CPU / 38Mi.
  - final sample: node about 4% CPU / 57% memory; backend about 8m CPU / 56Mi.
- Backend stayed Ready/Running with restart count 0.
- No backend panic/fatal/OOM stop signal was confirmed.
- No namespace Warning events were observed in the checked window.

## Cleanup

- `all`: closed WebSocket connections 300; cancel item ok; end room ok; delete users attempted 341.
- `control_market`: closed WebSocket connections 600; cancel item ok; end room ok; delete users attempted 341.
- Cleanup caveat: runner records delete attempts, not per-user delete success counts.
- Temporary Prometheus tunnel: closed.
- Monitor subagent: closed.
- Runner code retained: true.

---

# Redacted Evidence: 2026-06-05 Control Stream Diagnosis Matrix

Status: completed, diagnosis narrowed to baseline client-observed delay plus connection-count/fanout amplification.

Runtime secrets and full online addresses are intentionally omitted.

## Approval And Scope

- Approved by user conversation after the Task 9 failure diagnosis request.
- Environment: `single_source_online`.
- Entrypoint: online service, full address omitted.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:916015f0`.
- Runner path: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`.
- Stream mode for all diagnostic batches: `control_market`.
- Prometheus was queried through a temporary local tunnel; tunnel was closed after the run.
- Monitor subagent performed only read-only resource checks and did not create STOP.

## Matrix Results

| Batch | Mix | Target QPS | Target users | Physical WS | WS connect fails | Actual QPS | Control arrival P50 | Control arrival P95 | Control arrival P99 | Control interval P95 | Control interval P99 | `ws_time_sync_write_lag_p95` max | Result |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `agent_diag_auction_sync_20260605_cm_30qps_60ws` | bid/ranking/item | 30 | 60 | 120 | 0 | 30.00 | 131ms | 221ms | 429ms | 1.066s | 1.287s | 1.451ms | low pressure still above acceptance |
| `agent_diag_auction_sync_20260605_cm_70qps_100ws` | bid/ranking/item | 70 | 100 | 200 | 0 | 69.99 | 136ms | 220ms | 438ms | 1.080s | 1.299s | 2.055ms | similar to low pressure |
| `agent_diag_auction_sync_20260605_cm_70qps_200ws` | bid/ranking/item | 70 | 200 | 400 | 24 | 69.98 | 157ms | 287ms | 458ms | 1.139s | 1.291s | 4.215ms | connection-count amplification appears |
| `agent_diag_auction_sync_20260605_cm_70qps_300ws` | bid/ranking/item | 70 | 300 | 600 | 58 | 69.74 | 180ms | 503ms | 2.025s | 1.187s | 1.538s | 4.811ms | 600-WS scale amplifies delay and EOF |
| `agent_diag_auction_sync_20260605_cm_70qps_300ws_item_only` | item only | 70 | 300 | 600 | 32 | 69.97 | 147ms | 237ms | 417ms | 1.089s | 1.264s | 4.835ms | fanout removed, delay improves but still misses |

## Diagnosis

- Low pressure already misses the strict Task 9 `arrival P95 < 150ms` and `arrival P99 < 300ms` thresholds, so the failure is not solely caused by the 300-user/600-socket peak.
- Increasing HTTP QPS from 30 to 70 while keeping physical WS modest did not materially worsen control arrival delay.
- Increasing physical WebSocket count from 200 to 400 to 600 increased connect instability and tail delay. `dial:EOF` appeared at 400 and worsened at 600.
- Removing bid fanout at 600 physical WS improved control arrival from `503ms/2.025s` to `237ms/417ms`, so bid/market fanout is an amplifier.
- Server-side `ws_time_sync_write_lag_p95` remained low across all runs, so the observed control delay is after server-side time_sync write scheduling, or in client/entry/network/read-loop observation.

Current best explanation:

1. There is a baseline client-observed control delay/jitter around 200ms P95 under real online conditions.
2. Physical WebSocket count amplifies both connection instability and tail arrival delay.
3. Bid/market fanout further amplifies the tail under 600 physical connections.
4. Current evidence does not show backend resource exhaustion, backend restarts, or server-side time_sync write lag as the root cause.

## Cleanup

- All five batches completed runner cleanup.
- Closed WebSocket connections: 120, 200, 400, 600, and 600 respectively.
- Each batch cancelled its test item, ended its test room, and attempted deletion of its batch users.
- Cleanup caveat: runner records delete attempts, not per-user delete success counts.
- Temporary Prometheus tunnel: closed.
- Monitor subagent: closed.

---

# Redacted Evidence: 2026-06-05 Enhanced Control Stream Diagnosis

Status: completed, diagnosis narrowed further away from server write lag and runner read-loop processing.

Runtime secrets, full online addresses, WebSocket query strings, tickets, and reusable credentials are intentionally omitted.

## Approval And Scope

- Approved by user conversation for enhanced diagnostic rerun.
- Environment: `single_source_online`.
- Entrypoint: online service, full address omitted.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:916015f0`.
- Runner path: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`.
- Stream mode: `control_market`.
- Prometheus was queried through a temporary local tunnel; tunnel was closed after the run.
- Monitor subagent performed read-only checks only and was closed after the run.

## Runner Diagnostic Additions

- Added WebSocket connect duration percentiles.
- Added WebSocket read-process duration percentiles, measured after `ReadMessage` returns.
- Moved control arrival sampling to immediately after `ReadMessage`.
- Added best-effort host CPU/network sampling; current runner host reported `HOST_SAMPLE: not_supported`.

## Effective Runs

| Batch | Mix | Target QPS | Target users | Physical WS | WS connect fails | Actual QPS | Control arrival P50 | Control arrival P95 | Control arrival P99 | Control interval P95 | Control interval P99 | WS read process P95 | WS connect P95 | `ws_time_sync_write_lag_p95` max |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `agent_diag2_auction_sync_20260605_cm_70qps_300ws_retry` | bid/ranking/item | 70 | 300 | 600 | 70 | 69.90 | 156.625ms | 402.194ms | 593.952ms | 1.232s | 1.389s | 23.959µs | 5.285s | 4.729ms |
| `agent_diag2_auction_sync_20260605_cm_70qps_300ws_item_only_r2` | item only | 70 | 300 | 600 | 32 | 69.82 | 133.114ms | 408.093ms | 577.459ms | 1.241s | 1.352s | 35.458µs | 5.288s | 4.744ms |

## Prometheus And Resource Evidence

- Bid mix Prometheus: server HTTP P95 max 4.224ms, P99 max 24.4ms, `ws_write_p95` max 0.989ms, `ws_time_sync_write_lag_p95` max/last 4.729ms, backend restarts 0.
- Item-only Prometheus: server HTTP P95 max 2.873ms, P99 max 6.422ms, bid broadcast 0, `ws_write_p95` max 0.960ms, `ws_time_sync_write_lag_p95` max/last 4.744ms, backend restarts 0.
- Final node resource snapshot: about 3% CPU and 63% memory.
- Final pod snapshot: backend about 7m CPU and 56Mi memory; MySQL about 9m CPU and 673Mi; Redis about 10m CPU and 74Mi.
- Backend pod stayed Ready/Running with restart count 0.

## Log Evidence

- Strict marker count for `panic|fatal|oom|killed` with word-boundary style matching: 0.
- Earlier monitor STOP was a false positive: a loose case-insensitive `OOM` grep matched ordinary `room` path strings.
- One attempted item-only batch failed during setup with `register merchant: status=500 code=50001`; it did not enter load and was replaced by the successful `_item_only_r2` batch.

## Reconcile

- Bid mix: item detail HTTP 200, ranking HTTP 200, room HTTP 200, connected WS 600, bid attempts 10066, observed WS events included `auction_snapshot=300`, `bid_success=447600`, `time_sync=77244`, `user_outbid=8439`.
- Item-only: item detail HTTP 200, ranking HTTP 200, room HTTP 200, connected WS 600, bid attempts 0, observed WS events included `auction_snapshot=300`, `time_sync=73861`.

## Diagnosis

- Server-side `time_sync` write lag is not the observed bottleneck.
- Runner read-loop processing is not the observed bottleneck.
- The effective 600-WS bid mix and item-only reruns had similar control arrival and interval tails, while both had slow WS connect percentiles and `dial:EOF`.
- Current best explanation is connection/transport/entry-path jitter under 600 physical WebSocket connections, with bid fanout remaining a possible amplifier but not the dominant cause in this rerun.

## Cleanup

- Bid mix: closed WebSocket connections 600; cancel item ok; end room ok; delete users attempted 341.
- Item-only rerun: closed WebSocket connections 600; cancel item ok; end room ok; delete users attempted 341.
- Cleanup caveat: runner records delete attempts, not per-user delete success counts.
- Temporary Prometheus tunnel: closed.
- Monitor subagent: closed.

---

# Redacted Evidence: 2026-06-05 WebSocket Connection Stability Matrix

Status: completed. Runtime secrets, full online addresses, SSH target, WebSocket query strings, tickets, and reusable credentials are intentionally omitted.

## Approval And Scope

- Approved by user conversation for online regression matrix.
- Environment: `single_source_online`.
- Entrypoint: online service, full address omitted.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:916015f0`.
- Runner path: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`.
- Stream mode: `control_market`.
- Prometheus was queried through a temporary local tunnel.

## Matrix Summary

| Batch | Upgrade mode | Mix | Physical WS | Connect errors | Connect P95 | Connect P99 | Control arrival P95 | Control arrival P99 | Control interval P95 | Control interval P99 | `ws_time_sync_write_lag_p95` max |
| --- | --- | --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `agent_ws_conn_stability_20260605_immediate` | `immediate` | bid/ranking/item | 600 | `dial:EOF=46` | 5.291s | 12.196s | 851.406ms | 1.970s | 1.478s | 2.041s | 4.676ms |
| `agent_ws_conn_stability_20260605_jittered` | `jittered` | bid/ranking/item | 600 | `dial:EOF=1` | 2.719s | 3.665s | 1.035s | 1.663s | 1.656s | 2.198s | 4.813ms |
| `agent_ws_conn_stability_20260605_priority_jittered` | `priority_jittered` | bid/ranking/item | 600 | none | 2.594s | 3.233s | 15.403s | 30.487s | 1.784s | 2.669s | 4.822ms |
| `agent_ws_conn_stability_20260605_item_only_jittered` | `jittered` | item only | 600 | `dial_status:502=1` | 2.509s | 3.197s | 572.207ms | 1.051s | 1.382s | 1.739s | 4.827ms |

## Resource And Log Evidence

- Backend pod stayed Ready/Running with restart count 0.
- Prometheus `backend_restarts` max was 0 in all four batches.
- Strict backend log marker count for `panic|fatal|oom|killed`: 0.
- Final node resource snapshot: about 3% CPU and 64% memory.
- Final pod resource snapshot: backend about 7m CPU and 58Mi memory; MySQL about 9m CPU and 673Mi; Redis about 10m CPU and 83Mi.

## Reconcile And Cleanup

- All four batches completed runner reconcile: item detail HTTP 200, ranking HTTP 200, room HTTP 200, connected WS 600.
- Cleanup results:
  - `agent_ws_conn_stability_20260605_immediate`: closed WS 600, cancel item ok, end room ok, delete users attempted 341.
  - `agent_ws_conn_stability_20260605_jittered`: closed WS 600, cancel item ok, end room ok, delete users attempted 341.
  - `agent_ws_conn_stability_20260605_priority_jittered`: closed WS 600, cancel item ok, end room ok, delete users attempted 341.
  - `agent_ws_conn_stability_20260605_item_only_jittered`: closed WS 600, cancel item ok, end room ok, delete users attempted 341.
- Cleanup caveat: runner records delete attempts, not per-user delete success counts.

## Conclusion

- Jittered split upgrade materially improved connection stability versus immediate upgrade: `dial:EOF` dropped from 46 to 1 and Connect P99 dropped from 12.196s to 3.665s.
- Priority jittered had the best connection metrics, but its bid-mix control arrival P95/P99 was anomalously high and should be rerun before rollout.
- Server-side time sync write lag stayed below 5ms; backend resource/restart/log evidence does not point to backend write path exhaustion.
