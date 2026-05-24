# 测试报告：order-payment-deposit

## 基本信息

- 测试目标：订单模块、支付模块、保证金模块
- 测试类型：本地单元测试、全量编译检查、接口契约测试、模块集成测试、状态一致性测试
- 测试时间：2026-05-24 23:30-23:32 Asia/Shanghai
- 执行 agent：Codex
- 读取文档：
  - `docs/agent-testing/README.md`
  - `docs/agent-testing/agent-runner-guide.md`
  - `docs/agent-testing/environment.md`
  - `docs/agent-testing/go-runner-guide.md`
  - `docs/agent-testing/reports/README.md`
  - `docs/agent-testing/modules/order.md`
  - `docs/agent-testing/modules/payment.md`
  - `docs/agent-testing/modules/deposit.md`

## 测试环境

- 服务地址：本地测试服务 `127.0.0.1:18080`
- 配置来源：临时测试配置 `/private/tmp/live-auction-agent-test.yaml`
- MySQL：本地 compose MySQL，地址和凭据已省略
- Redis：本机 Redis `127.0.0.1:6379`，未写入测试数据
- Apifox：读取当前项目 OpenAPI Spec，下载时间 `2026-05-24T07:04:06.604Z`
- WebSocket：不适用，本次目标模块不直接使用 WebSocket

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地真实测试数据库；只操作 `agent_opd_20260524233000` 批次数据 | 验证订单、保证金和商品规则持久化状态 |
| Redis | 仅用于服务启动依赖；本次模块无 Redis 断言 | 订单/支付/保证金模块不直接读写 Redis |
| WebSocket | 不使用 | 当前模块无 WebSocket 副作用 |
| 外部服务 | 不使用 | 文档禁止真实支付、退款、短信和第三方服务 |

## 测试数据

- 测试批次 ID：`agent_opd_20260524233000`
- 创建数据：
  - 3 个测试用户：buyer、other、merchant
  - 2 条 `auction_rules`
  - 2 条 `auction_items`
  - 3 条 `orders`
  - 1 条 `deposits`
- 复用数据：本地测试数据库 schema、当前本机 Redis 服务

## 执行步骤

1. 运行目标模块本地测试：`rtk go test ./internal/app/order/... ./internal/app/payment/... ./internal/app/deposit/...`
2. 运行全量测试：`rtk go test ./...`，首次因默认 Go cache 目录权限失败，未作为业务失败计入。
3. 使用 `/tmp/live-auction-go-cache` 重跑全量测试：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...`
4. 启动临时测试服务：`rtk env GOCACHE=/tmp/live-auction-go-cache go run main.go server -c /private/tmp/live-auction-agent-test.yaml`
5. 运行临时 Go runner：`rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN=<已省略> go run .`
6. 读取 Apifox OpenAPI Spec，并对订单/支付/保证金接口做字段对齐检查。
7. 停止临时服务并记录清理结果。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 目标模块单元测试 | `rtk go test ./internal/app/order/... ./internal/app/payment/... ./internal/app/deposit/...` 输出 `Go test: 25 passed in 17 packages` | 通过 |
| 全量编译和单元测试 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...` 全部 package `ok` 或 `[no test files]` | 通过 |
| 默认 Go cache 权限 | `rtk go test ./...` 输出 70 passed、5 build failed，失败原因为打开用户 Go build cache `operation not permitted` | 环境问题，已用 `/tmp` cache 规避 |
| 本地服务启动 | 服务输出 `server listening on http://127.0.0.1:18080` | 通过 |
| 用户注册与商家身份 | Runner CASE `register users and set merchant identity`：HTTP 注册 x3 和 `PUT /users/me` 均成功，DB 中 3 个 batch 用户身份正确 | 通过 |
| 订单测试数据准备 | Runner CASE `prepare order item rule data`：DB 中 3 条 batch 订单均为 `pending` | 通过 |
| 订单列表 | Runner CASE `buyer list pending orders`：`GET /api/v1/orders?status=pending&page=1&page_size=2` 返回 200，列表 2 条，DB batch pending 总数 3 | 通过 |
| 订单详情权限 | Runner CASE `order detail owner allowed other denied`：所属用户 200，其他用户 401，DB 归属为 buyer | 通过 |
| 支付订单 | Runner CASE `pay pending order and reject expired order`：pending 未过期订单支付 200 且 DB 为 `paid`；过期订单支付 400 且 DB 保持 `pending` | 通过 |
| 取消订单 | Runner CASE `cancel pending order and reject later pay`：pending 订单取消 200 且 DB 为 `cancelled`；取消后支付 400 且 DB 保持 `cancelled` | 通过 |
| 保证金支付与查询 | Runner CASE `pay and get my deposit`：支付和查询均 200；DB 金额 `500`、状态 `paid` | 通过 |
| 零保证金拒绝支付 | Runner CASE `reject deposit pay when required amount is zero`：接口 400，DB deposits 计数 0 | 通过 |
| Runner 汇总 | `PASS: 8  FAIL: 0` | 通过 |
| 清理结果 | Runner cleanup：deposits 1、orders 3、items 2、rules 2、users 3 均删除成功 | 通过 |

## 通过项

- 目标模块本地测试通过。
- 全量 Go 测试在指定 `/tmp` Go cache 后通过。
- 订单列表分页、订单详情归属隔离、支付状态流转、过期支付拒绝、取消状态流转、取消后支付拒绝均通过真实 HTTP + DB 验证。
- 保证金支付、查询、零保证金拒绝均通过真实 HTTP + DB 验证。
- 本批次测试数据已清理。

## 失败项

- 无业务失败项。

## 跳过项

- 并发测试：本轮未执行；需要扩展 runner 的并发场景，并记录并发隔离证明。
- WebSocket 测试：不适用，订单/支付/保证金模块不直接维护 WebSocket。
- 真实第三方支付、退款、短信：按模块文档禁止。
- 保证金 `refunded` / `forfeited` 终态重付：本轮未做故障注入数据，建议后续补回归。
- Apifox 自动同步：本轮只做对齐检查，没有修改 Apifox。

## Apifox 对齐偏差

- `POST /api/v1/items/{item_id}/deposit/pay` 未出现在当前 Apifox OpenAPI paths 中。
- `GET /api/v1/items/{item_id}/deposit` 未出现在当前 Apifox OpenAPI paths 中。
- `GET /api/v1/orders` 当前代码支持 `status` query；Apifox 当前 `listOrders` 只声明了 `page` 和 `page_size`。
- 订单列表和详情代码响应包含 `item_title`；Apifox `Order` schema 当前未声明 `item_title`。
- `PayOrderRequest.result` Apifox schema 限定 enum `success`；当前代码只校验必填，不校验枚举值。

## 风险和建议

- 建议补充 Apifox 中保证金接口和订单 `status` / `item_title` 字段，避免前后端契约偏差。
- 建议为 `orders.item_id` 增加唯一约束或事务保护；当前服务按 `FindOrderByItemID` 实现幂等，但数据库层没有唯一约束。
- 建议补充支付 handler 层测试，覆盖缺少 `result`、非法 `result` 和 `orderSvc == nil`。
- 建议补充保证金终态 `refunded` / `forfeited` 再支付拒绝的接口或集成测试。
- 建议补充支付/取消并发 runner，验证最终状态唯一性。

## 建议沉淀的回归测试

- 同一订单支付和取消并发，最终只能落入 `paid` 或 `cancelled`。
- 同一商品并发创建订单，最终最多一条订单。
- 缺少 `result` 或非法 `result` 的支付请求不能改变订单状态。
- `refunded` / `forfeited` 保证金记录不能再次支付覆盖为 `paid`。
- 保证金支付后才允许有保证金要求的拍品出价。

## 已知缺口

- 本轮接口测试通过 DB 直接准备 `auction_items`、`auction_rules`、`orders`，没有覆盖完整竞拍成交链路。
- 本轮没有验证商家查询订单详情和列表的正向路径。
- 本轮没有验证订单列表未知 `status` 查询行为。
- 本轮没有做 Redis 状态断言，因为目标模块不直接读写 Redis。
- 本轮没有跑 Apifox 同步，只记录了对齐偏差。

## 测试数据清理结果

- 测试批次 ID：`agent_opd_20260524233000`
- 清理方式：按 batch 前缀删除本轮 runner 创建的 `deposits`、`orders`、`auction_items`、`auction_rules`、`users`
- 清理结果：
  - `deposits`: 1 行
  - `orders`: 3 行
  - `auction_items`: 2 行
  - `auction_rules`: 2 行
  - `users`: 3 行
- 未清理原因：无
