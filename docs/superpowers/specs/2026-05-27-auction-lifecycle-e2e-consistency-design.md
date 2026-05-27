# 竞拍基础流程 E2E 一致性测试设计

## 背景

当前端到端测试目标不是并发、压测或故障注入，而是验证基础直播竞拍流程能否在真实依赖下跑通，并证明关键业务状态在 HTTP、MySQL、Redis 和 WebSocket 之间一致。

早期设计中的 `auction-session` 已被当前设计中的 `room` 替代。本测试设计不依赖 `docs/agent-testing/modules/auction-session.md`，而以 `room + item + bid + ws` 表达完整竞拍生命周期。

## 目标

验证以下闭环：

```text
商家激活房间
-> 房间开播
-> 创建拍品和竞拍规则
-> 拍品上架进入房间待拍队列
-> 开始竞拍并初始化 Redis 竞拍状态
-> 用户建立 WebSocket 连接
-> 用户 A 出价
-> 用户 B 出价并成为领先者
-> 查询排行榜
-> 竞拍结束并生成成交结果
-> 结束后继续出价被拒绝且状态不被污染
```

本设计重点验证：

- 业务主流程完整跑通。
- 房间状态流转正确：`idle -> live`。
- 拍品状态流转正确：`draft -> published -> ongoing -> ended`。
- 出价状态正确：当前价、领先用户、排行榜、BidLog 一致。
- Redis 缓存与 MySQL 最终状态不冲突。
- WebSocket 推送与最终 HTTP / MySQL / Redis 状态一致。

## 不覆盖范围

- 并发出价和竞态优先级。
- 大规模压测。
- Redis / MySQL / WebSocket 故障注入。
- 支付履约、物流、鉴定、短信和真实第三方服务。
- 早期 `auction-session` 模块。
- 未定义的竞拍结束与出价同时发生优先级。

## 读取文档

执行本 E2E 前按渐进式读取规则读取：

- `docs/agent-testing/README.md`
- `docs/agent-testing/agent-runner-guide.md`
- `docs/agent-testing/go-runner-guide.md`
- `docs/agent-testing/flows/auction-lifecycle.md`
- `docs/agent-testing/modules/room.md`
- `docs/agent-testing/modules/item.md`
- `docs/agent-testing/modules/bid.md`
- `docs/agent-testing/modules/ws.md`
- `docs/agent-testing/reports/README.md`

如果涉及启动服务、连接数据库、连接 Redis 或创建测试数据，再读取：

- `docs/agent-testing/environment.md`

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| 后端服务 | 真实本地服务 | 验证真实 HTTP handler、中间件、路由和业务 wiring |
| MySQL | 测试库或线上等价真实库，仅操作本批次数据 | 验证持久化状态、状态流转、BidLog 和成交结果 |
| Redis | 测试 Redis，仅操作本批次 key | 验证房间状态、待拍队列、竞拍实时状态和排行榜 |
| WebSocket | 真实连接 | 验证 ticket、房间连接和业务事件推送 |
| 第三方服务 | 不调用或 mock | 不属于基础竞拍闭环 |
| 并发请求 | 不使用 | 当前阶段不覆盖并发测试 |

## 测试数据

测试批次：

```text
batch_id = agent_e2e_<YYYYMMDDHHMMSS>
room_title = agent_e2e_<batch_id>_room
item_title = agent_e2e_<batch_id>_item
idempotency_key = agent_e2e_<batch_id>_<user>_<price>
```

准备数据：

- 商家 S 和有效 token。
- 用户 A、用户 B 和有效 token。
- 1 个房间，由商家 S 通过接口激活。
- 1 个拍品 P，绑定该房间。
- 竞拍规则 R：
  - `start_price = 1000`
  - `bid_increment = 100`
  - `price_cap = 0`
  - `start_time` 为当前可测试时间。
  - `end_time` 晚于开始时间，且便于触发过期结算。

所有数据必须带 batch ID 或由 runner 记录具体 ID。清理只允许操作本批次创建的数据和 key。

## 主流程

### 1. 商家激活房间

请求：

```text
POST /api/v1/merchant/room
```

验证：

- HTTP 返回 `room_` 前缀的 `room_id`。
- MySQL `live_rooms.status = idle`。
- 商家重复激活返回同一个 `room_id`。
- 同一商家在 MySQL 中只有一个未删除房间。

### 2. 房间开播

请求：

```text
POST /api/v1/rooms/{room_id}/start
```

验证：

- HTTP 成功。
- MySQL `live_rooms.status = live`。
- Redis `auction:room:{room_id}:state.status = live`。
- `GET /api/v1/merchant/room` 返回 `status = live`，actions 与 live 状态一致。

### 3. 创建拍品和规则

请求：

```text
POST /api/v1/items
```

验证：

- HTTP 返回 `item_` 前缀的 `item_id` 和 `rule_id`。
- MySQL `auction_items.status = draft`。
- MySQL `auction_items.room_id = room_id`。
- MySQL `auction_rules.item_id = item_id`。
- 商品与规则互相引用一致。

### 4. 拍品上架

请求：

```text
POST /api/v1/items/{item_id}/publish
```

验证：

- HTTP 成功。
- MySQL `auction_items.status = published`。
- Redis `auction:room:{room_id}:item_queue` 包含 `item_id`。
- `GET /api/v1/rooms/{room_id}` 返回的 `item_queue` 与 Redis ZSET 一致，且不是 `null`。

### 5. 开始竞拍

请求：

```text
POST /api/v1/items/{item_id}/start
```

验证：

- HTTP 成功。
- MySQL `auction_items.status = ongoing`。
- Redis `auction:item:{item_id}:state.current_price = 1000`。
- Redis state 中 `bid_count = 0`、`participant_count = 0`。
- Redis `end_time_unix` 与规则结束时间一致或在可解释范围内。
- 如 WebSocket 当前实现广播 `auction_started`，验证房间连接可收到该事件。

### 6. 建立 WebSocket 连接

请求：

```text
POST /api/v1/ws-ticket
GET /ws/v1/rooms/{room_id}?ticket=<ticket>
```

验证：

- 用户 A 和用户 B 均能建立真实 WebSocket 连接。
- ticket 被一次性消费，重复使用同一 ticket 不能再次连接。
- 连接能收到后续业务事件。
- WebSocket 事件只作为实时证据，最终状态仍以 HTTP / MySQL / Redis 为准。

### 7. 用户 A 出价 1100

请求：

```text
POST /api/v1/items/{item_id}/bids
```

请求体：

```json
{
  "price": 1100,
  "idempotency_key": "agent_e2e_<batch>_user_a_1100"
}
```

验证：

- HTTP `current_price = 1100`。
- HTTP `leader_user_id = user_a`。
- Redis item state `current_price = 1100`，`leader_user_id = user_a`。
- Redis ranking 中用户 A 分数为 1100。
- MySQL `bid_logs` 有用户 A 对该 item 的 1100 记录。
- WebSocket 收到 `bid_success`，payload 与 HTTP / Redis 状态一致。

### 8. 用户 B 出价 1200

请求：

```text
POST /api/v1/items/{item_id}/bids
```

请求体：

```json
{
  "price": 1200,
  "idempotency_key": "agent_e2e_<batch>_user_b_1200"
}
```

验证：

- HTTP `current_price = 1200`。
- HTTP `leader_user_id = user_b`。
- Redis item state `current_price = 1200`，`leader_user_id = user_b`。
- Redis ranking 第一名为用户 B，分数 1200。
- MySQL `bid_logs` 有用户 B 对该 item 的 1200 记录。
- WebSocket 收到 `bid_success`。
- 如果当前实现发送 `user_outbid`，用户 A 收到被超越事件，payload 指向用户 A 和当前领先用户 B。

### 9. 查询排行榜

请求：

```text
GET /api/v1/items/{item_id}/ranking
```

验证：

- HTTP 排行榜第一名是用户 B，价格 1200。
- HTTP 排行榜第二名是用户 A，价格 1100。
- Redis ranking 与 HTTP 排名一致。
- MySQL `bid_logs` 按用户最高价聚合后能解释 HTTP 排名。

### 10. 结束竞拍并验证成交

推荐使用短结束时间拍品，并通过当前实现支持的过期结算路径触发结束。如果执行环境无法稳定触发后台结算，可将该步骤标记为阻塞，并记录需要的结算触发方式；不要自行绕过业务接口修改状态。

验证：

- MySQL `auction_items.status = ended`。
- MySQL `auction_items.winner_id = user_b`。
- MySQL `auction_items.deal_price = 1200`。
- Redis item state 被清理，或处于文档定义的结束后状态。
- WebSocket 收到 `auction_ended`，payload 中 winner 和成交价与 MySQL 一致。
- 如果当前实现创建订单并广播 `order_created`，验证订单事件与最终成交结果一致；不测试支付履约。

### 11. 结束后继续出价被拒绝

请求：

```text
POST /api/v1/items/{item_id}/bids
```

请求体：

```json
{
  "price": 1300,
  "idempotency_key": "agent_e2e_<batch>_user_a_1300_after_end"
}
```

验证：

- HTTP 返回明确错误。
- MySQL 成交用户仍为用户 B。
- MySQL 成交价仍为 1200。
- MySQL 不产生新的有效成交状态。
- Redis ranking 和 item state 不被该失败出价污染。
- WebSocket 不应出现成功出价事件。

## 一致性矩阵

| 节点 | HTTP | MySQL | Redis | WebSocket |
| --- | --- | --- | --- | --- |
| 房间开播 | 房间视图 `live` | `live_rooms.status=live` | room state `status=live` | 不要求 |
| 拍品上架 | 商品状态 `published` | `auction_items.status=published` | room queue 包含 item | 不要求 |
| 开始竞拍 | 商品状态 `ongoing` | `auction_items.status=ongoing` | item state 初始化 | `auction_started` 如支持 |
| 用户 A 出价 | `current_price=1100` | BidLog A/1100 | leader=A，ranking=A/1100 | `bid_success` |
| 用户 B 出价 | `current_price=1200` | BidLog B/1200 | leader=B，ranking=B/1200 | `bid_success` / `user_outbid` |
| 排行榜 | B 第一，A 第二 | BidLog 聚合一致 | ZSET 排名一致 | 不要求 |
| 竞拍结束 | 商品状态 `ended` | winner=B，deal=1200 | 清理或结束状态一致 | `auction_ended` |
| 结束后出价 | 错误响应 | 成交结果不变 | 缓存不变 | 无成功事件 |

## 执行方式

使用 Go runner 采集结构化证据：

- 从 `docs/agent-testing/runner-template.go` 复制到 `/tmp/agent-runner-<batch>/main.go`。
- 按 `docs/agent-testing/go-runner-guide.md` 填写 batch、baseURL、Redis 和 `TEST_DSN`。
- 每个流程节点实现为一个 runner case。
- 每个 case 同时输出 HTTP、DB、Redis、WebSocket 证据。
- runner 结束时输出 summary 和 cleanup 结果。

不使用零散 curl 作为最终证据来源。

## 通过标准

本 E2E 通过必须满足：

- 主流程所有步骤成功执行。
- 房间状态、拍品状态、出价状态和成交状态符合定义流转。
- HTTP、MySQL、Redis 和 WebSocket 中的关键状态一致。
- Redis room queue、item state、ranking 与对应 HTTP / MySQL 结果不冲突。
- 结束后继续出价失败，且不会改变成交结果或缓存。
- 每条结论都有 runner 证据。
- 测试数据清理结果已记录。

## 失败报告

失败时按流程文档格式输出：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
影响范围：
建议修复点：
建议新增的回归测试：
```

如果是状态不一致，必须额外记录：

```text
不一致节点：
HTTP 证据：
MySQL 证据：
Redis 证据：
WebSocket 证据：
最终状态来源：
是否影响业务继续执行：
```

## 文档更新要求

`docs/agent-testing/flows/auction-lifecycle.md` 不应再引用不存在的 `auction-session` 模块契约，并应说明当前流程由 `room` 替代旧场次设计。关联模块应改为：

- `docs/agent-testing/modules/room.md`
- `docs/agent-testing/modules/item.md`
- `docs/agent-testing/modules/bid.md`
- `docs/agent-testing/modules/ws.md`
