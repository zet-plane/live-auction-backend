# 测试报告：订单、支付、出价回归

## 基本信息

- 测试目标：根据 `modules/order.md`、`modules/payment.md`、`modules/bid.md` 验证订单、支付、出价模块，并回归本次补充建议。
- 测试类型：模块集成、接口契约、场景、并发、状态一致性、回归。
- 测试时间：2026-05-25 01:44 Asia/Shanghai。
- 执行 agent：Codex。
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/agent-runner-guide.md`、`docs/agent-testing/go-runner-guide.md`、`docs/agent-testing/environment.md`、`docs/agent-testing/modules/order.md`、`docs/agent-testing/modules/payment.md`、`docs/agent-testing/modules/bid.md`、`docs/agent-testing/reports/README.md`。

## 测试环境

- 服务地址：本机测试服务，具体配置文件含凭据，已省略。
- 配置来源：临时测试配置。
- MySQL：本机测试数据库，地址和凭据已省略。
- Redis：本机测试 Redis，地址和凭据已省略。
- Apifox：只做本地契约偏差记录，未写入远端。
- WebSocket：未覆盖，当前出价模块尚未实现出价广播。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实测试数据库，仅操作本批次数据 | 验证订单唯一约束、状态流转、BidLog、排行榜 fallback 和一口价订单创建 |
| Redis | 真实测试 Redis，仅操作本批次 `auction:item:<batch>*` key | 验证 Lua 出价、幂等、自动延时、排行榜和缺失 state 拒绝 |
| WebSocket | 未使用 | 当前代码只保留 TODO |
| 外部服务 | 未使用 | 支付模块无真实支付调用 |

## 测试数据

- 测试批次 ID：`agent_obp_20260525024500`。
- 创建数据：4 个测试用户、8 个测试商品、8 条竞拍规则、4 条订单、5 条出价日志、14 个 Redis key。
- 复用数据：无业务数据复用；只复用本地测试环境服务、数据库和 Redis。

## 执行步骤

1. 新增并执行订单 DTO / Model 回归测试，覆盖 `PayOrderRequest.result` 和 `orders.item_id` 唯一索引声明。
2. 启动本机测试服务。
3. 使用 Go runner 注册测试用户、准备订单/商品/Redis state。
4. 通过 HTTP 验证订单列表、订单详情、支付、取消、出价、排行榜、并发出价和一口价成交。
5. 首轮 runner 发现 `result=failed` 仍支付成功，补充 payment handler 显式校验后重启服务。
6. 重跑 runner，验证全部 12 个场景通过并清理本批次数据。
7. 执行 `go test ./...` 全量单元测试。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 全量 Go 测试 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...` 退出码 0 | 通过 |
| 支付 result 非 success 拒绝 | `POST /orders/{id}/pay {"result":"failed"}` 返回 400 / `40001`，DB 订单仍为 `pending` | 通过 |
| 订单支付和取消 | 支付订单变为 `paid`；取消订单变为 `cancelled`；取消后支付返回 400 且状态不变 | 通过 |
| 订单查询隔离 | 买家列表/详情 200；其他用户查详情 401；DB 仅本批次买家 3 条订单 | 通过 |
| 非 ongoing 出价拒绝 | published 商品出价返回 400 / `40001`，无 BidLog | 通过 |
| Redis state 缺失出价拒绝 | ongoing 商品但无 Redis state 出价返回 400 / `40002`，无 BidLog | 通过 |
| 自动延时 | 临近结束出价后 `end_time_unix` 增加 60 秒，`extend_count=1`，`total_extended_sec=60` | 通过 |
| 出价幂等 | 同一 `idempotency_key` 二次请求返回同一 `bid_id`，BidLog 只写 1 条 | 通过 |
| 出价价格规则 | 低价返回 `40003`，非法加价返回 `40004`，Redis 当前价不变 | 通过 |
| Redis 排行榜 | 成功出价后 Redis ranking 最高价用户位于第一 | 通过 |
| MySQL 排行榜降级 | 删除 Redis ranking/names 后，接口从 `bid_logs` 聚合返回最高价用户 | 通过 |
| 并发出价 | 并发 1300/1400/1500 后最终 Redis 当前价与 DB MAX(price) 均为 1500 | 通过 |
| 一口价成交建单 | 出价达到 price_cap 后商品 `ended`，winner/deal_price 正确，订单为 `pending` | 通过 |
| 清理 | 删除本批次 Redis key=14、BidLog=5、orders=4、items=8、rules=8、users=4 | 通过 |

## 通过项

- 本次 runner 最终结果：PASS 12，FAIL 0。
- `PayOrderRequest.result` 已通过代码显式校验为 `success`，不再只依赖绑定 tag。
- `orders.item_id` 已声明命名唯一索引，runner 数据也调整为每个订单独立商品。
- 已补充并验证出价自动延时、Redis 排行榜降级、商品非 ongoing 拒绝、Redis state 缺失拒绝四类场景。

## 失败项

- 最终无失败项。
- 首轮 runner 曾暴露 `result=failed` 仍支付成功；已修复并通过重跑验证。

## 跳过项

- WebSocket 出价广播：当前代码未实现。
- Redis 成功但 BidLog 写入失败、一口价更新商品失败的故障注入：当前接口缺少可控故障注入能力，仅记录为已知缺口。
- Apifox 远端写入：未获明确写入授权，本次只记录偏差。

## Apifox 对齐偏差

- 订单列表 `GET /api/v1/orders` 建议补齐 `status` query。
- `Order` schema 建议补齐 `item_title` 字段。
- `PayOrderRequest.result` 远端 enum `success` 与代码已对齐。

## 风险和建议

- 若已有环境 `orders` 表存在重复 `item_id` 历史数据，唯一索引迁移可能失败，迁移前应先做重复数据扫描和清理策略。
- 出价 Redis 状态成功后，BidLog 或商品状态更新失败仍可能产生跨存储不一致；建议后续设计补偿任务、可重放出价日志队列或事务外盒。
- 一口价成交后订单创建失败目前依赖补偿 cron，建议 runner 后续增加 cron 补偿验证。

## 建议沉淀的回归测试

- 支付接口 `result=failed` 不得改变订单状态。
- 同一商品并发成交/补偿最多生成一条订单。
- 自动延时字段 `end_time_unix`、`extend_count`、`total_extended_sec` 一致性。
- Redis ranking 缺失时排行榜从 MySQL fallback 返回正确最高价。
- Redis state 缺失、商品非 ongoing 出价均不得写 BidLog。

## 已知缺口

- 未覆盖 Redis 成功但 BidLog 写入失败的补偿或回滚策略。
- 未覆盖一口价 Redis 成功、BidLog 成功但商品 ended 更新失败的补偿或回滚策略。
- 未覆盖真实 Apifox 写入后的二次拉取校验。

## 测试数据清理结果

- 测试批次 ID：`agent_obp_20260525024500`。
- 清理方式：按本批次 ID 删除 Redis `auction:item:<batch>*` key；按本批次 ID 删除 `bid_logs`、`orders`、`auction_items`、`auction_rules`、`users`。
- 清理结果：Redis key=14、BidLog=5、orders=4、items=8、rules=8、users=4 均已删除。
- 未清理原因：无。
