# TODO

## P0 — 业务完整性

### 定时任务（cron）

- [ ] **自动开始竞拍** — 商品上架后按 `rule.start_time` 自动触发 `StartItem`，复用现有服务方法，挂到 `kernel.Engine.Cron`
- [ ] **自动结束过期竞拍** — `EndExpiredAuctions()` 已实现，接入 cron 周期轮询（建议 5s），结束竞拍、创建订单、广播 `auction_ended`
- [ ] **订单超时关闭** — `pending` 订单超过 N 分钟未支付，状态流转为 `expired`，通过 cron 或订单创建时写入延迟任务触发

---

## P1 — 工程深度

### 可观测性（设计文档已有：`docs/superpowers/specs/2026-05-25-observability-design.md`）

- [ ] **结构化日志** — 引入 `zap` 或 `zerolog`，关键链路（出价、竞拍结束、订单创建）打带 `trace_id` 的 JSON 日志
- [ ] **Prometheus metrics** — 出价 QPS、Lua 脚本延迟、WebSocket 连接数、HTTP 错误率
- [ ] **健康检查接口** — `GET /health`，检查 MySQL + Redis 连通性，返回各组件状态
- [ ] **Grafana 看板** — 出价链路 P99 延迟、在线连接数、错误率趋势

---

## P2 — 质量与文档

- [ ] **E2E 集成测试** — 跑完整流程：注册 → 缴纳保证金 → 出价 → 竞拍结束 → 支付（`docs/agent-testing/` 框架已有）
- [ ] **Apifox 文档同步** — 将 `docs/5-21.md` 的接口导入 Apifox，支持 AI 驱动的接口测试
- [ ] **删除 `.golangci.yml`** — 无 CI 引用，本地也未使用，可直接删除

---

## P3 — 可选优化

- [ ] **Redis 降级策略** — Redis 不可用时出价链路如何降级（拒绝出价 vs 切 MySQL 兜底）
- [ ] **出价日志异步落库** — 当前同步写 MySQL，高并发下改为写 Redis LIST，worker 批量消费
- [ ] **Redis 读写分离** — 排行榜、商品详情等读操作走从库
