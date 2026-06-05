# WS Connect Path Diagnosis Runner

This runner measures WebSocket connect path timing for batch-scoped test data.

It creates one merchant, one room, one item, and a configured number of users; then each round issues WebSocket tickets, dials WebSocket connections, optionally waits up to 3s for the first message, closes connections, and prints aggregate timings.

It records aggregate durations only and must not print tickets, authorization headers, DSNs, or full WebSocket query strings. Base URLs are redacted in output.

Required env:

- `PERF_BATCH_ID`
- `PERF_BASE_URL`
- `PERF_USER_COUNT`
- `PERF_TARGET_WS`
- `PERF_STREAM`
- `PERF_CONNECT_CONCURRENCY`
- `PERF_CONNECT_ROUNDS`
- `PERF_CONNECT_TIMEOUT` (must be a positive Go duration, such as `15s`)

Optional env:

- `PERF_WAIT_FIRST_MESSAGE` (`true` by default; set `false` to isolate dial timing)
- `PERF_REQUEST_TIMEOUT` (`10s` by default; if set, must be positive)

Output blocks:

- `CONNECT_PROBE_PLAN`
- `PREFLIGHT`
- `ROUND`
- `CLEANUP`
- `SUMMARY`

Recommended smoke:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_connect_path_20260606_public_local_smoke \
  PERF_BASE_URL=https://<redacted-online-entry> \
  PERF_USER_COUNT=40 \
  PERF_TARGET_WS=40 \
  PERF_STREAM=control \
  PERF_CONNECT_CONCURRENCY=8 \
  PERF_CONNECT_ROUNDS=1 \
  PERF_CONNECT_TIMEOUT=15s \
  PERF_WAIT_FIRST_MESSAGE=false \
  go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go
```

Local compile check:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606 -run '^$' -count=1
```
