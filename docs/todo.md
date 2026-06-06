# TODO

后续主线：先把直播竞拍闭环做成可演示，再把可靠性、观测、压测和告警补成能讲清楚的工程故事。

## 推荐推进顺序

1. **闭环可演示**：自动开始/结束竞拍、保证金释放、全流程 E2E。
2. **实时可用**：WebSocket 服务端心跳、倒计时同步、出价限流。
3. **工程可信**：Redis 竞拍态恢复、补偿故障矩阵、告警和 runbook。
4. **展示加分**：压测报告、pprof、Grafana 大屏、AI/运营辅助能力。

---

## P0 — 竞拍闭环完整性

### 定时任务与状态流转

- [x] **自动开始竞拍** — 商品上架后按 `rule.start_time` 自动触发 `StartItem`，复用现有服务方法，挂到 `kernel.Engine.Cron`
- [x] **自动结束竞拍实时化** — `SettleDueAuctions()` 已接入 `@every 1s` 实时结算；`EndExpiredAuctions()` 保留 `@every 1m` 作为兜底 fallback
- [x] **订单超时关闭** — `order.scan_expired_orders` 已接入 cron，`pending` 订单超时后流转为 `expired`
- [x] **订单创建补偿** — `order.scan_compensation` 已接入 cron，扫描已结束但未建单的竞拍并补偿创建订单

### 保证金与交易闭环

- [x] **保证金释放策略** — 已明确并实现：竞拍结算退款非赢家；赢家保证金保持 `paid` 到订单完成；订单支付后 `refunded`；订单取消或过期后 `forfeited`；终态不被失败路径覆盖
- [x] **完整交易 E2E** — 已按 `docs/agent-testing/` 获批执行并产出报告：`docs/agent-testing/reports/20260606-164751-auction-lifecycle-flow.md`
- [x] **订单创建失败补偿说明** — 已在订单模块与全生命周期契约中说明：竞拍实时结算创建订单失败时，由 `ScanCompensation` 扫描已结束、有赢家、无订单的拍品补偿创建；订单创建按 `item_id` 幂等

---

## P1 — 实时体验与并发控制

### WebSocket 稳定性

- [x] **客户端心跳响应** — `StartReadLoop` 已支持客户端 `ping -> pong`
- [x] **服务端主动心跳** — `StartWriteLoop` 已定时发送 WebSocket control `ping`，`StartReadLoop` 通过 control `pong` 刷新 read deadline
- [x] **倒计时毫秒级同步** — 服务端每秒广播 `time_sync { item_id, server_time_unix_ms, end_time_unix_ms, status }`，客户端以服务端时间为准，解决本地时钟漂移和反狙击延时后的倒计时偏差
- [x] **断线重连恢复说明** — 已明确前端重连后通过 HTTP 房间详情、商品详情、排行榜和个人状态接口恢复现场，不依赖 WebSocket 历史消息
- [x] **WebSocket 在线数可信度** — Hub 关闭路径已幂等；同用户同房间新连接替换旧连接；Redis `online_count` 由 `SCARD online_users` 回写收敛

### 出价限流与公平性

- [ ] **出价防抖限流** — Redis 滑动窗口，同一用户对同一商品 1s 内最多 5 次，超出返回 429；这是题目中「防抖节流」的服务端实现
- [ ] **幂等性复盘** — 梳理出价、订单创建、支付、保证金缴纳、商品上架的幂等 key 或唯一约束，形成一张表
- [ ] **Lua 原子脚本边界说明** — 说明为什么用 Redis Lua 而不是乐观锁，以及 Redis Cluster 下需要 `{itemID}` hash tag 避免 `CROSSSLOT`

---

## P1 — 可观测性与告警

### 已有基础

- [x] **健康检查接口** — `GET /api/v1/health` 检查 MySQL + Redis 连通性并返回组件状态
- [x] **OpenTelemetry 基础指标** — 已有 HTTP、Redis Lua、DB、cron、bid、order 等 recorder
- [x] **结构化日志基础** — 已有 zap/logx 与 trace 字段辅助能力，Loki 可按 `trace_id` 查询
- [x] **Grafana 应用看板基础** — 已有本地观测运行文档和 k8s dashboard 配置

### 下一步告警

- [ ] **应用 SLO 告警** — 出价 P95/P99 延迟、HTTP 5xx、出价失败率、Lua 错误码异常
- [ ] **业务一致性告警** — 竞拍结束后订单创建失败、订单补偿持续失败、过期订单扫描失败、保证金异常状态堆积
- [ ] **运行时依赖告警** — MySQL 不可用、Redis 不可用、Redis Lua 延迟异常、cron job 连续失败
- [ ] **WebSocket 告警** — 在线连接数异常下降、连接错误率升高、广播失败率升高
- [ ] **飞书告警链路落地** — 基于 `docs/design/k3s-monitoring-alerting.md`，部署 Alertmanager -> 飞书 webhook adapter，并验证测试告警能到群
- [ ] **告警 runbook** — 每个 critical/warning 告警补充含义、排查命令、可能原因、止血动作和恢复验证方式

---

## P2 — 测试、文档与交付

- [x] **E2E 集成测试报告** — 已使用 `docs/agent-testing/` 框架跑完整流程，记录环境、测试数据、证据和清理结果：`docs/agent-testing/reports/20260606-164751-auction-lifecycle-flow.md`
- [ ] **并发一致性测试** — 针对同一商品并发出价，验证最高价、排行榜、BidLog、winner、订单只生成一次
- [ ] **Apifox / Swagger 文档同步** — 将当前 handler annotations 和 `docs/5-21.md` 的接口口径对齐，支持 AI 驱动接口测试
- [x] **删除 `.golangci.yml`** — 当前仓库中已不存在该文件
- [ ] **README 演示路径** — 补一条最短本地演示路径：启动依赖 -> 启动服务 -> 注册用户 -> 创建商品 -> 出价 -> 结束竞拍 -> 支付

---

## P2 — 压测与性能证据

- [ ] **出价接口压测** — 用 `wrk` 或 `k6` 对出价链路压测，拿到 P50/P95/P99/P999、错误率和吞吐
- [ ] **瓶颈定位报告** — 对比 Lua 执行、MySQL BidLog 落库、WebSocket 广播、网络开销，形成「数据 -> 结论 -> 下一步优化」
- [ ] **pprof 性能剖析** — 对 WebSocket 广播或出价链路做 CPU/heap profile，重点观察 `json.Marshal`、锁竞争和分配热点
- [ ] **容量说明** — 明确当前单实例可支撑的直播间在线人数、出价 QPS、广播消息量，以及扩容前提

---

## P3 — 可选架构演进

- [ ] **Redis 竞拍态恢复工具** — 从 MySQL ongoing item、rule 和 bid log 重建 Redis state/ranking，解决 Redis 数据丢失后的恢复问题
- [ ] **Redis 降级策略** — 明确 Redis 不可用时出价链路是拒绝出价，还是切 MySQL 兜底；推荐先拒绝出价并给出可观测告警
- [ ] **出价日志异步落库** — 当前同步写 MySQL；高并发下可改为 Redis Stream/List + worker 批量消费，但需要先有压测数据支撑
- [ ] **WebSocket 多实例广播** — 现有 Hub 是进程内内存实现；多实例时改为 Redis Pub/Sub，各节点订阅自己管理的 room channel，保持 `Broadcaster` 接口不变
- [ ] **Redis 读写分离** — 排行榜、商品详情等读操作走从库，但要先明确一致性窗口和故障切换策略

---

## P3 — 产品与展示加分

- [ ] **商家运营看板** — 当前价、出价次数、参与人数、在线人数、剩余时间、成交率、异常竞价提示
- [ ] **竞拍热度摘要** — 结束后生成一段竞拍复盘：参与人数、延时次数、最高价变化、关键出价节点
- [ ] **AI 辅助运营** — 商品标题/话术生成、直播间氛围文案、异常竞价提醒；不直接介入强一致出价路径

---

## P3 — AI 深度工程化

核心判断：AI 应该做流程闭环，而不是停留在表层 vibe coding 或一次性代码补全。

- [ ] **Agent 驱动的工程闭环** — 让 AI 在项目作业中承担「需求拆解 -> 设计文档 -> 实施计划 -> TDD/测试 -> 代码实现 -> 自检报告 -> 文档同步」的完整工程链路，而不是只做一次性代码生成
- [ ] **AI 测试工程化** — 基于 `docs/agent-testing/` 固化接口、集成、并发和 E2E 测试流程，让 AI 每次测试都记录环境、测试数据、证据、清理结果和失败分析
- [ ] **AI 代码评审机制** — 让 AI 以 code review 方式检查一致性风险、状态机漏洞、幂等缺口、补偿遗漏和测试空洞，并输出可追踪的问题列表
- [ ] **AI 运维分析能力** — 将日志、metrics、trace、压测报告和告警 runbook 串起来，让 AI 能根据真实观测数据定位瓶颈、解释异常、提出下一步优化
- [ ] **AI 工程证据展示** — 答辩时展示 AI 产出的 spec、plan、测试报告、压测结论、故障矩阵、接口文档同步记录，体现工程方法而不是表层 vibe coding

---

## 面试/答辩话题

- **Redis Lua vs 乐观锁** — 乐观锁需要「读-校验-写」并在高竞争下重试；Lua 将校验和写入压成一次原子操作，P99 延迟更稳定
- **MySQL 与 Redis 职责边界** — Redis 承载竞拍中实时态，MySQL 保存最终可信业务数据和审计记录
- **最终一致性与补偿** — WebSocket 推送失败不阻断主链路；订单创建失败由补偿任务修复；Redis 状态丢失需要恢复工具
- **单体优先的架构取舍** — 核心竞拍链路留在单体内闭环，减少远程调用不确定性；扩展优先从广播层和观测层开始
- **多实例演进路径** — Hub 进程内广播适合单实例；多实例用 Redis Pub/Sub 或消息总线，不拆散拍卖状态机
- **AI 深度工程化** — AI 不只是生成代码，而是参与需求建模、风险识别、测试执行、观测分析和文档沉淀；用可审计产物证明工程过程
