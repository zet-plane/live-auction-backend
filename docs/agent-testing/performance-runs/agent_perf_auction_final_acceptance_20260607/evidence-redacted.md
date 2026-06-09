# Evidence Redacted: Auction Final Acceptance

## Status

- Batch ID: `agent_perf_auction_final_acceptance_20260607`
- Execution status: preflight
- Sensitive values: omitted

## Approval

Approved in conversation by user response: "可以".

Approved defaults:

- max_qps: 1100
- max_physical_ws: 9000
- max_duration: 90 minutes
- allowed_commands: read-only kubectl, read-only Prometheus/log queries, approved runner, batch-scoped data prep/reconcile/cleanup
- cleanup: only this batch
- public path check: allowed as separate public experience conclusion

## Preflight

- Backend ready replicas: `3/3`.
- Backend pods: 3 running, restart baseline `0` each.
- Backend image summary: runtime image checked, exact online value omitted from report if needed.
- Service-path readiness: `/readyz` returned HTTP 200 with MySQL and Redis ok.
- Prometheus readiness: ready.
- Resource baseline:
  - backend pods around `5-6m CPU / 15-17Mi`.
  - MySQL around `10m / 869Mi`.
  - Redis around `13m / 389Mi`.
  - node around `5% CPU / 67% memory`.
- Strict log marker baseline: `0` for fatal/panic/OOMKilled/killed expression. A coarse `oom` grep matched `room` SQL text and was treated as false-positive.
- Runner build: local Linux amd64 binary built and uploaded to remote `/tmp`.

## Stage Results

### smoke: service path 50 QPS / 200 physical WS

- Batch: `agent_perf_auction_final_acceptance_20260607_smoke`
- Entry: service path, exact address omitted.
- Config:
  - `PERF_STAGE_QPS=50`
  - `PERF_STAGE_WS=100`
  - `PERF_USER_COUNT=140`
  - `PERF_WS_STREAM_MODE=control_market`
  - `PERF_WS_UPGRADE_MODE=jittered`
- Result:
  - target QPS `50.00`, actual QPS `49.99`.
  - target logical WS `100`; connected physical WS `200`.
  - control WS `100`; market WS `100`.
  - WS connect failures `0`.
  - WS connect P50/P95/P99/max: `4.945ms / 25.973ms / 28.249ms / 31.836ms`.
  - total HTTP requests `8999`.
  - success `8999`.
  - HTTP failures `0`.
  - business failures `0`.
  - timeouts `0`.
  - client E2E P50/P95/P99/max: `1.450ms / 2.248ms / 3.137ms / 9.385ms`.
  - status codes: `200=8999`.
  - business codes: `0=8999`.
  - WS event counts: `bid_success=146200`, `time_sync=11400`, `user_outbid=5150`.
  - control time sync arrival delay P50/P95/P99: `1.952ms / 3.215ms / 4.005ms`.
  - control time sync interval P50/P95/P99: `1.998s / 2.001s / 2.002s`.

### qps300_ws3000: service path 300 QPS / 3000 physical WS

- Batch: `agent_perf_auction_final_acceptance_20260607_qps300_ws3000`
- Entry: service path, exact address omitted.
- Config:
  - `PERF_STAGE_QPS=300`
  - `PERF_STAGE_WS=1500`
  - `PERF_USER_COUNT=1600`
  - `PERF_WS_STREAM_MODE=control_market`
  - `PERF_WS_UPGRADE_MODE=jittered`
- Result:
  - target QPS `300.00`, actual QPS `280.80`.
  - actual / target ratio: `93.6%`; healthy probe, but below the `95%` claimed-capacity threshold.
  - target logical WS `1500`; connected physical WS `3000`.
  - control WS `1500`; market WS `1500`.
  - WS connect failures `0`.
  - WS connect P50/P95/P99/max: `4.658ms / 10.799ms / 25.131ms / 35.118ms`.
  - total HTTP requests `50547`.
  - success `46058`.
  - HTTP failures `4`.
  - business failures `0`.
  - expected business rejects `4485`, all `40003 price too low`.
  - timeouts `4`.
  - error rate `0.01%`.
  - timeout rate `0.01%`.
  - client E2E P50/P95/P99/max: `1.202ms / 25.131ms / 39.492ms / 95.082ms`.
  - status codes: `200=46058`, `400=4485`.
  - business codes: `0=46058`, `40003=4485`.
  - WS event counts: `bid_success=5487005`, `time_sync=193500`, `user_outbid=33736`.
  - control time sync arrival delay P50/P95/P99: `15.228ms / 40.095ms / 59.800ms`.
  - control time sync interval P50/P95/P99: `1.018s / 2.020s / 2.043s`.

### qps500_ws3000: service path 500 QPS / 3000 physical WS

- Batch: `agent_perf_auction_final_acceptance_20260607_qps500_ws3000`
- Entry: service path, exact address omitted.
- Config:
  - `PERF_STAGE_QPS=500`
  - `PERF_STAGE_WS=1500`
  - `PERF_USER_COUNT=1600`
  - `PERF_WS_STREAM_MODE=control_market`
  - `PERF_WS_UPGRADE_MODE=jittered`
- Result:
  - target QPS `500.00`, actual QPS `447.30`.
  - actual / target ratio: `89.5%`; service-side healthy, but below the `95%` claimed-capacity threshold.
  - target logical WS `1500`; connected physical WS `3000`.
  - control WS `1500`; market WS `1500`.
  - WS connect failures `0`.
  - WS connect P50/P95/P99/max: `4.595ms / 9.552ms / 20.496ms / 35.202ms`.
  - total HTTP requests `80515`.
  - success `69760`.
  - HTTP failures `0`.
  - business failures `0`.
  - expected business rejects `10755`, all `40003 price too low`.
  - timeouts `0`.
  - client E2E P50/P95/P99/max: `1.306ms / 26.940ms / 43.094ms / 99.245ms`.
  - status codes: `200=69760`, `400=10755`.
  - business codes: `0=69760`, `40003=10755`.
  - WS event counts: `bid_success=6117000`, `time_sync=196500`, `user_outbid=50402`.
  - control time sync arrival delay P50/P95/P99: `16.281ms / 42.527ms / 59.702ms`.
  - control time sync interval P50/P95/P99: `1.021s / 2.013s / 2.029s`.

### qps700_ws3000: service path 700 QPS / 3000 physical WS

- Batch: `agent_perf_auction_final_acceptance_20260607_qps700_ws3000`
- Entry: service path, exact address omitted.
- Config:
  - `PERF_STAGE_QPS=700`
  - `PERF_STAGE_WS=1500`
  - `PERF_USER_COUNT=1600`
  - `PERF_WS_STREAM_MODE=control_market`
  - `PERF_WS_UPGRADE_MODE=jittered`
- Result:
  - target QPS `700.00`, actual QPS `602.71`.
  - actual / target ratio: `86.1%`; service-side healthy, but below the `95%` claimed-capacity threshold.
  - target logical WS `1500`; connected physical WS `3000`.
  - control WS `1500`; market WS `1500`.
  - WS connect failures `0`.
  - WS connect P50/P95/P99/max: `4.760ms / 9.983ms / 21.991ms / 34.499ms`.
  - total HTTP requests `108489`.
  - success `89866`.
  - HTTP failures `9`.
  - business failures `0`.
  - expected business rejects `18614`, all `40003 price too low`.
  - timeouts `9`.
  - error rate `0.01%`.
  - timeout rate `0.01%`.
  - client E2E P50/P95/P99/max: `1.705ms / 28.492ms / 47.027ms / 114.186ms`.
  - status codes: `200=89866`, `400=18614`.
  - business codes: `0=89866`, `40003=18614`.
  - WS event counts: `bid_success=6469150`, `time_sync=195000`, `user_outbid=63991`.
  - control time sync arrival delay P50/P95/P99: `17.873ms / 54.435ms / 70.748ms`.
  - control time sync interval P50/P95/P99: `1.024s / 2.026s / 2.057s`.

### qps300_ws6000: service path 300 QPS / 6000 physical WS

- Batch: `agent_perf_auction_final_acceptance_20260607_qps300_ws6000`
- Entry: service path, exact address omitted.
- Config:
  - `PERF_STAGE_QPS=300`
  - `PERF_STAGE_WS=3000`
  - `PERF_USER_COUNT=3100`
  - `PERF_WS_STREAM_MODE=control_market`
  - `PERF_WS_UPGRADE_MODE=jittered`
- Result:
  - target QPS `300.00`, actual QPS `250.63`.
  - actual / target ratio: `83.5%`; treated as WS scaling probe, not claimed HTTP capacity.
  - target logical WS `3000`; connected physical WS `6000`.
  - control WS `3000`; market WS `3000`.
  - WS connect failures `0`.
  - WS connect P50/P95/P99/max: `5.149ms / 11.697ms / 21.646ms / 65.354ms`.
  - total HTTP requests `45114`.
  - success `33990`.
  - HTTP failures `2`.
  - business failures `0`.
  - expected business rejects `11122`, all `40003 price too low`.
  - timeouts `2`.
  - client E2E P50/P95/P99/max: `12.026ms / 72.142ms / 105.694ms / 247.003ms`.
  - status codes: `200=33990`, `400=11122`.
  - business codes: `0=33990`, `40003=11122`.
  - WS event counts: `bid_success=10503762`, `time_sync=408000`, `user_outbid=24177`.
  - control time sync arrival delay P50/P95/P99: `36.992ms / 99.150ms / 136.051ms`.
  - control time sync interval P50/P95/P99: `1.041s / 2.027s / 2.067s`.

## Observability

- smoke Prometheus:
  - server HTTP P95 max `93.449ms`.
  - server HTTP P99 max `98.690ms`.
  - HTTP RPS max `50.889/s`.
  - bid RPS max `40/s`.
  - DB operation RPS max `31.432/s`.
  - ws active max `200`.
  - ws delivery RPS max `817.778/s`.
  - ws write P95 max `0.957ms`.
  - ws send queue depth P95 max `4.5`.
  - ws time sync write lag P95 max `1.805ms`.
  - backend restarts max `0`.
- Post-smoke resource sample:
  - backend pods around `6-7m CPU / 24-29Mi`.
  - MySQL around `9m / 870Mi`.
  - Redis around `11m / 390Mi`.
- qps300_ws3000 Prometheus:
  - server HTTP P95 max `97.333ms`.
  - server HTTP P99 max `99.467ms`.
  - HTTP RPS max `345.265/s`.
  - bid RPS max `230.467/s`.
  - DB operation RPS max `92.644/s`.
  - ws active max `3000`.
  - ws delivery RPS max `31008.756/s`.
  - ws write P95 max `0.978ms`.
  - ws send queue depth P95 max `4.5`.
  - ws time sync write lag P95 max `23.382ms`.
  - backend restarts max `0`.
  - qps300_ws3000 resource samples:
  - during setup/stage backend pods reached around `388-400m CPU / 67-81Mi`.
  - post-cleanup backend pods around `6-7m CPU / 61-66Mi`.
  - Redis post-cleanup around `236m / 396Mi`.
- qps500_ws3000 Prometheus:
  - server HTTP P95 max `97.337ms`.
  - server HTTP P99 max `99.467ms`.
  - HTTP RPS max `465.791/s`.
  - bid RPS max `363.285/s`.
  - DB operation RPS max `95.016/s`.
  - ws active max `3000`.
  - ws delivery RPS max `34351.906/s`.
  - ws write P95 max `29.946ms`.
  - ws send queue depth P95 max `4.501`.
  - ws time sync write lag P95 max `43.750ms`.
  - backend restarts max `0`.
- qps500_ws3000 host sample:
  - load source CPU `67.22%`.
  - receive throughput about `30.84 MB/s`.
  - transmit throughput about `7.41 MB/s`.
- qps700_ws3000 Prometheus:
  - server HTTP P95 max `97.337ms`.
  - server HTTP P99 max `99.467ms`.
  - HTTP RPS max `608.400/s`.
  - bid RPS max `482.511/s`.
  - DB operation RPS max `95.133/s`.
  - ws active max `3000`.
  - ws delivery RPS max `36099.667/s`.
  - ws write P95 max `9.987ms`.
  - ws send queue depth P95 max `4.511`.
  - ws time sync write lag P95 max `28.515ms`.
  - backend restarts max `0`.
- qps700_ws3000 host sample:
  - load source CPU `75.80%`.
  - receive throughput about `33.27 MB/s`.
  - transmit throughput about `8.56 MB/s`.
- qps300_ws6000 Prometheus:
  - server HTTP P95 max `97.333ms`.
  - server HTTP P99 max `99.467ms`.
  - HTTP RPS max `391.711/s`.
  - bid RPS max `200.667/s`.
  - DB operation RPS max `158.778/s`.
  - ws active max `6000`.
  - ws delivery RPS max `58274.911/s`.
  - ws write P95 max `4.359ms`.
  - ws send queue depth P95 max `4.502`.
  - ws time sync write lag P95 max `46.558ms`.
  - backend restarts max `0`.
- qps300_ws6000 host sample:
  - load source CPU `82.14%`.
  - receive throughput about `49.92 MB/s`.
  - transmit throughput about `9.32 MB/s`.
- Final post-test resource sample:
  - backend pods around `6-7m CPU / 76-86Mi`.
  - MySQL around `9m / 869Mi`.
  - Redis around `11m / 424Mi`.
  - no remote runner process remained.
  - backend pod restarts remained `0`.
  - strict fatal/panic/OOMKilled/killed log markers over test window: `0`.

## Reconcile

- smoke reconcile: item detail ok, ranking ok, room ok, WS connected `200`, bid attempts `7199`.
- qps300_ws3000 reconcile: item detail ok, ranking ok, room ok, WS connected `3000`, bid attempts `40439`.
- qps500_ws3000 reconcile: item detail ok, ranking ok, room ok, WS connected `3000`, bid attempts `64413`.
- qps700_ws3000 reconcile: item detail ok, ranking ok, room ok, WS connected `3000`, bid attempts `86791`.
- qps300_ws6000 reconcile: item detail ok, ranking ok, room ok, WS connected `6000`, bid attempts `36092`.

## Cleanup

- smoke cleanup: `closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=141`.
- qps300_ws3000 cleanup: `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601`.
- qps500_ws3000 cleanup: `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601`.
- qps700_ws3000 cleanup: `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601`.
- qps300_ws6000 cleanup: `closed_ws=6000 cancel_item=ok end_room=ok delete_users_attempted=3101`.

## Conclusion

- Current status: smoke, qps300_ws3000, qps500_ws3000, qps700_ws3000 and qps300_ws6000 completed and cleaned up. Service-side metrics remain healthy up to observed `~603 RPS / 3000 physical WS` and `6000 physical WS` probe, but actual QPS is below the 95% target threshold for claimed capacity. Load-source CPU rises to `82.14%` in the 6000 WS probe, so testing stopped before 9000 WS to avoid measuring only single-source runner limits.
