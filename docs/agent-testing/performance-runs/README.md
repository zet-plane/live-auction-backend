# Performance Run Evidence Index

This directory stores reusable performance-run assets for `live-auction-backend`.
Each run directory should keep the runner source, a redacted evidence summary, and
enough context to replay or interpret the run without exposing online endpoints,
tokens, DSNs, Redis credentials, full WebSocket tickets, or reusable credentials.

## 2026-06-06 Evidence Set

| Run | Status | Formal report | Evidence summary | Runner / plan | Use for |
| --- | --- | --- | --- | --- | --- |
| `agent_perf_auction_20260606_skip_tls_70qps_500ws` | Executed; service-path capacity evidence | `../reports/20260606-033413-auction-skip-tls-performance.md` | `agent_perf_auction_20260606_skip_tls_70qps_500ws/evidence-redacted.md` | `agent_perf_auction_20260606_skip_tls_70qps_500ws/main.go` | skip-TLS baseline and remote service-path capacity regression |
| `agent_perf_auction_20260606_public_domain_local` | Prepared; not executed in this evidence set | none yet | `agent_perf_auction_20260606_public_domain_local/evidence-redacted.md` | `agent_perf_auction_20260606_public_domain_local/main.go`, `agent_perf_auction_20260606_public_domain_local/performance-plan.md` | future public HTTPS/WSS user-path validation |

## Current Conclusions

- Service-path capacity evidence is formalized by
  `docs/agent-testing/reports/20260606-033413-auction-skip-tls-performance.md`.
- The service-path report supports a backend-side capacity statement of about
  `485 HTTP RPS / 388 bid RPS / 2000 active WS` under `single_source_online`,
  remote-host service-path, and `control_market` WebSocket splitting.
- The same report does not prove public HTTPS/WSS user-path latency. Use
  `agent_perf_auction_20260606_public_domain_local/performance-plan.md` for the
  next approved public-domain run.
- The local SSH service-tunnel ramp in the skip-TLS report is intentionally
  classified as invalid capacity evidence because the local forwarded port
  dropped during the ramp.

## Evidence Rules

- Keep raw online addresses and credentials out of this directory.
- Prefer `evidence-redacted.md` for durable summaries and keep long runner logs
  redacted or omitted unless they are needed for replay.
- When a run becomes a formal result, link it from this index and from the
  matching report under `docs/agent-testing/reports/`.
- If a runner is prepared but not executed, state that explicitly so it is not
  mistaken for passed evidence.
