# Redacted Evidence: agent_perf_auction_20260602_qps60_100

Status: completed, stopped by runner threshold.

Runtime secrets and full online addresses are intentionally omitted.

## Approval And Scope

- Approved by user conversation.
- Environment: `single_source_online`
- Entrypoint: public Ingress, full address omitted.
- Batch ID: `agent_perf_auction_20260602_qps60_100`
- Target stages: smoke, then 60/70/80/90/100 QPS with `160 users / 160 WS`.
- Actual executed stages: smoke and 60 QPS. Later stages skipped after STOP.

## Preflight

- Public health: HTTP 200.
- Health components: MySQL ok, Redis ok.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:91c9a696`
- Rollout: deployment successfully rolled out.
- Pods: core live-auction pods Running.
- Initial backend restart count: 0.
- Initial node sample: about 3% CPU / 48% memory.
- Initial backend pod sample: about 3m CPU / 40Mi.

## Runner

- Data created: one batch-scoped merchant, one room, one item, 160 batch-scoped users.
- WebSocket model: one test user per WebSocket connection.
- Request mix: room detail 20%, item detail 25%, ranking 25%, bid 15%, ws-ticket 5%, merchant room 5%, health 5%.
- Known runner caveat: WebSocket connections are established sequentially before each stage, so the gap before the 60 QPS stage was longer than the stage duration.

## Stage Results

| Stage | Target QPS | Actual QPS | Target WS | Connected WS | Total | Success | HTTP Failures | Business Fails | Expected Rejects | Timeouts | Error Rate | Timeout Rate | Client E2E P50 | Client E2E P95 | Client E2E P99 | Client E2E Max | Codes | Result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| smoke_10qps_20ws | 10 | 9.98 | 20 | 20 | 599 | 524 | 3 | 0 | 72 | 3 | 0.50% | 0.50% | 436ms | 865ms | 1.413s | 1.684s | 200=524, 40003=72 | completed |
| step_60qps_160ws | 60 | 45.08 | 160 | 160 | 8115 | 6910 | 98 | 0 | 1107 | 98 | 1.21% | 1.21% | 934ms | 2.600s | 3.660s | 7.095s | 200=6910, 40003=1107 | stopped |

## Skipped Stages

- step_70qps_160ws
- step_80qps_160ws
- step_90qps_160ws
- step_100qps_160ws

Reason: `step_60qps_160ws` triggered the previous client E2E P99 threshold. Newer runner versions treat client E2E latency as advisory for server-side optimization.

## Reconcile

- Item detail: HTTP 200.
- Ranking: HTTP 200.
- Room detail: HTTP 200.
- WebSocket connected at stop: 160.
- Bid attempts: 1290.

## Observability Summary

- During setup sample: node about 7% CPU / 49% memory; backend about 129m CPU / 40Mi.
- During running samples: backend remained low in sampled `kubectl top` windows; MySQL about 8-18m CPU / 536Mi; Redis about 7-8m CPU / 9Mi.
- Post cleanup sample: node about 3% CPU / 46% memory; backend about 3m CPU / 42Mi.
- Backend restart count after test: 0.
- Panic/OOM/fatal evidence: not observed in sampled logs.
- Log sampling caveat: command output included ordinary info lines, so logs are evidence for no obvious crash signal, not a precise 5xx count.

## Stop Event

- Stage: `step_60qps_160ws`
- Trigger: previous client E2E P99 threshold.
- Client E2E P99: 3.660s
- Additional note: actual QPS was 45.08 against target 60, so this run did not prove stable 60 QPS capacity.

## Cleanup

- Closed WebSocket connections: 160.
- Cancel item: ok.
- End room: ok.
- Delete users attempted: 161.
- Runner code retained: true.

## Follow-up Validation Attempt: 20260603 WS/Prometheus Runner Fix

- Batch ID: `agent_perf_auction_20260603_ws_prom_validation`.
- Scope: approved short validation only; intended stages were `smoke_10qps_20ws` and `step_60qps_160ws`.
- Runner guard: `PERF_END_QPS=60` was used to prevent execution beyond the approved 60 QPS stage.
- Public health preflight: HTTP 200.
- Prometheus preflight through temporary local port-forward: HTTP 200.
- Backend deployment summary: image tag `91c9a696`, `1/1 ready`, backend restart count 0.
- Result: no load stage executed. Setup stopped at merchant promotion with `PUT /api/v1/users/me` returning HTTP 500 / business code `50001`.
- Backend log evidence: `UpdateMe` emitted a GORM query error at the same timestamp as the runner setup failure.
- Cleanup: the single setup-created test account for this batch was deleted through authenticated API; delete returned HTTP 200 / business code `0`.
- Local fix prepared: `internal/app/user/dao.UpdateUser` now updates only mutable profile fields instead of full-row `Save`.
- Local verification passed: `go test -count=1 ./internal/app/user/... ./docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100`.
- Deployed fix: backend image tag `ba7098c5`; rollout completed with `1/1 ready`.
- Post-deploy smoke: register -> update `identity=merchant` -> query -> delete all returned HTTP 200 / business code `0`.
- Runner follow-up issue: long validation batch IDs made merchant display name exceed the `users.name` 64 char limit. Runner now uses compact merchant display names.
- Validation3 with `PERF_WS_CONNECT_CONCURRENCY=32`: `60 QPS / 160 WS` reached actual QPS 58.99, HTTP P99 554ms, but only `79/160` WS connected; WS failures were later diagnosed as handshake EOF.
- WS diagnostic with `PERF_WS_CONNECT_CONCURRENCY=32`: `WS_CONNECTED=45`, `WS_CONNECT_FAILS=275`, `WS_CONNECT_ERRORS={"dial:EOF":275}`.
- WS diagnostic with `PERF_WS_CONNECT_CONCURRENCY=8`: `WS_CONNECTED=160`, `WS_CONNECT_FAILS=0`, actual QPS 58.99, HTTP P99 583ms, cleanup closed 160 WS and deleted 161 users.
- Runner default changed to `PERF_WS_CONNECT_CONCURRENCY=8` to avoid ingress/backend handshake bursts while preserving parallel one-user-one-WS setup.

## Follow-up Full Staircase Attempt: 20260603 QPS 60-100

- Batch ID: `agent_perf_auction_20260603_qps60_100_full`.
- Scope: approved full staircase attempt for `60/70/80/90/100 QPS`, each configured for 3 minutes, with `160 users / 160 WS`.
- Runner config: `PERF_START_QPS=60`, `PERF_END_QPS=100`, `PERF_WS_CONNECT_CONCURRENCY=8`, `PERF_OBSERVABILITY_STEP=30s`.
- Preflight: public health HTTP 200, Prometheus readiness HTTP 200, STOP file absent.
- Setup: created batch-scoped merchant, room, item, and 160 users.
- Stage executed: `step_60qps_160ws`.
- Result: stopped by runner threshold at 60 QPS; later 70/80/90/100 stages were not executed.
- WebSocket result: `WS_CONNECTED=160`, `WS_CONNECT_FAILS=1`, `WS_CONNECT_ERRORS={"dial:EOF":1}`.
- HTTP result: actual QPS 55.51, total 9992, success 8496, HTTP failures/timeouts 124, business failures 0, expected business rejects 1372.
- Latency result from runner client: client E2E P50 381ms, P95 433ms, P99 5.601s, max 10.002s.
- Stop event: previous runner returned `STOP_REASON=client_e2e_p99_gt_2s`; newer runner versions do not use client E2E P99 as the default hard stop for server-side interface performance tests.
- Prometheus timeline: `server_http_p95` max 0.004250s, `server_http_p99` max 0.005600s, `http_rps` max 69.022222, `auction_bid_rps` max 9, `lua_result_rps` max 9, `db_operation_rps` max 97.2, `ws_active` max 160, `backend_restarts` max 0.
- Reconcile: item detail ok, ranking ok, room ok, `ws_connected=160`, `bid_attempts=1470`.
- Cleanup: `closed_ws=160 cancel_item=ok end_room=ok delete_users_attempted=161`.
- Post-test backend check: rollout healthy, app container restart 0, backend about 4m CPU / 48Mi, MySQL about 9m CPU / 539Mi, Redis about 8m CPU / 10Mi.
- Log check: no panic/OOM/fatal/500-style backend error in filtered sample; only ordinary 404 noise appeared.
- Monitor subagent conclusion: backend resource, restart, and Prometheus service latency stayed healthy; the observed issue was client E2E P99/timeouts.
