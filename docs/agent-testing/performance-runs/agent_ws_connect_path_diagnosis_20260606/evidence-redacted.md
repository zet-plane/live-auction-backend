# WS Connect Path Diagnosis Evidence

All entries are redacted. Do not add credentials, tokens, DSNs, full WebSocket query strings, or reusable secrets.

## Baseline

- Time: 2026-06-06 Asia/Shanghai.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:5359b9f3`.
- Ready replicas: `1/1`.
- Health: OK; MySQL and Redis status OK with sub-millisecond check latency.
- Strict log marker count over previous 15 minutes: `0`.
- Key pod resources: backend about `7m CPU / 48Mi`; MySQL about `8m CPU / 674Mi`; Redis about `10m CPU / 78Mi`; OTel Collector about `2m CPU / 81Mi`; Prometheus about `3m CPU / 237Mi`.
- `ws_connection_active` before tests: `0`.

