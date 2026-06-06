# Performance Run: agent_perf_auction_arch_capacity_20260606170223

This directory contains the runner assets for the internal architecture capacity test approved on 2026-06-06.

Scope:

- Environment kind: `staging_capacity`.
- Entrypoint: in-cluster, service-path, or same-network backend path.
- Public HTTPS/WSS E2E experience is out of scope.
- HTTP mix: bid 80%, ranking 10%, item detail 10%.
- WebSocket mode: `control_market`, where each logical user opens one control and one market connection.

Key files:

- `performance-plan.md`: approved plan and execution boundaries.
- `main.go`: performance runner source copied from the prior service-path runner.
- `evidence-redacted.md`: redacted stage evidence, reconcile summary, cleanup summary and conclusion.

Do not write production URLs, tokens, DSNs, Redis passwords, reusable credentials, full tickets, or full WebSocket query strings into this directory.

