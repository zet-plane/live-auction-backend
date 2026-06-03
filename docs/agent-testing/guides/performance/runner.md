# Performance Runner 使用指南

本指南说明 agent 如何把性能压测代码落地为可复跑资产。它只定义 runner 生成、填写、执行、证据保留和清理边界；压测计划、授权、环境、停止条件、监控和报告规则仍以 `guides/performance/README.md` 为准。

## 何时使用 Performance Runner

使用 performance runner：

- 需要执行出价、WebSocket、房间列表或混合链路压测。
- 需要 smoke、阶梯加压、峰值保持或稳定性压测分阶段输出。
- 需要把压测代码保留到仓库中，供复跑、审计或后续调优。
- 需要固定 stdout 块，方便主 agent、monitor agent、recorder agent 和报告解析。

不使用 performance runner：

- 只做接口契约或状态一致性验证；使用 `guides/go-runner.md` 和 `templates/runner.go`。
- 只用外部压测平台且平台本身已经生成完整脚本、证据和报告。
- 未获得线上或线上等价依赖授权。

## 代码落地位置

Performance runner 不放在 `/tmp`。每次正式性能压测创建可追踪目录：

```text
docs/agent-testing/performance-runs/<batch_id>/
├── main.go
├── README.md
└── evidence-redacted.md
```

要求：

- `main.go` 来自 `docs/agent-testing/guides/performance/performance-runner.go`。
- `README.md` 记录如何复跑，但不得写线上地址、token、DSN、密码或可复用凭据。
- `evidence-redacted.md` 记录脱敏输出摘要、阶段结果、对账和清理结果。
- 代码保留，线上测试数据必须按 `batch_id` 清理或记录未清理原因。

## 创建步骤

### 1. 创建批次目录

```bash
rtk mkdir -p docs/agent-testing/performance-runs/<batch_id>
```

### 2. 复制模板

读取 `docs/agent-testing/guides/performance/performance-runner.go`，将内容复制到：

```text
docs/agent-testing/performance-runs/<batch_id>/main.go
```

### 3. 填写压测计划字段

在 `main.go` 中只填写非敏感内容：

- `Stages`：smoke、step load、peak hold、soak。
- `buildRequest`：按已批准计划构造请求。
- `classifyBusiness`：解析业务错误码。
- `isBusinessSuccess`：判断业务成功和可接受失败。
- `reconcile`：执行已批准的 HTTP / MySQL / Redis / WebSocket 抽样对账。
- `cleanup`：只清理本批次数据。

不得写入：

- 线上域名或完整地址。
- token、DSN、密码、Redis 密码。
- 真实用户手机号、支付信息或真实商品 ID。

### 4. 用环境变量运行

示例：

```bash
rtk env \
  PERF_BATCH_ID=agent_bid_load_20260601120000 \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=http://127.0.0.1:18080 \
  PERF_STOP_FILE=STOP \
  go run docs/agent-testing/performance-runs/<batch_id>/main.go
```

常用环境变量：

| 变量 | 说明 | 是否可写入报告 |
| --- | --- | --- |
| `PERF_BATCH_ID` | 本次压测批次 | 可以 |
| `PERF_ENVIRONMENT` | `local_smoke` / `single_source_online` / `staging_capacity` / `production_guarded` | 可以 |
| `PERF_BASE_URL` | 被测服务入口，线上地址必须脱敏 | 不写完整线上值 |
| `PERF_AUTH_TOKEN` | 临时测试 token | 禁止 |
| `PERF_PROMETHEUS_URL` | Prometheus 入口，线上地址必须脱敏 | 不写完整线上值 |
| `PERF_OBSERVABILITY_STEP` | Prometheus `query_range` 采样步长，例如 `30s` | 可以 |
| `PERF_STOP_FILE` | 本地 STOP 文件路径 | 可以 |
| `PERF_HUMAN_MONITOR` | 人工旁路监控者标识 | 可以 |
| `PERF_REQUEST_TIMEOUT` | 单请求超时，例如 `5s` | 可以 |
| `PERF_START_QPS` | 从指定 QPS 档开始复跑 | 可以 |
| `PERF_END_QPS` | 在指定 QPS 档结束，用于受控短验证 | 可以 |
| `PERF_WS_CONNECT_CONCURRENCY` | WebSocket 建连并发度；一用户一连接场景默认 `8`，避免入口层握手尖峰 | 可以 |
| `PERF_WS_CONNECT_TIMEOUT` | 单条 WebSocket 握手超时，例如 `15s` | 可以 |
| `PERF_WS_CONNECT_MAX_ATTEMPTS` | WebSocket 建连最大尝试次数；应高于目标 WS 数 | 可以 |

## Port-forward 模式

线上 smoke 或受控小流量压测可以使用 `kubectl port-forward` 把线上服务转发到本机，再由 runner 请求本地端口。

记录方式：

```text
PerformanceEnvironment.kind: single_source_online
PerformanceEnvironment.entrypoint: kubectl_port_forward
LoadSource.kind: local_machine
PERF_BASE_URL: http://127.0.0.1:<local_port>
```

限制：

- 适合脚本验证、业务对账、小流量 guarded load。
- 不得单独作为线上峰值容量结论。
- 必须记录 port-forward 目标、时间窗口和关闭结果。
- 必须观察本机 CPU、网络和 port-forward 进程状态。
- 吞吐或延迟异常时，不能直接归因于后端服务。

## STOP 文件

runner 每次发请求前检查 `PERF_STOP_FILE`。创建该文件会让 runner 停止后续加压，并进入 `RECONCILE` 和 `CLEANUP`。

示例：

```bash
rtk touch docs/agent-testing/performance-runs/<batch_id>/STOP
```

触发 STOP 后，报告必须记录：

```text
停止来源：
停止时间：
当时阶段：
已完成请求数：
触发指标或人工原因：
后续清理结果：
```

## 输出格式

runner 输出块是证据契约，不应随意改名。

```text
=== PERF_PLAN
=== PREFLIGHT
=== STAGE: <stage_name>
=== STOP_EVENT
=== RECONCILE
=== CLEANUP
=== SUMMARY
```

每个 `STAGE` 至少输出：

```text
TARGET_QPS:
ACTUAL_QPS:
CONCURRENCY:
TOTAL:
SUCCESS:
HTTP_FAILURES:
BUSINESS_FAILS:
TIMEOUTS:
ERROR_RATE:
TIMEOUT_RATE:
BUSINESS_FAILURE_RATE:
CLIENT_E2E_P50:
CLIENT_E2E_P95:
CLIENT_E2E_P99:
CLIENT_E2E_MAX:
STATUS_CODES:
BUSINESS_CODES:
```

`CLIENT_E2E_P50/P95/P99/MAX` 是压测源看到的端到端耗时，包含本机调度、HTTP client、DNS/TLS、公网链路、Ingress、服务内处理和响应传回。它用于记录外部体验和网络噪声，不作为服务端接口性能优化的默认硬判停条件。服务端接口性能必须优先使用 Prometheus `server_http_p95/server_http_p99` 时间线判断。runner 还必须输出吞吐、成功/失败/超时计数、派生失败率、状态码分布和业务码分布；只输出延迟分位不能作为压测证据。

## 清理边界

Performance runner 的“代码不清理”，但“线上测试数据必须清理”。

必须保留：

- `main.go`。
- 脱敏复跑说明。
- 脱敏证据摘要。

必须清理或记录未清理原因：

- 本批次 Redis key。
- 本批次测试房间、拍品、出价、订单或关联数据。
- 临时 WebSocket 连接。
- 临时 port-forward 进程。
- 临时 token，或确认其已过期。

禁止：

- 删除 runner 代码来替代清理记录。
- 清理无 `batch_id`、无前缀或非本次创建的数据。
- `DROP DATABASE`、`DROP TABLE`、`TRUNCATE`、`FLUSHALL`、`FLUSHDB`。

## 报告衔接

| Runner 输出 | 报告字段 |
| --- | --- |
| `PERF_PLAN` | 压测目标、环境、压测源、阶段模型 |
| `PREFLIGHT` | preflight 证据 |
| `STAGE` | 每档压测结果 |
| `STOP_EVENT` | 停止条件触发情况 |
| `RECONCILE` | 业务状态抽样对账 |
| `CLEANUP` | 清理结果 |
| `SUMMARY` | 结论、风险和后续建议 |

报告只引用脱敏路径和摘要，不写入完整线上地址、token、DSN、密码或可复用凭据。
