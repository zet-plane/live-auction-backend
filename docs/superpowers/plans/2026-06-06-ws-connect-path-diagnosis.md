# WebSocket Connect Path Diagnosis Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Determine why online WebSocket connect P95/P99 remains around 5-6s after jittered split upgrade, and identify whether the remaining bottleneck is runner/client network, public ingress/TLS/Traefik, k3s node networking, or backend WebSocket accept/register path.

**Architecture:** This is a diagnosis plan, not an implementation plan. It builds progressively narrower measurement loops: external public WSS, SSH-host-side public WSS, in-cluster Service, and backend pod-local checks. Each phase changes only one path variable at a time and uses batch-scoped test data, read-only observability, and cleanup.

**Tech Stack:** Go performance runner, temporary Go/WebSocket probe, Kubernetes/k3s, Traefik, Prometheus, OpenTelemetry metrics, Linux socket/conntrack read-only checks, Markdown evidence.

---

## Context

Recent online strategy rerun showed:

- `jittered b30/i1s` only reduced WS Connect P95/P99 from `5.808s / 6.480s` to `5.506s / 6.172s`.
- `jittered b30/i1s` significantly improved control arrival P95/P99, client E2E tail, and timeout count.
- Backend `ws_time_sync_write_lag_p95` stayed around `4.6ms`.
- `ws_connection_lifecycle` is deployed and verified for accepted, closed normal, and closed replaced.

Interpretation:

- Jittered smooths post-connect pressure and stabilisation.
- It does not materially fix raw WebSocket connect tail.
- The next question is where the raw connect tail is introduced.

## Hypotheses

Ranked hypotheses to test:

1. Public internet / client-side route is the dominant source of connect tail.
   - Prediction: running the same public WSS connect probe from the online host is much faster than from the local runner.
2. Public ingress / Traefik / TLS upgrade path is the dominant source.
   - Prediction: in-cluster Service or pod-local WebSocket connect is materially faster than public WSS from either local or host.
3. Backend WebSocket ticket/accept/register path is slow under concurrent upgrade.
   - Prediction: pod-local or service-local connect still shows high tail, and `ws_connection_lifecycle` accepted timestamps/connection count lag behind dial attempts.
4. Node networking / conntrack / backlog pressure is the source.
   - Prediction: public and service-local paths show degradation at the same concurrency threshold, with conntrack/socket queue signals increasing.
5. Runner implementation or local load source scheduling inflates measured connect durations.
   - Prediction: a minimal WS-only probe reports much lower connect tail than the full performance runner under the same target/concurrency.

## File Structure

- Create: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/README.md`
  - Documents approved scope, commands, batch naming, and how to interpret results.
- Create: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go`
  - Minimal WS-only diagnosis runner. It creates one batch-scoped room/user set, issues tickets, measures phases, connects, closes, and cleans up.
- Create: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`
  - Stores脱敏 evidence summary only: batch IDs, aggregate timings, metric queries, cleanup summaries.
- Create: `docs/agent-testing/reports/20260606-ws-connect-path-diagnosis.md`
  - Final diagnosis report.
- Modify: `docs/superpowers/plans/2026-06-06-ws-connect-path-diagnosis.md`
  - Mark execution status after each task.

## Safety And Approval Boundary

This plan uses online dependencies. Before executing any online test command, the agent must request explicit approval with:

```text
Route: docs/agent-testing/README.md -> templates/protocol.md -> guides/runner.md -> guides/performance/README.md -> guides/performance/types.md -> guides/performance/online.md -> guides/performance/runner.md -> guides/environment.md
Dependencies: online HTTP, online WebSocket, online Prometheus/kubectl read-only checks.
Data: batch IDs prefixed with agent_ws_connect_path_20260606_.
Cleanup: close WS, end room, delete test users/merchant where supported.
Evidence: write redacted evidence and a diagnosis report; do not write online addresses, credentials, tokens, DSNs, or full WS query strings.
```

Do not modify online Kubernetes resources, restart workloads, scale deployments, patch images, or read secrets.

## Task 1: Establish Read-Only Online Baseline

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`

- [x] **Step 1: Request approval**

Ask the user to approve the online diagnosis scope described above.

Expected: user explicitly says approval is granted.

- [x] **Step 2: Verify deployment and health**

Run:

```bash
rtk ssh deploy@115.191.46.148 "kubectl rollout status deployment/live-auction-backend -n live-auction --timeout=60s && kubectl get deployment live-auction-backend -n live-auction -o jsonpath='{.spec.template.spec.containers[0].image}{\"\\n\"}{.status.readyReplicas}{\"/\"}{.status.replicas}{\" ready\\n\"}'"
rtk curl -fsS https://auction-api.kirac0on.com/health
```

Expected:

- rollout successful.
- image is the intended deployed commit.
- ready replicas are `1/1`.
- health returns code `0` and MySQL/Redis status `ok`.

- [x] **Step 3: Capture pre-test resource and error baseline**

Run:

```bash
rtk ssh deploy@115.191.46.148 "kubectl top pods -n live-auction | grep -E 'live-auction-backend|mysql|redis|otel-collector|prometheus'"
rtk ssh deploy@115.191.46.148 "kubectl logs -n live-auction deployment/live-auction-backend --since=15m | grep -iE '(^|[^[:alpha:]])(panic|fatal|oom|killed)([^[:alpha:]]|$)' | wc -l"
rtk ssh deploy@115.191.46.148 "kubectl exec -n live-auction deploy/prometheus -- wget -qO- 'http://127.0.0.1:9090/api/v1/query?query=sum%28ws_connection_active%29'"
```

Expected:

- backend restart remains 0.
- strict error marker count is 0.
- `ws_connection_active` is 0 or otherwise explained before test.

- [x] **Step 4: Append redacted baseline evidence**

Append to `evidence-redacted.md`:

```markdown
## Baseline

- Backend image:
- Ready replicas:
- Health:
- Strict log marker count:
- Key pod resources:
- ws_connection_active before tests:
```

Do not include credentials, tokens, DSNs, or full URLs beyond public endpoint labels already known in project-local docs.

## Task 2: Build Minimal WS-Only Connect Probe

**Files:**
- Create: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go`
- Create: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/README.md`

- [x] **Step 1: Create runner directory**

Run:

```bash
rtk mkdir -p docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606
```

Expected: directory exists.

- [x] **Step 2: Implement minimal probe**

Create `main.go` with these behaviours:

- Read env:
  - `PERF_BATCH_ID`
  - `PERF_BASE_URL`
  - `PERF_USER_COUNT`
  - `PERF_TARGET_WS`
  - `PERF_STREAM`
  - `PERF_CONNECT_CONCURRENCY`
  - `PERF_CONNECT_ROUNDS`
  - `PERF_CONNECT_TIMEOUT`
- Register one merchant and `PERF_USER_COUNT` users.
- Promote merchant, create/start room, create/publish/start item.
- For each round:
  - issue WS ticket per attempt.
  - measure ticket issue duration.
  - dial WSS.
  - measure dial duration.
  - optionally wait for first message for up to 3s.
  - close connection.
- Output aggregate P50/P95/P99 for:
  - ticket issue duration
  - WS dial duration
  - first message wait duration
  - total connect path duration
- Cleanup:
  - close all WS.
  - cancel item.
  - end room.
  - delete test users and merchant.

Output blocks:

```text
=== CONNECT_PROBE_PLAN
=== PREFLIGHT
=== ROUND: <n>
=== CLEANUP
=== SUMMARY
```

- [x] **Step 3: Write README**

Create `README.md`:

```markdown
# WS Connect Path Diagnosis Runner

This runner measures WebSocket connect path timing for batch-scoped online test data.

It records aggregate durations only and must not print tickets, authorization headers, DSNs, or full WebSocket query strings.

Required env:

- `PERF_BATCH_ID`
- `PERF_BASE_URL`
- `PERF_USER_COUNT`
- `PERF_TARGET_WS`
- `PERF_STREAM`
- `PERF_CONNECT_CONCURRENCY`
- `PERF_CONNECT_ROUNDS`
- `PERF_CONNECT_TIMEOUT`

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
  go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go
```
```

- [x] **Step 4: Run local compile test only**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606 -run '^$' -count=1
```

Expected: package compiles.

- [x] **Step 5: Commit runner**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606
rtk git commit -m "test: add ws connect path diagnosis runner"
```

## Task 3: Compare Full Runner Versus Minimal Probe From Local Machine

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`

- [x] **Step 1: Run minimal public WSS probe**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_connect_path_20260606_public_local_c8 \
  PERF_BASE_URL=https://auction-api.kirac0on.com \
  PERF_USER_COUNT=80 \
  PERF_TARGET_WS=80 \
  PERF_STREAM=control \
  PERF_CONNECT_CONCURRENCY=8 \
  PERF_CONNECT_ROUNDS=1 \
  PERF_CONNECT_TIMEOUT=15s \
  go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go
```

Expected:

- cleanup succeeds.
- output includes ticket P95/P99 and dial P95/P99.

- [x] **Step 2: Run full runner connection-only-ish comparison**

Run a low-QPS item-only split-stream run to compare with the existing runner:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_connect_path_20260606_full_runner_item_only \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=https://auction-api.kirac0on.com \
  PERF_STAGE_QPS=1 \
  PERF_STAGE_WS=80 \
  PERF_USER_COUNT=100 \
  PERF_REQUEST_MIX=item_only \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=immediate \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=220 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

- cleanup succeeds.
- compare full-runner WS connect P95/P99 against minimal probe dial P95/P99.

- [x] **Step 3: Interpret local probe comparison**

Decision:

- If minimal probe dial P95/P99 is also around 5-6s, the full runner is not the main source.
- If minimal probe is much faster, inspect full runner scheduling, target planning, and data setup interference before blaming infrastructure.

Append result to `evidence-redacted.md`.

## Task 4: Compare Public WSS From Online Host

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`

- [x] **Step 1: Copy probe to `/private/tmp` or run through repo command from local if available**

Preferred: run the committed probe locally against public WSS first. If local/public remains slow, run the same binary from the online host using a temporary copy that contains no secrets.

Do not copy credentials or kubeconfig.

- [x] **Step 2: Run public WSS probe from online host**

Run from online host with the same public endpoint:

```bash
rtk ssh deploy@115.191.46.148 "cd /home/deploy/live-auction-backend && GOCACHE=/tmp/live-auction-go-cache PERF_BATCH_ID=agent_ws_connect_path_20260606_public_host_c8 PERF_BASE_URL=https://auction-api.kirac0on.com PERF_USER_COUNT=80 PERF_TARGET_WS=80 PERF_STREAM=control PERF_CONNECT_CONCURRENCY=8 PERF_CONNECT_ROUNDS=1 PERF_CONNECT_TIMEOUT=15s go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go"
```

Expected:

- cleanup succeeds.
- host-side public WSS timings are captured.

- [x] **Step 3: Interpret local versus host public WSS**

Decision:

- If host public WSS is much faster than local public WSS, client/network path is likely dominant.
- If host public WSS is also slow, public ingress/TLS/Traefik or backend path remains suspect.

Append result to `evidence-redacted.md`.

## Task 5: Compare In-Cluster Service Path

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`

- [x] **Step 1: Run probe from online host against service path**

Use service entry if reachable from host through port-forward or in-cluster execution. Prefer a temporary Kubernetes Job only if explicitly approved; otherwise use `kubectl port-forward` to backend service on the online host.

Run:

```bash
rtk ssh deploy@115.191.46.148 "kubectl -n live-auction port-forward svc/live-auction-backend 18088:8088 --address 127.0.0.1 >/tmp/ws-connect-pf.log 2>&1 & echo \$!"
```

Then:

```bash
rtk ssh deploy@115.191.46.148 "cd /home/deploy/live-auction-backend && GOCACHE=/tmp/live-auction-go-cache PERF_BATCH_ID=agent_ws_connect_path_20260606_service_pf_c8 PERF_BASE_URL=http://127.0.0.1:18088 PERF_USER_COUNT=80 PERF_TARGET_WS=80 PERF_STREAM=control PERF_CONNECT_CONCURRENCY=8 PERF_CONNECT_ROUNDS=1 PERF_CONNECT_TIMEOUT=15s go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go"
```

Stop the remote port-forward PID after the run.

Expected:

- service path probe cleanup succeeds.
- service path connect P95/P99 captured.

- [x] **Step 2: Interpret service path**

Decision:

- If service path is much faster than public WSS, suspect public ingress/TLS/Traefik/network.
- If service path is also slow, suspect backend accept/register path or node/service networking.

Append result to `evidence-redacted.md`.

## Task 6: Concurrency Threshold Sweep

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/evidence-redacted.md`

- [x] **Step 1: Sweep public WSS concurrency**

Run minimal probe for concurrency levels `1, 4, 8, 16, 32` with target WS `80`:

```bash
for c in 1 4 8 16 32; do
  rtk env GOCACHE=/tmp/live-auction-go-cache \
    PERF_BATCH_ID=agent_ws_connect_path_20260606_public_local_c${c} \
    PERF_BASE_URL=https://auction-api.kirac0on.com \
    PERF_USER_COUNT=100 \
    PERF_TARGET_WS=80 \
    PERF_STREAM=control \
    PERF_CONNECT_CONCURRENCY=${c} \
    PERF_CONNECT_ROUNDS=1 \
    PERF_CONNECT_TIMEOUT=15s \
    go run docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606/main.go
done
```

Expected:

- cleanup succeeds for each batch.
- connect P95/P99 curve by concurrency is captured.

- [x] **Step 2: Capture read-only node/ingress indicators during sweep**

Before and after the sweep, run:

```bash
rtk ssh deploy@115.191.46.148 "kubectl top pods -n live-auction | grep -E 'traefik|live-auction-backend|mysql|redis|otel-collector|prometheus' || true"
rtk ssh deploy@115.191.46.148 "kubectl get pods -A | grep -E 'traefik|live-auction-backend'"
rtk ssh deploy@115.191.46.148 "kubectl logs -n live-auction deployment/live-auction-backend --since=10m | grep -iE '(^|[^[:alpha:]])(panic|fatal|oom|killed)([^[:alpha:]]|$)' | wc -l"
```

Expected:

- no restarts.
- strict error marker 0.

- [x] **Step 3: Interpret concurrency threshold**

Decision:

- If tail increases sharply only at higher concurrency, the bottleneck is connection burst handling.
- If tail is high even at concurrency 1, suspect network/TLS baseline latency or probe measurement issue.
- If service path has no tail but public path does, focus on ingress/TLS/public route.

Append result to `evidence-redacted.md`.

## Task 7: Write Diagnosis Report

**Files:**
- Create: `docs/agent-testing/reports/20260606-ws-connect-path-diagnosis.md`

- [x] **Step 1: Write report**

Create report with:

```markdown
# 测试报告：ws connect path diagnosis

## 基本信息

- 测试目标：
- 测试类型：
- 测试时间：
- 执行 agent：
- 读取文档：
- 线上地址脱敏说明：

## 结果矩阵

| Path | Source | Concurrency | Target WS | Ticket P95 | Ticket P99 | Dial P95 | Dial P99 | First Msg P95 | First Msg P99 | Cleanup |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |

## 分层判断

## Prometheus / lifecycle 证据

## 资源和日志复核

## 结论

## 下一步建议

## 清理结果
```

- [x] **Step 2: State bottleneck classification**

Use one of:

- `client_or_public_network_likely`
- `public_ingress_tls_likely`
- `service_or_backend_likely`
- `runner_measurement_likely`
- `inconclusive`

Include evidence for why.

- [x] **Step 3: Commit evidence and report**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_ws_connect_path_diagnosis_20260606 docs/agent-testing/reports/20260606-ws-connect-path-diagnosis.md docs/superpowers/plans/2026-06-06-ws-connect-path-diagnosis.md
rtk git commit -m "test: diagnose ws connect path"
```

## Task 8: Decision Gate For Multi-Instance Test

**Files:**
- Modify only if writing a follow-up plan:
  - `docs/superpowers/plans/YYYY-MM-DD-ws-multi-instance-validation.md`

- [x] **Step 1: Decide whether multi-instance is justified**

Proceed to multi-instance only if report classification is `service_or_backend_likely` and evidence shows:

- public and service-local paths both have high connect tail, or
- backend accepted lifecycle lags behind dial attempts, and
- MySQL/Redis are not bottlenecks, and
- backend resource or socket/accept behaviour suggests one pod is limiting.

Do not proceed to multi-instance if classification is `client_or_public_network_likely` or `public_ingress_tls_likely`.

- [x] **Step 2: Write follow-up plan if justified**

Decision: not justified. Report classification is `client_or_public_network_likely`; no multi-instance follow-up plan was written.

If justified, write a separate plan for:

- 2 backend replicas.
- session/ticket compatibility check.
- WS lifecycle per pod.
- active connection distribution.
- same strategy rerun under jittered.
- rollback conditions.

Expected: separate plan, not an ad hoc online change.
