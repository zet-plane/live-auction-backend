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
