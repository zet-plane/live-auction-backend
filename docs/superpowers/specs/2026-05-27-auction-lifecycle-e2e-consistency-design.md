# 竞拍基础流程 E2E 一致性测试设计

## 背景

当前端到端测试目标不是并发、压测或故障注入，而是验证基础直播竞拍流程能否在真实依赖下跑通，并证明关键业务状态在 HTTP、MySQL、Redis 和 WebSocket 之间一致。

早期设计中的 `auction-session` 已被当前设计中的 `room` 替代。本测试设计不依赖 `docs/agent-testing/modules/auction-session.md`，而以 `room + item + bid + ws` 表达完整竞拍生命周期。

## 目标

验证以下闭环：

```text
商家激活房间
-> 房间开播
-> 创建多件拍品和竞拍规则
-> 多件拍品上架进入房间待拍队列
-> 用户 A、B、C 建立 WebSocket 连接
-> 开始第一件拍品竞拍并初始化 Redis 竞拍状态和倒计时
-> 用户 A 未缴保证金出价失败
-> 用户 A 缴保证金后出价成功
-> 用户 B 缴保证金后与 A 多轮交替出价
-> 用户 C 只旁观并接收房间广播
-> 查询排行榜
-> 竞拍结束并生成成交结果
-> 结束后继续出价被拒绝且状态不被污染
```

本设计重点验证：

- 业务主流程完整跑通。
- 房间状态流转正确：`idle -> live`。
- 拍品状态流转正确：`draft -> published -> ongoing -> ended`。
- 多件拍品上架后房间待拍队列与 Redis 一致。
- 拍卖倒计时由规则结束时间和 Redis `end_time_unix` 正确驱动。
- 保证金是有保证金要求拍品的出价前置条件。
- 出价状态正确：当前价、领先用户、排行榜、BidLog 一致。
- A/B 多轮交替出价后，最终领先用户和成交结果正确。
- C 作为旁观用户，只接收房间广播，不产生保证金、出价或排行榜数据。
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
- `docs/agent-testing/modules/deposit.md`
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
| 保证金 | 真实保证金接口和 `deposits` 表 | 验证出价前置校验，不调用真实第三方支付 |
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
- 用户 A、用户 B、用户 C 和有效 token。
- 1 个房间，由商家 S 通过接口激活。
- 至少 2 个拍品，均绑定该房间：
  - `item_1`：执行完整竞拍、保证金、出价、成交链路。
  - `item_2`：验证同房间多件上架和待拍队列一致性，不参与本次成交。
- 竞拍规则 R：
  - `start_price = 1000`
  - `bid_increment = 100`
  - `deposit_amount = 5000`
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

### 3. 创建多件拍品和规则

请求：

```text
POST /api/v1/items
```

验证：

- 为同一房间创建 `item_1` 和 `item_2`。
- 两次 HTTP 均返回 `item_` 前缀的 `item_id` 和 `rule_id`。
- MySQL 中两件商品均为 `draft`。
- MySQL `auction_items.room_id = room_id`。
- MySQL `auction_rules.item_id` 分别指向对应商品。
- 两件商品的规则均包含 `deposit_amount = 5000`。
- 商品与规则互相引用一致。

### 4. 多件拍品上架并验证房间待拍队列

请求：

```text
POST /api/v1/items/{item_id}/publish
```

验证：

- `item_1` 和 `item_2` 上架 HTTP 均成功。
- MySQL 中两件商品均为 `published`。
- Redis `auction:room:{room_id}:item_queue` 同时包含 `item_1` 和 `item_2`。
- Redis ZSET 顺序与 `GET /api/v1/rooms/{room_id}` 返回的 `item_queue` 一致。
- `item_queue` 不是 `null`。
- 后续开始 `item_1` 竞拍后，`item_2` 仍可作为同房间待拍商品被追踪；如果当前实现不会从队列移除已开始商品，报告中按实现事实记录队列状态，不自行改写 Redis。

### 5. 建立 A/B/C WebSocket 连接

请求：

```text
POST /api/v1/ws-ticket
GET /ws/v1/rooms/{room_id}?ticket=<ticket>
```

验证：

- 用户 A、用户 B、用户 C 均能建立真实 WebSocket 连接。
- ticket 被一次性消费，重复使用同一 ticket 不能再次连接。
- C 不调用保证金、出价或其它业务写接口，只作为旁观连接。
- 后续所有 WebSocket 事件都要记录 A、B、C 各自是否收到。
- WebSocket 事件只作为实时证据，最终状态仍以 HTTP / MySQL / Redis 为准。

### 6. 开始 item_1 竞拍并验证倒计时

请求：

```text
POST /api/v1/items/{item_1_id}/start
```

验证：

- HTTP 成功。
- MySQL `auction_items.status = ongoing`。
- Redis `auction:item:{item_1_id}:state.current_price = 1000`。
- Redis state 中 `bid_count = 0`、`participant_count = 0`。
- Redis `end_time_unix` 与规则结束时间一致或在可解释范围内。
- `GET /api/v1/items/{item_1_id}` 返回的 `remaining_ms` 与 Redis `end_time_unix - now` 在合理误差内一致。
- 等待一个短时间窗口后再次查询，`remaining_ms` 应单调减少。
- 如 WebSocket 当前实现广播 `auction_started`，A、B、C 均应收到该房间广播。

### 7. 用户 A 未缴保证金出价失败

请求：

```text
POST /api/v1/items/{item_1_id}/bids
```

请求体：

```json
{
  "price": 1100,
  "idempotency_key": "agent_e2e_<batch>_user_a_1100_missing_deposit"
}
```

验证：

- HTTP 返回 `deposit required` 或等价明确错误。
- MySQL 不新增用户 A 的 `bid_logs`。
- Redis item state 仍为 `current_price = 1000`，`leader_user_id` 为空。
- Redis ranking 不包含用户 A。
- A、B、C 均不应收到 `bid_success`。

### 8. 用户 A 缴保证金

请求：

```text
POST /api/v1/items/{item_1_id}/deposit/pay
```

验证：

- HTTP 返回保证金状态 `paid`。
- MySQL `deposits.item_id = item_1_id`。
- MySQL `deposits.user_id = user_a`。
- MySQL `deposits.amount = auction_rules.deposit_amount = 5000`。
- MySQL `deposits.status = paid`。
- 同一 `item_id + user_id` 只有一条保证金记录。

### 9. A/B 多轮交替出价

按顺序执行以下出价，不使用并发：

| 步骤 | 用户 | 前置条件 | 出价 | 期望当前价 | 期望领先者 |
| --- | --- | --- | --- | --- | --- |
| 1 | A | 已缴保证金 | 1100 | 1100 | A |
| 2 | B | 未缴保证金 | 1200 | 1100 | A |
| 3 | B | 缴保证金 | N/A | 1100 | A |
| 4 | B | 已缴保证金 | 1200 | 1200 | B |
| 5 | A | 已缴保证金 | 1300 | 1300 | A |
| 6 | B | 已缴保证金 | 1400 | 1400 | B |
| 7 | A | 已缴保证金 | 1500 | 1500 | A |
| 8 | B | 已缴保证金 | 1600 | 1600 | B |

用户 B 的未缴保证金出价失败后，先调用：

```text
POST /api/v1/items/{item_1_id}/deposit/pay
```

再继续 B 的有效出价。

每一次失败出价验证：

- HTTP 返回明确错误。
- MySQL 不新增有效 `bid_logs`。
- Redis item state 和 ranking 不变。
- A、B、C 均不应收到 `bid_success`。

每一次成功出价验证：

- HTTP `current_price` 等于本轮期望当前价。
- HTTP `leader_user_id` 等于本轮期望领先者。
- Redis item state `current_price` 和 `leader_user_id` 与 HTTP 一致。
- Redis ranking 中该用户分数更新为本轮价格。
- MySQL `bid_logs` 新增一条对应用户和价格的记录。
- MySQL 对 A/B 的成功出价记录数量与执行步骤一致。
- 如果未触发自动延时，Redis `end_time_unix` 不应变化，HTTP `remaining_ms` 只随时间递减。
- 如果当前实现触发 `auction_extended`，则 Redis `end_time_unix`、HTTP `remaining_ms` 和 WebSocket `auction_extended` payload 必须一致。

WebSocket 验证：

- 每一次成功出价，A、B、C 都应收到房间级 `bid_success` 或当前实现定义的等价房间广播。
- 每一次领先者被替换，前领先者应收到 `user_outbid`。
- C 作为旁观者不应收到只面向 A 或 B 的用户单播。
- C 不应出现在 `deposits`、`bid_logs` 或 Redis ranking 中。
- 每条 WebSocket 事件 payload 的 `item_id`、`current_price`、`leader_user_id` 与 HTTP / Redis 结果一致。

### 10. 查询排行榜

请求：

```text
GET /api/v1/items/{item_1_id}/ranking
```

验证：

- HTTP 排行榜第一名是用户 B，价格 1600。
- HTTP 排行榜第二名是用户 A，价格 1500。
- Redis ranking 与 HTTP 排名一致。
- MySQL `bid_logs` 按用户最高价聚合后能解释 HTTP 排名。
- 用户 C 不在排行榜中。

### 11. 结束竞拍并验证成交

推荐使用短结束时间拍品，并通过当前实现支持的过期结算路径触发结束。如果执行环境无法稳定触发后台结算，可将该步骤标记为阻塞，并记录需要的结算触发方式；不要自行绕过业务接口修改状态。

验证：

- MySQL `auction_items.status = ended`。
- MySQL `auction_items.winner_id = user_b`。
- MySQL `auction_items.deal_price = 1600`。
- Redis item state 被清理，或处于文档定义的结束后状态。
- A、B、C 均收到房间级 `auction_ended`，payload 中 winner 和成交价与 MySQL 一致。
- 如果当前实现创建订单并广播 `order_created`：
  - 若为赢家单播，则只有 B 收到。
  - 若为房间广播，则 A、B、C 都收到。
  - 无论是哪种实现，都必须与当前 WebSocket 模块契约和最终成交结果一致。
- 不测试订单支付履约。

### 12. 结束后继续出价被拒绝

请求：

```text
POST /api/v1/items/{item_1_id}/bids
```

请求体：

```json
{
  "price": 1700,
  "idempotency_key": "agent_e2e_<batch>_user_a_1700_after_end"
}
```

验证：

- HTTP 返回明确错误。
- MySQL 成交用户仍为用户 B。
- MySQL 成交价仍为 1600。
- MySQL 不产生新的有效成交状态。
- Redis ranking 和 item state 不被该失败出价污染。
- A、B、C 均不应收到成功出价事件。

## 一致性矩阵

| 节点 | HTTP | MySQL | Redis | WebSocket |
| --- | --- | --- | --- | --- |
| 房间开播 | 房间视图 `live` | `live_rooms.status=live` | room state `status=live` | 不要求 |
| 多件拍品上架 | 商品状态 `published` | 两件 `auction_items.status=published` | room queue 包含 item_1/item_2 | 不要求 |
| 开始竞拍 | 商品状态 `ongoing`，倒计时可见 | `auction_items.status=ongoing` | item state 初始化，`end_time_unix` 正确 | A/B/C 收到 `auction_started` 如支持 |
| A 未缴保证金出价 | 错误响应 | 无 BidLog | state/ranking 不变 | 无 `bid_success` |
| A 缴保证金 | `paid` | Deposit A paid/5000 | N/A | 不要求 |
| A/B 多轮出价 | 每轮 current/leader 正确 | BidLog 逐轮增加 | state/ranking 逐轮一致 | A/B/C 收到房间广播，前领先者收到单播 |
| C 旁观 | N/A | 无 Deposit/BidLog | ranking 无 C | 只收房间广播，不收用户单播 |
| 排行榜 | B 第一 1600，A 第二 1500 | BidLog 聚合一致 | ZSET 排名一致 | 不要求 |
| 竞拍结束 | 商品状态 `ended` | winner=B，deal=1600 | 清理或结束状态一致 | A/B/C 收到 `auction_ended` |
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
- 多件拍品上架后，房间待拍队列与 Redis 一致。
- 拍卖倒计时与 Redis `end_time_unix` 一致，并随时间递减。
- 未缴保证金不能产生有效出价；缴纳保证金后可以出价。
- A/B 多轮交替出价后，最终排行榜和成交结果正确。
- C 只接收房间广播，不产生保证金、出价或排行榜数据。
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
- `docs/agent-testing/modules/deposit.md`
- `docs/agent-testing/modules/bid.md`
- `docs/agent-testing/modules/ws.md`
