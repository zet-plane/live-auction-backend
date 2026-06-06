# 测试报告：agent interfaces

## 基本信息

- 测试目标：验证模块接口化改造后的跨模块链路，包括 room -> item reader、item -> deposit/order、payment -> order。
- 测试类型：本地真实依赖接口 / 集成 / 状态一致性 smoke；本地单元回归。
- 测试时间：2026-06-06 16:51:49 - 17:02:04 Asia/Shanghai。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用，主 agent 串行执行。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/environment.md`、`guides/go-runner.md`、`reports/README.md`、`flows/auction-lifecycle.md`、`modules/{bid,deposit,item,room,ws,payment,order}.md`。

## 测试环境

- 服务地址：本地 loopback 服务，端口 `18080`，测试结束后已停止。
- 配置来源：临时本地测试配置，位于 `/private/tmp`，未写入报告敏感字段。
- MySQL：本地测试 MySQL，地址和凭据已省略。
- Redis：本地测试 Redis，地址和凭据已省略。
- Apifox：读取当前 OAS，下载时间为 2026-06-06T06:36:21.564Z。
- WebSocket：本次未建立真实 WebSocket 连接。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地真实测试库 | 验证订单、房间、商品、保证金状态一致性 |
| Redis | 本地真实测试 Redis | 验证 room queue、auction state、ranking |
| WebSocket | 未执行真实连接 | 本次改造重点是模块接口，不覆盖完整 WS 收发 |
| 外部服务 | 未使用 | 当前支付/保证金均为站内状态，不调用第三方 |

## 测试数据

- 测试批次 ID：`agent_interfaces_20260606165149`
- 创建数据：4 个测试用户、1 个测试房间、3 个测试拍品、2 条保证金记录、2 条订单、Redis room/item/ranking/idempotency 相关 key。
- 复用数据：本地 MySQL/Redis 基础服务；未复用业务数据。

## 执行步骤

1. 启动本分支后端于本地 `18080`。
2. 执行本地单元回归，发现并修复 `ListRoomFeed` 未使用 `ItemReader` 富化商品详情的问题。
3. 运行 agent-testing Go runner：`/private/tmp/agent-runner-agent_interfaces_20260606165149`。
4. runner 串行执行 11 个 CASE，并输出 `SUMMARY` / `CLEANUP`。
5. 运行全仓本地单元测试。
6. 停止临时后端服务，确认 `18080` 不再响应。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 服务可达 | `GET /api/v1/health` 返回 HTTP 200，`code=0` | PASS |
| 用户与商家准备 | 注册 4 个测试用户；商家 `PUT /api/v1/users/me` 后 DB identity 为 `merchant` | PASS |
| 房间启动 | `POST /merchant/room` 与 `POST /rooms/{room_id}/start` 返回 200；DB `live_rooms.status=live`；Redis room state `status=live` | PASS |
| 商品上架与待拍队列 | 创建并上架 2 个商品；DB 状态为 `published`；Redis room queue 包含两个 item ID | PASS |
| 房间详情商品富化 | `GET /api/v1/rooms/{room_id}` 返回 `item_queue` 两个 ID，`item` 列表也含两个完整 item | PASS |
| 房间 feed 商品富化 | `GET /api/v1/rooms/feed?limit=10` 找到本批次 room，返回 `item_queue` 和 `item` 两个完整 item | PASS |
| 保证金拦截 | 开始 P1 后，未缴保证金出价返回 HTTP 400，业务码 `40005 deposit required`；DB item 为 `ongoing`；Redis state 为 `ongoing` | PASS |
| 保证金放行与订单创建 | A/B 缴纳保证金成功；A 出价 1100 成功；B 一口价 2000 成交；DB item `ended`、winner 为 B、订单 `pending` | PASS |
| 支付接口 | `POST /orders/{order_id}/pay` 返回 200；订单详情和 DB 状态为 `paid` | PASS |
| 取消接口 | 第二个成交订单取消返回 200；取消后再支付返回 400；DB 状态保持 `cancelled` | PASS |
| 本地单元回归 | `go test -count=1 ./...` 全仓通过 | PASS |
| 服务停止 | 停止后 `GET /api/v1/health` 到 `18080` 连接失败，HTTP code `000` | PASS |

Runner 汇总：

```text
PASS: 11  FAIL: 0
BATCH_ID: agent_interfaces_20260606165149
```

全仓测试命令：

```text
rtk env GOCACHE=/tmp/live-auction-go-cache go test -count=1 ./...
```

结果：退出码 0。

## 通过项

- `room -> item` reader 接口在真实 HTTP + Redis 队列链路下可用。
- 房间详情与房间 feed 都能返回待拍队列和完整 item 列表。
- `item -> deposit` 保证金校验在真实接口链路下有效。
- `item -> order` 一口价成交建单在真实 DB 链路下有效。
- `payment -> order` 支付与取消接口在真实 HTTP + DB 链路下有效。
- runner 清理了本批次创建的数据和 Redis key。
- 全仓本地单元测试通过。

## 失败项

- 无。

## 跳过项

- WebSocket 真实连接和消息序列未执行。原因：本次接口化改造主要影响 service/module seam；runner 覆盖了 HTTP、MySQL、Redis 状态一致性，但未纳入真实 WebSocket 客户端。风险：无法证明 `auction_started`、`bid_success`、`auction_ended`、`order_created` 的真实连接收发。后续补测条件：按 `modules/ws.md` 使用真实 ticket 和 WebSocket client 增加业务事件序列测试。
- 并发一致性未执行。原因：本次批准范围不包含并发专项；`flows/auction-lifecycle.md` 明确并发出价应走专项并发测试。风险：不能证明接口化后并发支付/取消或并发出价的最终唯一性。后续补测条件：按 `guides/concurrency.md` 输出完整并发设计并获批。
- 性能压测未执行。原因：本次目标是接口化正确性，不是吞吐或延迟评估。

## Apifox 对齐偏差

- 当前 OAS 缺少代码已注册的 `GET /api/v1/rooms/feed` 路径。该偏差不阻塞本次业务测试，但会影响接口契约文档完整性，建议同步到 Apifox。

## 风险和建议

- 本次测试前发现 `ListRoomFeed` 未使用注入的 `ItemReader`，导致 feed 不返回完整 item 列表；已通过单元回归和真实 runner 验证修复。
- 建议将 `GET /api/v1/rooms/feed` 补充到 Apifox/OAS，并把 `RoomFeedResult.list[].item` 字段纳入契约。
- 建议后续补一条真实 WebSocket 业务事件 flow，验证订单创建事件不会因模块接口化被遗漏。

## 建议沉淀的回归测试

- 单元回归：`ListRoomFeed` 在有 item queue 时必须调用 `ItemReader` 并按 queue 顺序返回完整 item。
- 接口回归：发布商品后 `GET /api/v1/rooms/feed` 必须同时返回 `item_queue` 和 `item`。
- 流程回归：一口价成交后订单生成，支付和取消接口分别走到 order service 契约。

## 已知缺口

- 未覆盖真实 WebSocket 连接收发。
- 未覆盖并发出价、并发支付/取消。
- 未覆盖 Apifox 修复后的二次对齐验证。

## 测试数据清理结果

- 测试批次 ID：`agent_interfaces_20260606165149`
- 创建的数据：4 users、1 live_room、3 auction_items、3 auction_rules、2 deposits、2 orders、3 bid_logs，以及本批次 Redis room/item/ranking/idempotency key。
- 清理方式：runner 按本批次 item_id、room_id、user_id 精确删除 DB 记录；按本批次 room/item key 精确 `DEL` Redis key；未执行 `DROP`、`TRUNCATE`、`FLUSHALL` 或 `FLUSHDB`。
- 清理结果：Redis item key 删除计数分别为 5、1、4；Redis room key 删除计数为 2；DB 删除 `bid_logs=3`、`deposits=2`、`orders=2`、`auction_rules=3`、`auction_items=3`、`live_rooms=1`、`users=4`。
- 未清理原因：无。
