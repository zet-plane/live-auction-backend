# 测试报告：order-payment-bid

## 基本信息

- 测试目标：订单模块、支付模块、出价模块
- 测试类型：本地单元测试、全量编译检查、接口契约测试、模块集成测试、并发测试、状态一致性测试
- 测试时间：2026-05-25 00:35-01:06 Asia/Shanghai
- 执行 agent：Codex
- 读取文档：
  - `docs/agent-testing/README.md`
  - `docs/agent-testing/agent-runner-guide.md`
  - `docs/agent-testing/environment.md`
  - `docs/agent-testing/go-runner-guide.md`
  - `docs/agent-testing/reports/README.md`
  - `docs/agent-testing/modules/order.md`
  - `docs/agent-testing/modules/payment.md`
  - `docs/agent-testing/modules/bid.md`

## 测试环境

- 服务地址：本地测试服务 `127.0.0.1:18080`
- 配置来源：临时测试配置 `/private/tmp/live-auction-agent-test.yaml`
- MySQL：本地 compose MySQL，地址和凭据已省略
- Redis：本机 Redis `127.0.0.1:6379`，只操作本批次 `auction:item:agent_obp_20260525003600*` keys
- Apifox：读取当前项目 OpenAPI Spec，下载时间 `2026-05-24T07:04:06.604Z`
- WebSocket：不适用，出价模块当前未实现 WebSocket 广播

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地真实测试数据库；只操作 `agent_obp_20260525003600` 批次数据 | 验证订单、支付状态、BidLog、商品成交状态和订单生成 |
| Redis | 本机真实 Redis；只操作本批次 auction item keys | 验证出价 Lua、实时竞拍状态、排行榜和幂等 key |
| WebSocket | 不使用 | 当前模块未实现出价广播 |
| 外部服务 | 不使用 | 文档禁止真实支付、退款、短信和第三方服务 |

## 测试数据

- 测试批次 ID：`agent_obp_20260525003600`
- 创建数据：
  - 4 个测试用户：buyer、other、third、merchant
  - 2 条 `auction_rules`
  - 2 条 `auction_items`
  - 2 条手工准备的 `orders`
  - 1 条由一口价出价自动生成的 `orders`
  - 4 条 `bid_logs`
  - 10 个本批次 Redis keys（最终清理数量）
- 复用数据：本地测试数据库 schema、本机 Redis 服务

## 执行步骤

1. 运行目标模块测试：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/order/... ./internal/app/payment/... ./internal/app/item/...`
2. 运行全量测试：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...`
3. 启动临时测试服务：`rtk env GOCACHE=/tmp/live-auction-go-cache go run main.go server -c /private/tmp/live-auction-agent-test.yaml`
4. 运行临时 Go runner：`rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN=<已省略> go run .`
5. 读取 Apifox OpenAPI Spec，并对订单、支付、出价接口做字段对齐检查。
6. 停止临时服务并记录清理结果。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 目标模块测试 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/order/... ./internal/app/payment/... ./internal/app/item/...`：order/service 和 item/service `ok`，其余无测试文件 | 通过 |
| 全量测试 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...`：全部 package `ok` 或 `[no test files]` | 通过 |
| 默认 Go cache 权限 | 未设置 `GOCACHE` 的目标测试命令触发用户 Go build cache `operation not permitted` | 环境问题，已用 `/tmp` cache 规避 |
| 本地服务启动 | 服务输出 `server listening on http://127.0.0.1:18080` | 通过 |
| 用户与商家准备 | Runner CASE `register users`：注册 4 个用户，merchant 身份更新成功，DB 身份正确 | 通过 |
| 拍品、订单、Redis state 准备 | Runner CASE `prepare items orders and redis state`：2 个 ongoing 商品，Redis state 起拍价 `1000`、`bid_count=0` | 通过 |
| 订单查询与权限 | Runner CASE `order list detail and ownership`：订单列表 200，订单所属用户详情 200，其他用户详情 401 | 通过 |
| 支付与取消 | Runner CASE `payment pay cancel and reject cancelled pay`：支付后 DB 为 `paid`；取消后 DB 为 `cancelled`；取消后支付 400 且状态不变 | 通过 |
| 出价成功与幂等 | Runner CASE `bid success and idempotency`：第一次和第二次同 key 出价均 200，bid_id 相同；Redis `current_price=1100`、`bid_count=1`，MySQL BidLog 只有 1 条 | 通过 |
| 无效出价拒绝 | Runner CASE `reject invalid increment bids`：低价 1050 返回 `40003`，非加价步长 1150 返回 `40004`；Redis 价格仍 1100，MySQL 无 other 用户 BidLog | 通过 |
| 排行榜 | Runner CASE `ranking reflects highest bids`：1200 出价成功；排行榜第一名为 1200 用户；Redis ranking 与 MySQL BidLog 一致 | 通过 |
| 并发出价 | Runner CASE `concurrent bids final state consistency`：3 个并发请求窗口约 3ms，1 个成功、2 个因当前价变化返回 `40003`；Redis 最终价 1500 与 MySQL `MAX(price)=1500` 一致 | 通过 |
| 一口价成交和订单生成 | Runner CASE `price cap ends auction and creates order`：1500 触发 ended；商品 DB `status=ended`、`winner_id` 为出价用户、`deal_price=1500`；自动生成 pending 订单 | 通过 |
| Runner 汇总 | `PASS: 9  FAIL: 0` | 通过 |
| 清理结果 | Runner cleanup：Redis keys 10、bid_logs 4、orders 3、items 2、rules 2、users 4 均删除成功 | 通过 |

## 并发隔离证明

- 并发请求实际触发时间窗口（毫秒）：约 3ms
- 验证最终状态使用的查询方式：Redis `auction:item:<item_id>:state` 与 MySQL `SELECT COUNT(*), MAX(price) FROM bid_logs WHERE item_id = ?`
- 并发冲突次数：2 个请求因 Redis Lua 中当前价已变化返回 `40003 price too low`
- 最终状态唯一性证据：Redis `current_price=1500`、`leader_user_id` 为 1500 出价用户；MySQL `MAX(price)=1500`

## 通过项

- 目标模块和全量 Go 测试通过。
- 订单列表、订单详情归属隔离、支付、取消、取消后支付拒绝通过。
- 出价成功、幂等重试不重复写 BidLog、低价/非法加价拒绝、排行榜、并发最终一致、一口价成交和订单生成通过。
- 本批次 MySQL 与 Redis 测试数据已清理。

## 失败项

- 无业务失败项。

## 跳过项

- WebSocket 测试：不适用，出价模块当前只有 TODO，没有广播实现。
- 真实第三方支付、退款、短信：按模块文档禁止。
- 完整商品创建/上架/开始链路：本轮按订单、支付、出价模块测试，拍品和规则由 runner 直接准备。
- 自动延时边界：本轮未设置临近结束时间场景，建议单独补测。
- Redis 故障降级到 MySQL 排行榜：本轮未注入 Redis 故障。

## Apifox 对齐偏差

- 出价接口 `POST /api/v1/items/{item_id}/bids` 与当前代码的核心字段对齐：`price`、`idempotency_key`、`bid_id`、`current_price`、`leader_user_id`、`end_time`、`status` 均存在。
- 排行榜接口 `GET /api/v1/items/{item_id}/ranking` 与当前代码的核心字段对齐：`list`、`rank`、`user_id`、`user_name`、`price`、`page`、`page_size` 均存在。
- `GET /api/v1/orders` 当前代码支持 `status` query；Apifox 当前 `listOrders` 只声明了 `page` 和 `page_size`。
- 订单列表和详情代码响应包含 `item_title`；Apifox `Order` schema 当前未声明 `item_title`。
- `PayOrderRequest.result` Apifox schema 限定 enum `success`；当前代码只校验必填，不校验枚举值。

## 风险和建议

- 建议补齐 Apifox 的订单 `status` query 和 `item_title` 字段，避免前端契约缺失。
- 建议统一 `PayOrderRequest.result` 语义：要么代码校验 enum `success`，要么 Apifox 去掉 enum。
- 建议补充出价自动延时、Redis 排行榜故障降级、商品非 ongoing 出价拒绝、Redis state 缺失出价拒绝的 runner 场景。
- 建议为 `orders.item_id` 增加唯一约束或事务保护，避免并发成交补偿下重复建单。
- 建议出价模块补充 Redis 成功但 BidLog 写入失败、一口价更新商品失败时的补偿或回滚策略。

## 建议沉淀的回归测试

- 同一幂等 key 重复出价只写 1 条 BidLog。
- 并发出价最终 Redis 当前价、leader 和 MySQL 最高出价一致。
- 一口价成交自动创建 pending 订单。
- 取消订单后再次支付不改变订单状态。
- 排行榜 Redis 失败时 MySQL fallback 仍按用户最高价排序。
- 自动延时不超过最大延时次数和最大总延时秒数。

## 已知缺口

- 本轮没有通过商品模块接口创建、上架、开始拍品，而是直接准备 DB 和 Redis state。
- 本轮没有覆盖商家查询订单列表/详情的正向路径。
- 本轮没有验证订单列表未知 `status` 查询行为。
- 本轮没有测试保证金与出价的联动，因为用户本次目标指定的是订单、支付、出价模块。
- 本轮没有修改 Apifox，只记录对齐偏差。

## 测试数据清理结果

- 测试批次 ID：`agent_obp_20260525003600`
- 清理方式：按 batch 前缀删除本轮 runner 创建的 Redis keys、`bid_logs`、`orders`、`auction_items`、`auction_rules`、`users`
- 清理结果：
  - Redis `auction:item:agent_obp_20260525003600*`: 10 keys
  - `bid_logs`: 4 行
  - `orders`: 3 行
  - `auction_items`: 2 行
  - `auction_rules`: 2 行
  - `users`: 4 行
- 未清理原因：无
