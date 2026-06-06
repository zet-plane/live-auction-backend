# Performance Run: agent_perf_auction_20260606_public_domain_local

This directory contains the runner asset for the public-domain auction performance run.

Status:

- Prepared, not executed.
- Execution still requires explicit user approval under `docs/agent-testing/README.md` and `skills/agent-testing-gate/SKILL.md`.
- This run should not be cited as performance evidence until `evidence-redacted.md` is updated with execution results and a formal report is written.

Formal evidence chain:

- Plan: `performance-plan.md`.
- Runner readiness summary: `evidence-redacted.md`.
- Runner source: `main.go`.
- Index entry: `../README.md`.
- Previous service-path baseline: `../../reports/20260606-033413-auction-skip-tls-performance.md`.

Run shape:

- Environment: `single_source_online`.
- Source/path: local machine -> public HTTPS/WSS ingress -> backend.
- Entrypoint: supplied through `PERF_BASE_URL`; do not write the full online value here.
- HTTP mix: `POST /api/v1/items/{item_id}/bids` 80%, `GET /api/v1/items/{item_id}/ranking` 10%, `GET /api/v1/items/{item_id}` 10%.
- WebSocket stream mode: `control_market`.
- Evidence log target: `runner-output-redacted.log`.

Required runner metrics for this batch:

- per-route client latency, including bid route P95/P99.
- `bid_success` client arrival delay P50/P95/P99/max.
- `time_sync` client arrival delay and interval P50/P95/P99/max.
- WebSocket connect count, failures, errors, and P50/P95/P99/max.

Do not write production URLs, tokens, DSNs, Redis passwords, full WebSocket tickets, or reusable credentials into this directory.
