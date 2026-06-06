# Performance Run: agent_perf_auction_20260606_skip_tls_70qps_500ws

This directory contains the runner asset used for the skip-TLS auction performance probe on 2026-06-06.

Formal evidence chain:

- Report: `../../reports/20260606-033413-auction-skip-tls-performance.md`.
- Redacted evidence summary: `evidence-redacted.md`.
- Runner source: `main.go`.
- Index entry: `../README.md`.

Run shape:

- Environment: `single_source_online`.
- Entrypoint: supplied through `PERF_BASE_URL`.
- Public TLS/WSS bypass: yes. The valid ramp was executed from the online host against the in-cluster backend service path.
- HTTP mix: `POST /api/v1/items/{item_id}/bids` 80%, `GET /api/v1/items/{item_id}/ranking` 10%, `GET /api/v1/items/{item_id}` 10%.
- WebSocket stream mode: `control_market`.
- Client E2E and `time_sync` arrival/interval latency: recorded as advisory only for this batch; not used as a hard stop.

Important runs:

- `agent_perf_auction_20260606_skip_tls_70qps_500ws`: local runner through SSH service tunnel, 70 QPS and 250 logical WS users, yielding 500 physical WS.
- `agent_perf_auction_20260606_remote_skip_tls_ramp_500qps_1000users`: remote-host runner against service path, ramping 150/300/500 QPS and 500/750/1000 logical WS users, yielding 1000/1500/2000 physical WS.

Capacity interpretation:

- Valid service-path evidence supports about `485 HTTP RPS / 388 bid RPS / 2000 active WS` with backend restarts `0` and strict `panic|fatal|oom|killed` markers `0`.
- The local SSH service-tunnel ramp is preserved as failure evidence only. It is not backend capacity evidence because the forwarded port dropped during the ramp.
- Public HTTPS/WSS latency is out of scope for this run. Use `../agent_perf_auction_20260606_public_domain_local/performance-plan.md` for the follow-up path.

Do not write production URLs, tokens, DSNs, Redis passwords, or reusable credentials into this directory.
