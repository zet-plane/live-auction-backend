# WS Connect Path Diagnosis Evidence

All entries are redacted. Do not add credentials, tokens, DSNs, full WebSocket query strings, or reusable secrets.

## Baseline

- Time: 2026-06-06 Asia/Shanghai, refreshed after explicit online approval.
- Backend image: `ghcr.io/zet-plane/live-auction-backend:5359b9f3`.
- Ready replicas: `1/1`.
- Health: OK; MySQL and Redis status OK with sub-millisecond check latency.
- Strict log marker count over previous 15 minutes: `0`.
- Key pod resources: backend about `7m CPU / 52Mi`; MySQL about `9m CPU / 674Mi`; Redis about `10m CPU / 78Mi`; OTel Collector about `2m CPU / 79Mi`; Prometheus about `3m CPU / 246Mi`.
- `ws_connection_active` before tests: `0`.

## Local Public WSS Comparison

- Minimal probe batch: `agent_ws_connect_path_20260606_public_local_c8`.
- Minimal probe source/path: local machine -> public HTTPS/WSS ingress.
- Minimal probe load: `users=80`, `target_ws=80`, `stream=control`, `connect_concurrency=8`, `rounds=1`, `connect_timeout=15s`.
- Minimal probe result: `success=80/80`, first message timeouts `0`, errors `{}`.
- Minimal probe ticket timing: P50 `284.857ms`, P95 `1.8493825s`, P99 `1.910111333s`, max `1.910111333s`.
- Minimal probe WS dial timing: P50 `4.726044917s`, P95 `5.5271745s`, P99 `5.766746708s`, max `5.766746708s`.
- Minimal probe first message wait: P50 `11.209µs`, P95 `21.197917ms`, P99 `50.681125ms`, max `50.681125ms`.
- Minimal probe total connect path: P50 `5.025978584s`, P95 `5.849663041s`, P99 `6.104261375s`, max `6.104261375s`.
- Minimal probe cleanup: closed WS `80`, cancel item OK, end room OK, deleted users `80/80`, delete merchant OK.
- Full runner comparison batch: `agent_ws_connect_path_20260606_full_runner_item_only`.
- Full runner source/path: local machine -> public HTTPS/WSS ingress.
- Full runner load: item-only low QPS, `stage_qps=1`, `stage_ws=80`, `stream_mode=control_market`, `connect_concurrency=8`, `connect_timeout=15s`.
- Full runner result: `WS_CONNECTED=160`, connect fails `0`, connect errors `{}`.
- Full runner WS connect timing: P50 `5.0058315s`, P95 `5.477145416s`, P99 `6.090593625s`, max `6.127999125s`.
- Full runner HTTP stage summary: total `180`, success `179`, HTTP failures `1`, timeouts `1`, business failures `0`, timeout rate `0.0056`.
- Full runner cleanup: closed WS `160`, cancel item OK, end room OK, delete users attempted `101`.
- Interpretation: minimal probe and full runner both show public WSS connect P95/P99 around `5-6s`; full runner scheduling is unlikely to be the primary source of the connect tail.

## Host Public WSS Comparison

- Remote execution note: the online host did not have a source checkout or Go toolchain at the plan path, so the probe was cross-compiled locally as a Linux x86_64 binary and copied as a temporary no-secret artifact under `/tmp`.
- Remote temporary artifact cleanup: probe binary and temporary directory removed after diagnosis.
- Host public batch: `agent_ws_connect_path_20260606_public_host_c8`.
- Host public source/path: online host -> public HTTPS/WSS ingress.
- Host public load: `users=80`, `target_ws=80`, `stream=control`, `connect_concurrency=8`, `rounds=1`, `connect_timeout=15s`.
- Host public result: `success=80/80`, first message timeouts `0`, errors `{}`.
- Host public ticket timing: P50 `198.845701ms`, P95 `248.594952ms`, P99 `308.545003ms`, max `308.545003ms`.
- Host public WS dial timing: P50 `416.045841ms`, P95 `443.213609ms`, P99 `475.874767ms`, max `475.874767ms`.
- Host public first message wait: P50 `7.892µs`, P95 `1.609025ms`, P99 `26.327701ms`, max `26.327701ms`.
- Host public total connect path: P50 `617.446398ms`, P95 `695.612558ms`, P99 `726.996709ms`, max `726.996709ms`.
- Host public cleanup: closed WS `80`, cancel item OK, end room OK, deleted users `80/80`, delete merchant OK.
- Interpretation: public WSS from the online host is materially faster than local public WSS. This points strongly at client/public network path as the dominant source of the local 5-6s connect tail.

## Service Port-Forward Path

- Service path batch: `agent_ws_connect_path_20260606_service_pf_c8`.
- Service source/path: online host -> `kubectl port-forward` to backend Service -> HTTP/WS over loopback.
- Port-forward cleanup: remote PID stopped; port-forward log tail showed normal connection handling only.
- Service path load: `users=80`, `target_ws=80`, `stream=control`, `connect_concurrency=8`, `rounds=1`, `connect_timeout=15s`.
- Service path result: `success=80/80`, first message timeouts `0`, errors `{}`.
- Service ticket timing: P50 `4.887042ms`, P95 `10.743811ms`, P99 `12.325609ms`, max `12.325609ms`.
- Service WS dial timing: P50 `7.050629ms`, P95 `11.229722ms`, P99 `13.714072ms`, max `13.714072ms`.
- Service first message wait: P50 `320.969µs`, P95 `2.857085ms`, P99 `3.405203ms`, max `3.405203ms`.
- Service total connect path: P50 `13.691841ms`, P95 `18.987053ms`, P99 `20.432785ms`, max `20.432785ms`.
- Service cleanup: closed WS `80`, cancel item OK, end room OK, deleted users `80/80`, delete merchant OK.
- Interpretation: service-local connect is very fast, so backend accept/register path and in-cluster service path are not the source of the 5-6s local public WSS tail.

## Local Public WSS Concurrency Sweep

- Sweep source/path: local machine -> public HTTPS/WSS ingress.
- Sweep batches: `agent_ws_connect_path_20260606_public_local_sweep_c1`, `c4`, `c8`, `c16`, `c32`.
- Sweep load per batch: `users=100`, `target_ws=80`, `stream=control`, `rounds=1`, `connect_timeout=15s`.

| Concurrency | Success | Ticket P95 | Ticket P99 | Dial P95 | Dial P99 | Total P95 | Total P99 | Cleanup |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 80/80 | `264.376292ms` | `339.48325ms` | `5.744712542s` | `5.819377459s` | `6.001154833s` | `6.08230425s` | OK |
| 4 | 80/80 | `329.247416ms` | `342.826291ms` | `4.949664458s` | `5.76373s` | `5.260143333s` | `6.076974375s` | OK |
| 8 | 80/80 | `298.954125ms` | `324.025167ms` | `5.748895375s` | `5.891515333s` | `6.056755833s` | `6.169647292s` | OK |
| 16 | 80/80 | `364.116042ms` | `424.859042ms` | `5.81768275s` | `5.968822875s` | `6.114004375s` | `6.257319208s` | OK |
| 32 | 80/80 | `396.304208ms` | `724.095542ms` | `5.857231334s` | `6.023220667s` | `6.201456875s` | `6.354627625s` | OK |

- Sweep errors: all batches reported `{}` for connect errors and `0` first-message timeouts.
- Pre-sweep resources: backend about `6m CPU / 50Mi`; MySQL about `9m CPU / 674Mi`; Redis about `10m CPU / 78Mi`; Prometheus about `3m CPU / 245Mi`.
- Post-sweep resources: backend about `8m CPU / 48Mi`; MySQL about `14m CPU / 674Mi`; Redis about `10m CPU / 78Mi`; Prometheus about `4m CPU / 281Mi`.
- Pod state after sweep: backend and Traefik pods running; backend restart count remained `0`.
- Strict backend log marker count after sweep: `0`.
- `ws_connection_active` after sweep: `0`.
- Interpretation: local public WSS connect tail is high even at concurrency `1` and remains consistently high through concurrency `32`; this does not look like a backend/service concurrency threshold. Combined with host-public and service-local results, the likely bottleneck is the local client/public network route to the public endpoint.
