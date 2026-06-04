# Redis 容灾与降级设计

## 背景

当前后端在多个层面依赖 Redis：

- 服务启动时会 `Ping` Redis；失败会导致 server 直接退出。
- 进行中的竞拍状态、出价排名、幂等、反狙击延时和结算协调都存放在 Redis，并通过 Lua 原子更新。
- 房间在线人数、房间商品队列、当前拍品指针和 WebSocket 在线状态使用 Redis 作为实时状态层。
- WebSocket ticket 通过 Redis 签发和消费。
- 部分读取路径已经能容忍 Redis 错误，并回退到 MySQL 或安全默认值。

这些依赖不应该共用同一种失败策略。Redis 故障不能让整个服务看起来已经死亡，但高价值竞拍写入也不能在实时权威状态不明时继续执行。

## 目标

1. Redis 不可用时，后端进程仍然保持存活。
2. MySQL 健康时，Redis 故障期间仍保留非竞拍或只读功能。
3. 当 Redis 状态不可用、落后或正在恢复时，暂停高风险竞拍写入，保护竞拍正确性。
4. 通过健康检查端点和指标清晰暴露 Redis 与竞拍降级状态。
5. 支持 Redis HA 故障切换，同时显式处理切换窗口、复制延迟和脑裂风险。

## 非目标

- 不替换 Redis Lua 作为实时竞拍串行化点。
- 第一版不把 MySQL 做成完整的实时出价 fallback。
- 第一版不引入 Redis Cluster。
- 不假设 Redis 异步复制可以保证零数据丢失。
- 本设计不重构完整部署拓扑。

## 设计概要

方案由三层组成：

1. Redis HA 用来缩短故障时间。
2. 应用内 Redis 熔断器避免请求线程反复阻塞在 Redis timeout 上。
3. 健康检查端点分别暴露进程存活、流量就绪和组件降级状态。

Redis 故障或恢复期间：

- 后端进程保持存活。
- MySQL 支撑的读取和订单路径尽量继续可用。
- `PlaceBid`、`StartItem` 和主结算路径等竞拍写入快速失败或暂停。
- Redis 支撑的读取富化回退到 MySQL 或安全默认值。
- Redis 可达后，不会立刻恢复竞拍写入；必须先校验或重建活跃竞拍状态。

## Redis HA 策略

第一阶段推荐使用 Redis Sentinel 或托管 Redis HA endpoint，而不是 Redis Cluster。

当前竞拍 Lua 一次操作会触达多个 key：

- `auction:item:{item_id}:state`
- `auction:item:{item_id}:ranking`
- `auction:item:{item_id}:bidder_names`
- `auction:item:{item_id}:idempotency:{key}`
- `auction:ending`

Redis Cluster 要求同一个 Lua 操作里的 key 落在同一个 hash slot。要做到这一点，需要重新设计 hash tag 和 key 结构。Cluster 更偏向容量扩展设计，不是当前最小 HA 方案。

对于 Sentinel 或托管 HA，应配置 Redis 来减少脑裂写入窗口：

```text
replica-read-only yes
min-replicas-to-write 1
min-replicas-max-lag 1
```

这些配置不会把 Redis 变成强一致系统。它们的作用是：当旧 primary 看不到健康 replica 时，限制它继续接受写入的时间窗口。

对于高价值竞拍写入，最终应把“出价成功”的确认条件定义为：

```text
Redis Lua 成功
通过 WAIT 或托管 HA 等价能力获得 replica 确认
完成持久出价日志交接
返回 HTTP success
```

第一版可以继续使用当前同步写 MySQL `bid_logs` 的方式。如果后续出价热路径改成 Redis Stream 异步落库，那么 Stream append 就是持久交接点，也必须纳入同样的 failover 安全策略。

## Redis 熔断器

新增一个进程内 Redis 熔断器，位置可以在 `internal/core/cache` 或邻近 runtime 包中。它跟踪 Redis 可用性，供业务代码做决策。

状态：

| 状态 | 含义 | 业务行为 |
| --- | --- | --- |
| `healthy` | Redis 命令正常成功 | 正常执行 |
| `suspect` | 最近出现错误或慢命令 | 允许有限尝试并记录指标 |
| `unavailable` | 连续失败超过阈值 | 竞拍写入快速失败；读取走 fallback |
| `recovering` | 故障后 Redis ping 成功 | 竞拍写入继续暂停；校验或重建状态 |

状态流转：

```text
healthy -> suspect
  Redis 命令 timeout、报错或高延迟。

suspect -> healthy
  短时间成功窗口通过。

suspect -> unavailable
  连续 N 次失败，或出现连接池 timeout 风暴。

unavailable -> recovering
  后台探测能 ping 通 Redis，并且简单读写探测成功。

recovering -> healthy
  活跃竞拍状态校验或重建成功。

recovering -> unavailable
  探测或校验失败。
```

建议第一版阈值：

```text
failure_threshold: 连续 3 次失败
success_threshold: 连续 3 次探测成功
probe_interval: 1s
redis_command_timeout: 竞拍写入 100-300ms
recovering_min_duration: 2s
```

精确值后续应配置化。第一版可以先使用保守硬编码，并配套单元测试。

## 业务路径策略

| 路径 | Redis unavailable | Redis recovering | 说明 |
| --- | --- | --- | --- |
| `PlaceBid` | 返回可重试的 auction-unavailable 错误 | 快速失败 | 实时状态可能过期时不接受出价 |
| `StartItem` | 返回可重试的 auction-unavailable 错误 | 拒绝 | 开拍需要 Redis 状态和结束调度 |
| `SettleDueAuctions` | 暂停 Redis 驱动结算 | 校验后再恢复 | MySQL fallback 补偿可谨慎继续 |
| `EndExpiredAuctions` fallback | 可扫描 MySQL，但不能重复结算 | 只修复安全场景 | 避免和恢复中的 Redis 状态冲突 |
| `GetRanking` | fallback 到 MySQL `bid_logs` | MySQL fallback | 当前行为基本已匹配 |
| `GetItem` / item lists | MySQL DTO fallback | MySQL 或重建后的 Redis 状态 | 实时字段可能缺失或过期 |
| `Room` queries | 返回 `online_count=0`、空队列或 MySQL current item | 同左 | 房间 Redis 是富化状态，不是持久事实 |
| WebSocket ticket | 第一版不做本机内存 fallback，返回不可用/降级 | 同左 | 本机 ticket 需要单实例或 sticky routing |
| WebSocket fanout | 本机 hub 继续发送 | 继续 | presence sync 软失败 |
| 登录、用户、订单、支付 | MySQL 健康则继续 | 继续 | 除非路径直接依赖 Redis，否则不受 Redis 熔断影响 |

竞拍写入错误应使用稳定的 service-boundary error，例如：

```text
HTTP 503
code: 50301
message: auction service temporarily unavailable
```

这样可以区分可重试的基础设施降级和业务无效出价。

## 健康检查端点

将当前健康检查拆成三个概念。

### `/livez`

Liveness 只回答：进程是否还活着，Kubernetes 是否不应该重启它。

规则：

- 不 ping Redis。
- 不因为 Redis 不可用而失败。
- 只在不可恢复的进程级异常时失败。

典型响应：

```json
{"status":"ok"}
```

### `/readyz`

Readiness 回答：这个实例是否适合接收常规流量。

当前单体服务的第一版策略：

- MySQL 不可用：返回 `503`。
- Redis 不可用：只要 MySQL 健康，仍返回 `200`。
- Redis 降级细节通过 `/health` 暴露，不通过 `/readyz` 摘掉整个实例。

原因：当前是单体服务，Redis 故障不应该移除所有流量；商品浏览、订单、账号等 MySQL 支撑路径仍应可用。

如果后续把竞拍写路径拆成独立服务，那么该服务的 `/readyz` 可以在 Redis 熔断状态不是 `healthy` 时返回 `503`。

### `/health`

详细组件健康状态，供监控和人工排障使用。

降级响应示例：

```json
{
  "status": "degraded",
  "components": {
    "mysql": {"status": "ok", "latency": "8ms"},
    "redis": {"status": "error", "error": "i/o timeout"},
    "redis_circuit": {"status": "unavailable"},
    "auction_write": {"status": "unavailable"},
    "auction_read": {"status": "degraded"},
    "ws_ticket": {"status": "degraded"}
  }
}
```

如果监控系统需要用非 2xx 触发告警，`/health` 可以在 degraded 时返回 `503`。Kubernetes liveness 不应使用这个端点。

## 恢复与状态校验

Redis 重新可达并不代表可以立刻恢复竞拍写入。应用必须先进入 `recovering`。

恢复步骤：

1. Ping Redis，并执行短读写探测。
2. 从 MySQL 列出所有 ongoing item。
3. 对每个 ongoing item：
   - 检查 `auction:item:{id}:state`。
   - 如果状态存在且内部一致，保留。
   - 如果缺失或过期，用 MySQL item/rule 加持久 `bid_logs` 重建。
   - 必要时从 `bid_logs` 重建 ranking 和 bidder names。
   - 从权威 end time 重建 `auction:ending` score。
4. 从 MySQL 校验 room current item，并机会性修复 Redis room state。
5. 只有全部校验完成后，熔断状态才能标记为 `healthy`。

如果任一活跃竞拍校验失败，竞拍写入继续不可用，并在 `/health` 中暴露 `recovering` 或 `unavailable`。

## 重建规则

所有 Redis key 都必须被分类为“可重建投影”或“权威状态”。

| Key | 分类 | 重建来源 |
| --- | --- | --- |
| `auction:item:{id}:state` | 实时工作状态；故障后必须可重建 | MySQL item/rule + `bid_logs` |
| `auction:item:{id}:ranking` | 可重建投影 | 按用户聚合 `bid_logs` 最高价 |
| `auction:item:{id}:bidder_names` | 可重建投影 | 从 `bid_logs` join users 表 |
| `auction:item:{id}:idempotency:{key}` | 快路径保护；需要持久备份 | 未来 `bid_logs` 唯一索引 |
| `auction:ending` | 可重建调度索引 | ongoing item state end time |
| `auction:room:{id}:state` | 可重建房间投影 | MySQL room + 本机 presence 默认值 |
| `auction:room:{id}:item_queue` | 可重建投影 | 房间内 published items |
| `ws:ticket:{ticket}` | 临时状态 | 不重建；未来可选本机 fallback |

关键持久记录是 `bid_logs`。只有每个已承认成功的出价都有持久证据，Redis 状态才可以可靠重建。

## 幂等加固

Redis 幂等 key 不足以覆盖 failover 场景，因为 replica 落后时 key 可能丢失。

后续应在 `bid_logs` 增加持久幂等字段：

```text
item_id
user_id
idempotency_key
```

并在这些字段上建立唯一索引。Redis 继续作为快路径幂等检查；MySQL 负责防止 failover 或重试后的重复已确认出价。

## 可观测性

新增围绕降级的指标和日志：

- Redis command error count，按 command 或 operation 维度。
- Redis command latency P95/P99。
- Redis circuit state gauge。
- Redis circuit transition counter。
- Auction write rejected count，reason 包含 `redis_unavailable`、`redis_recovering`。
- Redis state rebuild count 和 duration。
- Redis/MySQL auction state mismatch count。
- Settlement paused count。
- Bid log persistence failure count。
- Health endpoint component status。

日志应包含 operation、可安全记录的 item ID 或 room ID、circuit state。不要记录 secret、Redis credential 或 token。

## 部署说明

当前 k3s 生产形态是单节点。在同一节点上运行多个 Redis pod，只能防 Redis 进程或 pod 故障，不能防节点、磁盘或网络故障。

生产级 Redis HA 需要二选一：

- 使用单节点 k3s 集群外部的托管 Redis HA。
- 使用多节点 Kubernetes 集群，并将 Redis Sentinel/HA 跨节点调度。

无论采用哪种方式，应用级降级仍然必需，因为 failover 窗口和复制延迟仍然存在。

## 测试策略

单元测试：

- 熔断器在连续失败后从 `healthy` 进入 `suspect` 再进入 `unavailable`。
- 熔断器只有在校验成功后才能从 `unavailable` 经 `recovering` 回到 `healthy`。
- `PlaceBid` 在 circuit 为 `unavailable` 或 `recovering` 时快速失败。
- Ranking 和 item 读取在 cache 返回错误时 fallback。
- Room service 面对 nil/noop cache 或 cache error 时不 panic。
- Health handler 返回正确的 live、ready 和 degraded 响应。

集成测试或 agent 测试：

- Redis 不可用时启动后端；进程保持存活，`/livez` 成功。
- Redis 不可用时，非竞拍读取路径成功；`PlaceBid` 返回可重试 503。
- Redis 恢复后，circuit 进入 `recovering`，重建活跃竞拍状态，然后恢复写入。
- Redis state 缺失但 MySQL `bid_logs` 存在时，能重建 ranking 和 state。
- 模拟活跃出价中的 Redis failover，不产生两个 winner 或重复 order。

故障演练：

- kill Redis primary pod。
- 隔离 Redis primary 网络路径。
- Redis 不可用时重启后端。
- 注入 Redis 延迟 timeout，但不 kill Redis。
- Redis Lua 成功后让 MySQL bid log 写失败，确认 HTTP 不返回 success。

## 发布计划

1. 新增 `/livez`、`/readyz` 和更详细的 `/health`。
2. 修改启动流程，使 Redis 连接失败不再导致整个 server 退出。
3. 引入 Redis 熔断器，并通过 health 暴露状态。
4. 将 `PlaceBid`、`StartItem` 和 settlement 接入熔断状态，非 healthy 时快速失败或暂停。
5. 为 room 和 WebSocket presence 路径增加 fallback/noop cache 行为。
6. 增加活跃竞拍恢复校验与 Redis state 重建。
7. 接入 Redis HA 部署或托管 Redis endpoint。
8. 增加持久 bid idempotency 加固。

## 已定决策

1. 当前单体服务中，Redis 不可用但 MySQL 健康时，`/readyz` 仍返回 `200`。`/health` 报告 Redis 和 auction-write 降级。
2. WebSocket ticket fallback 不进入第一版。Redis 不可用时，ticket 签发和 WS upgrade 应返回降级/不可用，而不是静默切换到进程本地 ticket。只有单实例或 sticky routing 可接受时，后续才考虑进程本地 ticket。
3. 生产优先选择托管 Redis HA。自建 Sentinel 可用于成本控制或学习，但必须配套 failover 演练。
4. 已确认的出价成功最终应要求 `PlaceBidLua` 成功、通过 `WAIT` 或等价机制获得 replica 确认、并完成持久 bid log 写入后，再返回 HTTP success。在当前同步 bid-log 设计中，顺序是 Lua、replica acknowledgement、MySQL `bid_logs`、HTTP success。
