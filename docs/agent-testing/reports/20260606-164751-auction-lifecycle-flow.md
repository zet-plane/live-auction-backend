# 测试报告：auction-lifecycle

## 基本信息

- 测试目标：跑通直播竞拍完整闭环：注册 -> 商家开播 -> 上架商品 -> 缴纳保证金 -> 出价 -> 竞拍结束 -> 生成订单 -> 支付，并验证取消分支保证金罚没。
- 测试类型：Agent 全流程测试、状态一致性测试、WebSocket 小规模功能证据。
- 测试时间：2026-06-06 16:47:51 至 17:02:03（Asia/Shanghai）。
- 执行 agent：主 agent 串行执行。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/environment.md`、`guides/go-runner.md`、`flows/auction-lifecycle.md`、`modules/bid.md`、`modules/deposit.md`、`modules/item.md`、`modules/order.md`、`modules/payment.md`、`modules/room.md`、`modules/ws.md`、`reports/README.md`。

## 测试环境

- 服务地址：本地后端服务，具体地址已省略。
- 配置来源：worktree 本地 `config.yaml`，关闭 observability，连接本地等价依赖。
- MySQL：本地等价 MySQL，地址和凭据已省略。
- Redis：本地等价 Redis，地址和凭据已省略。
- Apifox：本次未执行接口规范对齐；测试按代码路由和 `docs/agent-testing/` 契约执行。
- WebSocket：真实本地 WebSocket 连接，ticket 和完整 query string 已省略。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地等价真实依赖 | 验证用户、房间、拍品、保证金、BidLog、订单最终状态 |
| Redis | 本地等价真实依赖 | 验证房间队列、竞拍实时状态、排行榜、WebSocket 在线数和 bid stream |
| WebSocket | 真实连接 3 个用户 | 验证 ticket、在线数、心跳和业务广播 |
| 外部服务 | 未使用 | 当前支付为站内模拟状态，不调用第三方 |

## 测试数据

- 测试批次 ID：`agent_auction_lifecycle_20260606164751`
- 创建数据：1 个商家、3 个用户、1 个直播间、3 个拍品、3 条保证金、5 条出价日志、2 个订单。
- 复用数据：本地等价 MySQL/Redis 容器；未复用业务数据。

## 执行步骤

1. 清理同批次上一轮失败残留。
2. 注册商家和用户 A/B/C，设置商家身份，创建并开播直播间。
3. 创建 P1/P2 并上架，验证房间待拍队列。
4. 为 A/B/C 建立 WebSocket 连接，验证 `ping -> pong` 和在线人数。
5. 等待自动开始 cron 将 P1 从 `published` 置为 `ongoing`。
6. 验证未缴保证金出价失败；A/B 缴纳保证金；A/B 多轮出价到 1600，B 领先。
7. 等待 P1 到期，由 `SettleDueAuctions` 自动结算，生成 B 的 `pending` 订单并退款 A 的保证金。
8. B 支付订单，验证订单 `paid` 且 B 保证金 `refunded`。
9. 创建 price cap 分支拍品，A 中标后取消订单，验证 A 保证金 `forfeited`。
10. 清理本批次 DB/Redis 数据。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 环境可达 | `GET /api/v1/health` 返回 `code=0,status=ok`；runner 命令退出码 0 | 通过 |
| 注册和开播 | runner CASE `register users and start merchant room`：4 个用户创建，直播间状态 `live`，Redis room state 为 `status:live` | 通过 |
| 商品上架和队列 | runner CASE `create publish items and verify room queue`：P1/P2 DB 状态 `published`，HTTP `item_queue` 与 Redis ZSET 均包含 P1/P2 | 通过 |
| WebSocket | runner CASE `connect room websocket for A/B/C`：3 条连接成功，`SCARD online_users -> 3`，收到 `pong` | 通过 |
| 自动开始竞拍 | runner CASE `automatic start changes P1 to ongoing`：HTTP/DB/Redis 均为 `ongoing`，房间 `current_item_id=P1`，WS 收到 `auction_started,time_sync` | 通过 |
| 保证金门禁和出价 | runner CASE `deposit gate and bid ranking`：未缴保证金出价返回 `40005`；缴纳后 4 次出价成功；当前价 `1600`，leader 为 B；BidLog 4 条；WS 收到 `bid_success,user_outbid` | 通过 |
| 到期结算和订单 | runner CASE `timed settlement creates pending order and refunds non-winner`：P1 `ended`，winner 为 B，成交价 `1600`，订单 `pending`，A 保证金 `refunded`，B 保证金保持 `paid`，WS 收到 `auction_ended,order_created` | 通过 |
| 支付退款赢家保证金 | runner CASE `pay order refunds winner deposit`：支付接口返回 200，订单 `paid`，B 保证金 `refunded`，订单详情为 `paid` | 通过 |
| 取消罚没分支 | runner CASE `winner cancellation forfeits deposit`：price cap 成交后订单 `pending`，取消接口返回 200，订单 `cancelled`，赢家保证金 `forfeited` | 通过 |
| 清理 | runner CLEANUP：删除本批次 deposits 3、orders 2、bid_logs 5、auction_rules 3、auction_items 3、live_rooms 1、users 4；删除相关 Redis state/ranking/queue/idempotency key；XDEL bid stream 5 条 | 通过 |

## 通过项

- 主流程完整执行成功。
- 自动开始、到期自动结算、订单生成、支付成功全部通过真实 HTTP/MySQL/Redis/WS 链路验证。
- 未缴保证金不能产生有效出价。
- A/B 多轮出价后最高价、排行榜、Redis state、BidLog 和成交结果一致。
- 非赢家竞拍结束后保证金退款。
- 赢家在订单完成前保证金保持 `paid`。
- 赢家支付订单后保证金退款。
- 赢家取消订单后保证金罚没。
- WebSocket ticket、在线数、心跳和主要业务广播均有证据。
- 本批次测试数据已清理。

## 失败项

- 无。

## 跳过项

- 并发一致性专项未执行：本流程契约明确基础端到端流程不执行并发场景；需单独按 `guides/concurrency.md` 设计并获批。
- 订单过期罚没分支未单独等待 30 分钟过期扫描：本次使用用户取消订单验证 `pending -> cancelled -> forfeited`；`pending -> expired -> forfeited` 已由订单服务测试覆盖，后续可用可控超时配置或 fixture 做专项集成。
- Apifox 对齐未执行：本次目标是 E2E 闭环，不是接口契约对齐；后续可按模块契约单独执行。

## Apifox 对齐偏差

- 未执行接口契约对齐；不适用。

## 风险和建议

- 本次 runner 使用临时目录 `/private/tmp/agent-runner-agent_auction_lifecycle_20260606164751`，建议后续把可复跑 E2E runner 固化为脱敏测试资产。
- 订单过期罚没建议补一个可控短超时的集成测试，避免依赖 30 分钟默认超时。
- 并发出价和结算同时出价仍需专项测试覆盖。

## 建议沉淀的回归测试

- `auction-lifecycle` Go runner 固化为可复跑 flow runner。
- 保证金终态不覆盖的接口/集成回归。
- 订单支付、取消、过期三条状态流转的保证金副作用回归。
- 出价并发一致性专项 runner。

## 已知缺口

- 未覆盖真实第三方支付、物流、履约和退款打款。
- 未覆盖大规模 WebSocket 扇出和性能。
- 未覆盖 Apifox/Swagger 对齐。

## 测试数据清理结果

```text
测试批次 ID：agent_auction_lifecycle_20260606164751
创建的数据：users=4, live_rooms=1, auction_items=3, auction_rules=3, deposits=3, bid_logs=5, orders=2
清理方式：按本批次账号前缀、房间 ID、拍品 ID 和 Redis key 精确删除；未执行 DROP/TRUNCATE/FLUSH。
清理结果：DB 删除 deposits=3, orders=2, bid_logs=5, auction_rules=3, auction_items=3, live_rooms=1, users=4；Redis 删除相关 state/ranking/bidder_names/idempotency/room queue/room state，XDEL bid stream=5。
未清理原因：无。
```
