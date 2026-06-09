# 线上性能压测闭环协议

本指南定义 agent 如何在线上或线上等价环境执行受控性能压测。线上压测默认采用 **agent 主执行、人类旁路监控** 模式：agent 执行压测命令、`kubectl` 查询、监控查询、日志查询、业务对账和清理；人类监控者同时观察 Grafana、日志和告警。

线上压测结论必须同时满足：批准边界明确、压测源明确、监控可用、停止开关有效、业务对账完成、清理完成。缺任一项只能写 `inconclusive`。

## 1. 授权门

连接线上环境或线上等价真实依赖前，agent 必须输出正式压测计划并获得用户明确批准。批准内容至少包括：

- 目标服务、接口、模块或流程。
- 线上压测窗口，包含开始时间、结束时间和是否低峰。
- 最大压力边界，包含最大 QPS / TPS、最大并发、最大连接数和最大持续时长。
- 允许使用的线上命令范围。
- 压测源数量、规格、网络位置和出口摘要。
- 测试数据边界，包含批次 ID、测试用户、测试商家、测试房间和测试拍品。
- 停止条件、回滚或降级联系人、人工监控者。

未获得批准前，agent 不得创建线上测试数据、连接线上 Redis / MySQL、发起线上 HTTP / WebSocket 请求或启动压测工具。

## 2. 权限门

批准后，agent 只能执行计划允许的命令族。

默认允许：

- 只读 Kubernetes 检查：`rtk kubectl get`、`rtk kubectl describe`、`rtk kubectl logs`、`rtk kubectl top`。
- 临时观测通道：`rtk kubectl port-forward`，仅用于 Prometheus、Grafana、Loki、Tempo 或被测服务的测试窗口访问。
- Prometheus / Grafana / Loki / Tempo 查询。
- 压测工具命令。
- 只作用于本次测试批次的数据准备、对账和清理命令。

默认禁止，除非用户单独批准：

- 修改线上配置、镜像、Ingress、Service、Deployment 或 Secret。
- 扩缩容、重启、回滚、发布或删除线上 workload。
- 无条件 `DELETE`、清库、清表、`FLUSHALL`、`FLUSHDB` 或跨批次清理。
- 使用真实用户、真实支付信息或非本批次商品执行写请求。

## 3. 就绪门

正式发压前，agent 必须完成 preflight 并记录证据：

- 被测目标已明确到模块、流程、接口、WebSocket 路径或服务入口。
- 线上服务入口已在授权范围内完成只读可达性探测，且结果可用。
- 后端版本、commit 或镜像摘要。
- Pod 数、实例数、CPU / 内存 request 和 limit。
- 服务地址摘要、入口层、网关或负载均衡摘要。
- Prometheus / Grafana 指标可查。
- 日志可按测试窗口检索。
- MySQL、Redis、节点和应用指标可查。
- 压测源可用，且不会先于被测服务成为瓶颈。
- 测试批次数据已创建或确认存在，且全部可追踪到 `batch_id`。
- 临时测试 token 可用且不会写入报告。
- Smoke 小流量已通过，证明脚本、认证、数据和监控没有误打非测试数据。
- 人类监控者已确认正在旁路观察。

如果任一项缺失，agent 必须停止正式压测，输出阻塞项、已验证证据和建议处理方式。

如果线上服务入口不可达，agent 必须停止在环境阻塞；不得继续创建测试数据、落地 runner、执行 smoke、启动压测工具或把不可达记录为业务性能失败。

## 4. Port-forward 模式

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

## 5. 执行门

线上压测必须分档执行，不得跳过 smoke 后直接打到峰值。

推荐顺序：

```text
smoke -> step_load -> peak_hold -> soak（可选）
```

每档开始前，agent 必须输出本档目标压力、持续时间、停止条件和对账点。每档结束后，agent 必须输出 runner 结果、监控摘要、日志摘要和业务对账摘要。

## 6. 双通道监控门

agent 负责采集机器可读证据，人类监控者负责观察图形化大盘、告警和肉眼异常。人类监控者看到异常时可以随时发出停止指令；agent 收到停止指令后必须立即停止压测并进入收尾流程。

agent 每档至少采集：

- runner 输出。
- Prometheus 指标摘要。
- 客户端按接口端到端指标：每个 request mix endpoint 的 P50 / P95 / P99 / max、请求数、失败数、超时数、状态码和业务码。
- 服务侧按 route 指标：和 request mix endpoint 对齐的 HTTP route/method/status RPS、P50 / P95 / P99。
- 应用错误日志摘要。
- MySQL / Redis 指标摘要。
- 业务状态抽样对账结果。

## 7. 自动判停门

出现以下任一情况，agent 必须立即停止压测或要求人工立即停止：

- HTTP 5xx、超时率或业务错误率连续超过计划阈值。
- 客户端按接口 P99 或服务侧按 route P99 连续超过计划阈值。
- MySQL 或 Redis 出现明显 timeout、连接池耗尽、慢查询激增或锁等待激增。
- Pod restart、panic、OOM、goroutine 或内存持续异常上升。
- WebSocket 连接失败率、丢消息数或广播延迟超过计划阈值。
- 业务状态对账失败。
- 发现非测试用户、非测试商品或非本批次数据被影响。
- 人类监控者要求停止。

停止后不得继续加压。agent 必须记录触发条件、停止时间、已完成阶段、影响范围、当前业务状态和清理动作。

## 8. 对账门

线上压测不能只看吞吐和延迟。每个关键阶段后，agent 必须抽样验证业务状态；没有对账不得写 `passed`。

出价链路至少验证：

- HTTP 查询中的最高价、最高出价人和 `bid_count`。
- Redis item state 和 ranking。
- MySQL `bid_logs`。
- 业务错误码分布是否符合预期，例如低价失败、成交后出价失败、重复请求失败。

WebSocket 链路至少验证：

- 连接成功率、在线人数和连接索引。
- 广播消息数量、端到端延迟和丢消息数。
- 事件 payload 与 HTTP / Redis / MySQL 最终状态一致。

## 9. 收尾门

压测结束或中止后，agent 必须完成收尾：

- 停止压测进程。
- 关闭临时 port-forward。
- 断开临时 WebSocket 连接。
- 清理本批次 Redis key 和可安全清理的测试数据。
- 确认临时 token 已过期或撤销。
- 确认核心指标回落。
- 记录未清理项、未清理原因和人工处理建议。
- 保留 runner 代码和脱敏证据。
- 按 `reports/README.md` 写入或补充报告。
