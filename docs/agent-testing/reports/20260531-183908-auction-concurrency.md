# 测试报告：auction concurrency

## 基本信息

- 测试目标：出价并发、成交 / 订单唯一性、保证金竞态、商品状态流转并发。
- 测试类型：并发一致性测试、状态一致性测试。
- 测试时间：2026-05-31 18:39 Asia/Shanghai。
- 执行 agent：主 agent + 3 个只读 subagent 做契约梳理；主 agent 执行 runner 和复核。
- 主 agent：Codex。
- 子 agent：出价并发、订单/支付、保证金/商品状态三个只读 subagent。
- 子 agent 结果摘要：均完成合约梳理并指出需确认语义；用户确认后执行。
- 主 agent 复核结论：runner 18 PASS / 5 FAIL；主复核额外标出 D1 HTTP 500 风险。
- 冲突和处理：阻塞语义已由用户确认后写入计划。
- Subagent cleanup：3 个 subagent 均已关闭。
- 并行数据隔离证明：batch_id 为 `agent_auction_concurrency_20260531172249`；用户、商品、订单、Redis key 均使用本批次前缀或本批次实体 ID。
- 读取文档：README、protocol、runner、concurrency、go-runner、subagent、environment、reports，以及 bid/order/payment/deposit/item/auction-lifecycle 契约。

## 测试环境

- 服务地址：本地 `127.0.0.1:8080`。
- 配置来源：`config.yaml`。
- MySQL：本地测试 MySQL，地址和凭据已省略。
- Redis：本地测试 Redis，地址和凭据已省略。
- Apifox：本次未做接口规范对齐。
- WebSocket：本次未覆盖真实连接。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 真实本地服务 | 验证 handler、鉴权、序列化和业务响应 |
| MySQL | 真实测试库 | 验证唯一约束、条件更新、最终持久化 |
| Redis | 真实测试 Redis | 验证 Lua、ranking、state 和清理 |
| WebSocket | 未使用 | 本次目标是 HTTP/DB/Redis 并发一致性 |
| 外部服务 | 禁止 | 当前模块不应调用真实第三方 |

## 测试数据

- 测试批次 ID：`agent_auction_concurrency_20260531172249`
- 创建数据：9 个用户、1 个房间、22 个本批次商品和规则、订单/保证金/出价日志若干。
- 复用数据：无。

## 执行步骤

1. `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...` 通过。
2. `rtk docker compose ps` 显示 MySQL 和 Redis healthy。
3. 临时启动本地服务。
4. 执行 `.agent-tmp/auction_concurrency_core` Go runner。
5. runner 使用 start gate 发起并发请求，并输出 CASE / SUMMARY / CLEANUP。
6. 停止临时服务，确认 8080 端口释放。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 编译/单测 | `go test ./...` 全部通过 | PASS |
| B1 不同价格并发出价 | 5 请求重叠约 10.9ms；最终 Redis/MySQL 最高价 1500，leader 一致 | PASS |
| B2 相同价格并发出价 | 1 成功 4 个 `40003`；BidLog 只有 1 条 1100 | PASS |
| B3 同 key 幂等 | 5 个 HTTP 成功；BidLog=1，`bid_count=1`，TTL 24h | PASS |
| B4 同用户递增 | ranking score 与 MySQL MAX(price) 均为 1400 | PASS |
| B5 一口价 vs 普通出价 | 商品 ended，winner/deal_price=一口价用户/1500，订单=1，后续出价失败 | PASS |
| B6 排行榜读写并发 | 10 个 ranking 查询无 500；最终 Redis top 与 MySQL top 一致 | PASS |
| O1 并发 CreateOrder | 最终只有 1 单，但 9 个调用暴露唯一索引冲突 | FAIL |
| O2 补偿 vs CreateOrder | 最终 1 单，CreateOrder 无错误 | PASS |
| O3 并发支付 | 5 个支付请求均 200；最终 paid | PASS |
| O4 支付 vs 取消 | 最终单一状态 cancelled，本轮 1 cancel 成功，其余失败可解释 | PASS |
| O5 并发取消 | 1 成功 4 失败，最终 cancelled | PASS |
| O6 已 paid 且过期后重复支付 | HTTP 400 invalid request，DB 仍 paid | FAIL |
| O7 已过期 pending 取消 | cancel 返回 200，扫描后仍 cancelled | FAIL |
| D1 同用户并发保证金 | 最终 1 条 paid，但 4 个请求 HTTP 500 | 主复核标风险/失败候选 |
| D2 保证金支付 vs 出价 | 出价先看不到 paid 时 `40005`，无 BidLog，最终保证金 paid | PASS |
| D3 不同用户并发保证金 | 5 个用户各 1 条 paid | PASS |
| I1 并发上架 | 5 个 HTTP 成功，最终 published，Redis member 唯一 | PASS |
| I2 并发开始 | 5 个 HTTP 成功，最终 ongoing，Redis state 存在 | PASS |
| I3 并发取消 | 5 个 HTTP 成功，最终 cancelled，Redis state/queue 清空 | PASS |
| I4 删除 vs 上架 | 两个 HTTP 都成功，DB 为 deleted+published，Redis queue 仍有 member | FAIL |
| I5 修改 vs 上架/删除 | 规则仍唯一，最终状态合法 | PASS |
| I6 过期结算 vs 取消 | 最终 cancelled 但订单 count=1 | FAIL |

## 通过项

- 出价并发核心路径通过：不同价、同价、幂等、同用户递增、一口价、排行榜读写并发均完成 HTTP/Redis/MySQL 对账。
- 支付/取消基础并发通过：并发支付、支付取消竞争、并发取消最终状态唯一。
- 不同用户保证金并发和保证金/出价竞态最终状态一致。
- 重复上架、重复开始、重复取消未留下 Redis/MySQL 矛盾状态。

## 失败项

- O1：`CreateOrder` 并发时虽然 DB 唯一约束保证最多一单，但 9 个调用返回唯一索引错误，不符合“直接返回已有订单”语义。
- O6：已 paid 订单超过 `expired_at` 后重复支付返回 invalid request，不符合“报已支付”语义。
- O7：已过期 pending 订单仍可取消，最终 cancelled，不符合“不能取消，应过期”语义。
- I4：删除与上架并发出现 DB `deleted_at != NULL` 且 `status=published`，Redis room queue 仍保留该 item。
- I6：过期结算与取消并发最终 item 为 cancelled，但同时产生订单，商品终态和订单副作用矛盾。

## 跳过项

- WebSocket 真实连接与推送一致性：本次计划未覆盖。
- 性能压测和容量指标：本次是并发一致性，不测吞吐。

## Apifox 对齐偏差

未执行接口规范对齐；本次只做真实接口并发与最终状态对账。

## 风险和建议

- D1 同一用户同一商品并发支付保证金，最终 DB 正确但出现 4 个 HTTP 500；建议服务层捕获唯一索引冲突并重查返回幂等结果。
- 商品状态动作多个 HTTP 成功说明当前“先查再保存”没有条件更新；建议将 publish/start/cancel/delete/update 改为条件更新或事务锁。
- 一口价成交后 Redis state 仍存在且状态不表达 ended；后续依赖 DB status 拦截，建议明确缓存终态语义。

## 建议沉淀的回归测试

- O1/O6/O7 加订单 service 并发/状态单元测试和 DAO 集成测试。
- I4/I6 加商品状态条件更新集成测试。
- D1 加保证金唯一冲突幂等测试。
- B1-B6 可保留为轻量 Go runner smoke test。

## 已知缺口

- 未测试同一 `idempotency_key` 不同 price 的并发提交。
- 未测试商家参与出价或缴保证金，因为用户确认本轮不考虑。

## 测试数据清理结果

- 前置 cleanup：无残留。
- 最终 cleanup：
  - 删除 batch 直插订单：5 行。
  - 删除本批次商品关联订单：4 行。
  - 删除 `bid_logs`：9 行。
  - 删除 `deposits`：7 行。
  - 删除 `auction_rules`：22 行。
  - 删除 `auction_items`：22 行。
  - 删除房间：1 行。
  - 删除用户：9 行。
  - Redis：逐 item 删除 state/ranking/bidder_names/idempotency，并从 room queue 移除本批次 item。
