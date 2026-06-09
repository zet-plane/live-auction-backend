# 可用性依赖故障集成测试计划

## 基本信息

- 计划时间：2026-06-10 Asia/Shanghai
- 计划目标：验证可用性模块在主 Redis 断开和 MySQL 运行时断开时的服务存活、降级模式、恢复行为以及 HTTP / MySQL / Redis / WebSocket 可见数据一致性。
- 目标能力：availability failover、Redis authority 切换、MySQL buffering、settlement pause、数据恢复对账。
- 关联模块：`item`、`bid`、`room`、`deposit`、`order`、`ws`。
- 计划来源：`docs/agent-testing/guides/failover.md`，并参考竞拍全生命周期、出价、房间、保证金、订单和 WebSocket 模块契约。
- 执行环境：本地或线上等价真实依赖，优先使用本地隔离 MySQL 数据库、主 Redis、备用 Redis 和本地后端服务。
- 计划状态：待用户 review 和批准后执行；本计划只生成测试设计，不连接真实依赖、不制造故障。

## 文档路线

本次按 Redis/MySQL 故障切换验证路线读取：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/environment.md
docs/agent-testing/guides/failover.md
docs/agent-testing/flows/auction-lifecycle.md
docs/agent-testing/modules/bid.md
docs/agent-testing/modules/room.md
docs/agent-testing/modules/deposit.md
docs/agent-testing/modules/order.md
docs/agent-testing/modules/ws.md
```

执行后写报告时再读取：

```text
docs/agent-testing/reports/README.md
```

## 通用测试计划字段

测试目标：

- 主 Redis 断开后，后端进程仍存活，健康状态进入可观测降级模式，已完成本地重建的拍品继续接受出价，无法安全重建的拍品进入保护并拒绝写入。
- MySQL 运行时断开后，已预热的 Redis 热路径只在配置的 10 秒 buffering window 内接受出价，结算、订单创建、保证金退款和最终赢家确认暂停；MySQL 恢复后 bid-log backlog 落库并通过对账后再恢复结算。
- 恢复后验证 HTTP 查询、MySQL 表、Redis state/ranking/stream、WebSocket 增量消息和日志中的关键状态一致。

测试范围：

- `/livez`、`/readyz`、`/health` 或 `/api/v1/health` 的模式和组件状态。
- 房间、拍品、出价、排行榜、保证金、订单和 WebSocket ticket / reconnect 路径。
- 本批次创建的 auction item、auction rule、room、user、deposit、bid log、order 和 Redis keys。
- 本地双 Redis authority：主 Redis 为 cloud authority，备用 Redis 为 local authority。

禁止范围：

- 不测试生产真实用户、真实订单、真实支付、短信、物流、退款或第三方服务。
- 不清空数据库或 Redis，不执行 `DROP`、`TRUNCATE`、`FLUSHALL`、`FLUSHDB`。
- 不修改、删除或批量扫描清理非本批次数据。
- 不在报告中写入 DSN、Redis 密码、线上地址、完整 token、完整 ticket 或可复用凭据。

测试类型：

- Agent 集成测试。
- Agent 全流程测试。
- 状态一致性测试。
- 故障切换 / 降级恢复测试。

测试数据：

```text
测试批次 ID：agent_availability_20260610HHMMSS
账号前缀：agent_availability_<batch>_
商家前缀：agent_availability_merchant_<batch>_
房间标题前缀：agent_availability_room_<batch>_
拍品标题前缀：agent_availability_item_<batch>_
幂等 key 前缀：agent_availability_<batch>_
Redis key 范围：本批次 room/item/user/ticket/auction keys
```

依赖策略：

- MySQL：真实测试库或本地隔离库，只操作本批次数据。
- Redis：真实主 Redis + 备用 Redis，用于 authority 切换、ticket 落点、auction state/ranking/stream 对账。
- HTTP：真实后端服务。
- WebSocket：真实 ticket 和 WebSocket client；WebSocket 只作为实时增量证据，最终事实以 HTTP / MySQL / Redis 对账为准。
- 故障注入：优先阻断后端到主 Redis 或 MySQL 的连接；如果在线上等价环境执行，故障注入方式必须由用户批准，且不得影响非测试服务。

执行步骤：

1. 执行前置就绪检查，不制造故障。
2. 创建本批次账号、商家、房间、拍品、规则和必要保证金。
3. 记录 baseline `/livez`、`/readyz`、`/health`、HTTP 房间/拍品/排行榜、Redis state/ranking/stream 和 MySQL 表摘要。
4. 执行主 Redis 断开场景。
5. 恢复主 Redis，验证 control-plane switchback 必须等待本批次 item state 对齐后再接受 cloud authority 写入。
6. 执行 MySQL 运行时断开场景。
7. 恢复 MySQL，验证 backlog drain、settlement resume、订单和保证金终态。
8. 执行交叉延伸场景：Redis 断开期间 WebSocket reconnect、MySQL buffering 超时保护、冷路径出价保护。
9. 生成报告并清理或记录保留原因。

验证方式：

- HTTP 响应摘要、业务码和关键字段。
- MySQL 查询摘要：`auction_items`、`auction_rules`、`bid_logs`、`deposits`、`orders`、`live_rooms`。
- Redis 查询摘要：active authority、item state、ranking、bidder names、idempotency key、stream pending/ack/dead、room state、ticket key 落点。
- WebSocket 消息摘要：ticket 获取、握手、重连后 snapshot / bid event / order event。
- 日志摘要：availability mode、rebuild、protected、buffering、bid-log worker、settlement paused/resumed。

预计输出：

- 测试报告：`docs/agent-testing/reports/<timestamp>-availability-dependency-fault.md`
- runner 或手工证据摘要：记录命令、退出码、HTTP 摘要、DB/Redis 摘要、日志检索方式和清理结果。

## 前置就绪检查

- `rtk go test ./...` 或至少 `rtk go test ./internal/app/item/... ./internal/app/room/... ./internal/app/deposit/... ./internal/app/order/... ./internal/app/ws/...` 无编译错误；若失败，先记录阻塞，不执行真实依赖故障测试。
- 本地或线上等价后端 `/livez` 返回 200。
- `/health` 或 `/api/v1/health` 显示 `mode=normal_cloud`、`active_redis=cloud`、`mysql_state=healthy`。
- 主 Redis、备用 Redis 和 MySQL 可达；后端配置的 `redis_failover_threshold` 和 `mysql_buffering_window` 已记录为脱敏摘要。
- 测试 batch ID、数据前缀、清理 SQL/Redis key 范围已确定。
- 已准备至少 3 个普通用户、1 个商家、1 个 live room、3 个 ongoing item：
  - `P_redis_ready`：用于主 Redis 断开后从 MySQL 重建并继续出价。
  - `P_redis_protected`：用于制造本批次 item 级 protected，验证拒绝写入但服务不崩。
  - `P_mysql_hot`：`deposit_amount=0`，用于 MySQL down 时验证已预热 hot path 的 10 秒 buffering。
  - `P_mysql_settlement`：`deposit_amount>0`，用于验证 MySQL down 期间 settlement / order / deposit side effects 暂停，恢复后再结算。

## 场景矩阵

| 场景 ID | 场景 | 故障 | 核心预期 |
| --- | --- | --- | --- |
| A0 | baseline 正常竞拍 | 无 | 正常 cloud Redis + MySQL 下创建房间、开始拍品、出价、排行榜、WebSocket 事件一致 |
| A1 | 主 Redis 断开，服务存活 | 主 Redis 不可达 | `/livez` 仍 200；health 进入 local Redis 或 protected 可观测状态 |
| A2 | 主 Redis 断开，ready item 继续出价 | 主 Redis 不可达，备用 Redis 可达 | `P_redis_ready` 在本地 Redis rebuild 后接受出价；Redis/HTTP/MySQL 最终一致 |
| A3 | 主 Redis 断开，protected item 拒绝出价 | 主 Redis 不可达，item 无法安全重建 | `P_redis_protected` 写请求被拒绝，服务不崩，其他 ready item 不受影响 |
| A4 | 主 Redis 恢复后验证式恢复 | 主 Redis 恢复 | 不因 ping 成功立即丢失 local authority 已接受写入；switchback 前后 item state 对齐 |
| A5 | Redis authority 切换中的 WebSocket | 主 Redis 断开与恢复 | 旧 ticket 可失败但新 ticket 可落到 active Redis；reconnect 后通过 HTTP snapshot 恢复最终状态 |
| B0 | MySQL down 前热路径预热 | 无 | `P_mysql_hot` 的 Redis state/hot config/ranking 已存在，保证金不依赖 MySQL |
| B1 | MySQL down 10 秒内出价 | MySQL 不可达，Redis 可达 | buffering window 内出价可成功进入 Redis 和 stream；MySQL bid log 暂未落库 |
| B2 | MySQL down 超过 10 秒 | MySQL 不可达，Redis 可达 | 新出价返回 availability/protected 类错误；不继续产生有效 bid side effects |
| B3 | MySQL down 期间 side effects 暂停 | MySQL 不可达 | settlement、order 创建、deposit refund/freeze/final winner confirmation 暂停 |
| B4 | MySQL 恢复后 backlog drain | MySQL 恢复 | Redis stream pending/new 消息落库，HTTP ranking、MySQL bid_logs 和 Redis state 对齐 |
| B5 | MySQL 恢复后结算恢复 | MySQL 恢复并对账通过 | settlement 恢复；订单唯一生成；非赢家保证金 refunded，赢家保证金按订单状态终态变化 |
| C1 | 未预热冷路径 MySQL down | MySQL 不可达 | 需要 MySQL 读取 item/rule/deposit 的冷路径拒绝写入，不误判为可用性成功 |
| C2 | Redis 与 MySQL 双故障边界 | 主 Redis 或 active Redis 与 MySQL 同时不可达 | 进入保护；不接受新竞拍写入；只保留安全读和 liveness |

## 场景 A：主 Redis 断开

### A0 baseline

执行：

1. 商家创建并开播房间。
2. 创建并上架 `P_redis_ready`、`P_redis_protected`。
3. 开始 `P_redis_ready`，用户 A 出价 1100，用户 B 出价 1200。
4. 查询房间详情、拍品详情、排行榜。
5. 获取 WebSocket ticket 并建立连接，记录后续出价事件。

验证：

- `/health` 包含 `mode=normal_cloud`、`active_redis=cloud`、`mysql_state=healthy`。
- `auction_items.status=ongoing`，`live_rooms.current_item_id=P_redis_ready`。
- Redis cloud state 当前价 1200、leader 为用户 B、ranking 第一为用户 B。
- `bid_logs` 最终包含 1100 和 1200 两条成功出价。
- WebSocket 收到与成功出价一致的 `bid_success` 或相关 market/control 事件。

### A1 服务存活和模式切换

执行：

1. 在后端进程已运行、baseline 已完成后阻断后端到主 Redis 的连接。
2. 等待超过 `redis_failover_threshold`。
3. 连续探测 `/livez`、`/readyz`、`/health`。

验证：

- `/livez` 仍返回 200。
- `/health` 或 `/readyz` 报告 active Redis 为 local，或报告 auction protected；不能输出 DSN、密码、完整地址或 token。
- 后端无 panic、fatal、进程退出或 pod restart。

### A2 ready item 本地 authority 出价

执行：

1. 在主 Redis 断开期间，对 `P_redis_ready` 发起下一笔合法出价 1300。
2. 如果首个请求触发 rebuild，允许一次短暂等待和重试，但只使用本批次幂等 key。
3. 查询 HTTP 拍品详情、排行榜、MySQL `bid_logs`、备用 Redis state/ranking。

验证：

- 出价最终成功，响应 `current_price=1300`、leader 为本次用户。
- 备用 Redis 中 `auction:item:{item_id}:state` 为 ready，authority epoch 与 health epoch 一致。
- Redis ranking 第一、HTTP ranking 第一、最新成功响应三者一致。
- `bid_logs` 最终包含 1300；若 rebuild 允许回退到已落库点，回退只出现在主 Redis 断开前未持久化的 Redis-only 出价上，报告必须明确记录。
- 主 Redis 不应继续接收该 item 的新 authority 写入。

### A3 protected item 拒绝

执行：

1. 使用 `P_redis_protected` 制造本批次 item 级无法安全重建状态。允许的故障注入仅限本批次 item 的 Redis authority metadata 或本批次 item 的重建前置数据，不修改非本批次数据。
2. 对 `P_redis_protected` 发起合法出价。
3. 同时对 `P_redis_ready` 再发起一笔合法出价 1400，验证隔离。

验证：

- `P_redis_protected` 返回 availability/protected 类错误，不产生新的 Redis ranking、bid-log stream 或 MySQL `bid_logs`。
- `/livez` 仍 200，其他 ready item 可以继续成功出价。
- health 或日志可见 protected item 计数、protected 原因或 rebuild failure 摘要。

### A4 主 Redis 恢复和验证式 switchback

执行：

1. 恢复主 Redis 连接。
2. 先只读检查 `/health`，确认不能仅凭 ping 成功就覆盖 local authority 已接受写入。
3. 通过本地控制面把 active Redis 切回 cloud，并等待 item 级 reconcile 完成。
4. switchback 后对 `P_redis_ready` 再出价 1500。

验证：

- switchback 前，local Redis 中 1300/1400 等 outage 期间出价没有丢失。
- switchback 后，主 Redis state/ranking 与 local Redis 和 MySQL `bid_logs` 对齐。
- 1500 出价成功后，HTTP、MySQL、active Redis 和 WebSocket 事件一致。
- 不生成重复 `bid_logs`、重复 winner 或重复 order。

### A5 WebSocket reconnect

执行：

1. 主 Redis 正常时获取 ticket 并连接 WebSocket。
2. 主 Redis 断开后，尝试使用旧 ticket 或旧连接继续接收；再重新获取 ticket 并连接。
3. 通过 HTTP 房间详情、拍品详情和排行榜恢复最终状态。

验证：

- 旧 ticket 在 authority 切换后失败是可接受行为，但不能泄漏完整 ticket。
- 新 ticket 必须写入 active Redis，握手成功后绑定正确 user/room。
- reconnect 后的最终 UI 恢复依据 HTTP snapshot；WebSocket 事件只作为增量证据。
- `online_count` / presence 可短暂 degraded，但不能影响出价、结算或 winner 确认。

## 场景 B：MySQL 运行时断开

### B0 热路径预热

执行：

1. 创建 `P_mysql_hot`，规则使用 `deposit_amount=0`，避免 MySQL down 后保证金校验阻断热路径。
2. 开始竞拍并在 MySQL 正常时完成至少一笔成功出价 1100。
3. 确认 Redis state、hot config、ranking、idempotency key 和 stream 可用。

验证：

- `P_mysql_hot` 的 Redis hot config 命中，不需要 MySQL 冷查询也能继续出价。
- MySQL `auction_items` 和 `bid_logs` 已有 baseline 状态。
- `/health` 为 `normal_cloud` / `mysql_state=healthy`。

### B1 10 秒 buffering 内接受出价

执行：

1. 在后端进程运行中阻断后端到 MySQL 的连接，保持 active Redis 可达。
2. 等待 health 进入 `mode=mysql_buffering`、`mysql_state=buffering`，记录 `mysql_buffering_started_at_unix_ms` 的脱敏摘要。
3. 在 buffering window 内对 `P_mysql_hot` 出价 1200、1300。
4. 查询 Redis state/ranking/stream；MySQL 查询如果不可用，只记录不可达证据，不判定业务失败。

验证：

- 1200、1300 在 10 秒窗口内成功，Redis 当前价为 1300，leader 为最后成功用户。
- `auction:bid_log:stream` 有本批次 bid event；worker 不应 ack 未落库消息。
- `/livez` 仍 200；`/health` 可见 buffering。
- 不在 MySQL 不可用时生成订单、退款保证金或确认最终 winner。

### B2 buffering 超时后保护

执行：

1. 保持 MySQL 不可达直到超过 `mysql_buffering_window`。
2. 对 `P_mysql_hot` 发起下一笔出价 1400。
3. 查询 Redis state/ranking/stream。

验证：

- 1400 返回 availability unavailable、auction protected 或等价保护错误。
- Redis 当前价仍为 1300，不新增 1400 ranking、idempotency 成功记录或 bid-log stream 事件。
- health 或日志包含 `mysql_buffering_timeout`、`auction_protected` 或等价原因。

### B3 side effects 暂停

执行：

1. 使用 `P_mysql_settlement` 准备 deposit-required 成交场景：用户 A/B 已缴保证金，已存在可结算竞拍状态。
2. 在 MySQL down / buffering 期间让拍品到期或触发结算扫描。
3. 记录 settlement 相关日志和 HTTP 查询。

验证：

- `SettleDueAuctions` 或结算路径记录 paused by availability state。
- 不创建新的 `orders`。
- 非赢家保证金不在 MySQL down 期间被错误改为 `refunded`。
- 赢家保证金不在订单生成前被提前释放。
- 最终 winner confirmation 暂停，不能产生半成功订单或保证金终态。

### B4 MySQL 恢复后 backlog drain

执行：

1. 恢复 MySQL。
2. 等待 health 回到 `mysql_state=healthy` 或恢复态结束。
3. 等待 bid-log worker 消费 pending/new stream。
4. 查询 MySQL `bid_logs`、Redis stream pending/ack、Redis state/ranking、HTTP ranking。

验证：

- MySQL `bid_logs` 包含 buffering window 内成功的 1200、1300。
- Redis stream 中本批次消息已 ack 或 pending 降到可解释范围；无本批次 malformed dead-letter。
- MySQL `bid_logs` 不重复；同一 `bid_id` 只出现一次。
- Redis 当前价/leader、HTTP ranking 第一、MySQL 最高出价一致。

### B5 恢复后结算恢复和业务终态

执行：

1. 在 B4 backlog drain 和对账通过后触发或等待 settlement。
2. 查询成交结果、订单详情/列表、保证金状态。
3. 对 winner 执行支付成功分支；另用独立本批次 item 覆盖 cancel/expired 分支。

验证：

- 每个成交商品最多 1 个订单，订单初始状态 `pending`。
- 订单 `item_id`、`user_id`、`price` 来自最终成交 item、winner 和成交价。
- 非赢家保证金在结算后为 `refunded`。
- 赢家保证金在订单支付前保持 `paid`。
- 赢家支付后订单为 `paid`，赢家保证金为 `refunded`。
- 独立 cancel/expired 分支中赢家保证金为 `forfeited`。
- `refunded` / `forfeited` 终态不被后续失败路径覆盖。

## 场景 C：延伸边界

### C1 未预热冷路径 MySQL down

执行：

1. 创建 `P_mysql_cold` 但不预热 Redis hot config，或清理本批次 hot config 后保留 MySQL 为唯一规则来源。
2. MySQL down 后发起出价。

验证：

- 请求失败，原因是冷路径无法读取 item/rule/deposit；不把它当作 buffering 成功缺陷。
- 不产生 Redis state/ranking/stream 副作用。
- 报告明确区分 hot path buffering 和 cold path fail-closed。

### C2 Redis 与 MySQL 双故障

执行：

1. 在 MySQL down 的基础上阻断 active Redis，或在主 Redis failover 时同时阻断 MySQL。
2. 只执行只读健康探测和本批次写请求保护验证。

验证：

- 新竞拍写入拒绝，服务仍保持 `/livez`。
- 不创建新 bid、order、deposit 终态或 winner confirmation。
- health 显示 degraded/protected；日志可定位双依赖不可用。

## 数据对账清单

| 数据源 | 对账字段 |
| --- | --- |
| HTTP health | `status`、`mode`、`active_redis`、`mysql_state`、`epoch`、`presence` |
| HTTP room/item | `room.status`、`current_item_id`、`item.status`、`current_price`、`leader_user_id`、`remaining_ms` |
| HTTP ranking | 第一名 `user_id`、`price`、当前用户 `rank`、分页排序 |
| HTTP order/deposit | 订单状态、订单价格、保证金状态、金额、终态时间 |
| MySQL `auction_items` | `status`、`winner_id`、`deal_price`、`room_id`、`rule_id` |
| MySQL `bid_logs` | `id`、`item_id`、`user_id`、`price`、`authority_epoch`、`auction_version`、去重 |
| MySQL `orders` | 唯一 `item_id`、`user_id`、`price`、`status`、`expired_at` |
| MySQL `deposits` | `item_id`、`user_id`、`amount`、`status`、`paid_at`、`refunded_at` |
| Redis item state | `authority_epoch`、`authority_state`、`status`、`current_price`、`leader_user_id`、`bid_count`、`auction_version` |
| Redis ranking | ZSET 第一名、每个用户最高价、参与人数 |
| Redis stream | 本批次 bid event、pending/ack、dead-letter、重复消费幂等 |
| Redis ws/presence | ticket key 落点、online users、online_count、presence degraded |
| WebSocket | `bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`order_created` 与最终状态一致 |
| 日志 | availability mode change、rebuild result、protected reason、buffering timeout、settlement paused/resumed、worker drain |

## 通过标准

- 主 Redis 断开不导致后端进程不可用，`/livez` 始终 200。
- 主 Redis 断开后，ready item 可在 local Redis authority 下继续出价；protected item 拒绝写入且不影响其他 item。
- 主 Redis 恢复后，不丢失 local Redis outage 期间已接受的本批次出价；control-plane switchback 前后有 item state 对齐证据。
- MySQL 断开后，已预热 hot item 只在 10 秒 window 内接受出价，超时后拒绝新出价。
- MySQL 断开期间 settlement、order、deposit refund/forfeit 和 winner confirmation 暂停。
- MySQL 恢复后，buffering 期间成功出价持久化到 `bid_logs`，无重复 bid log，Redis/HTTP/MySQL 最高价和 leader 一致。
- 恢复后结算只在 backlog drain 和 item 验证后进行，订单唯一，保证金终态符合流程契约。
- WebSocket 切换和重连不作为最终事实来源，最终状态能通过 HTTP / MySQL / Redis 恢复并一致。
- 所有报告和证据均脱敏；清理只作用于本批次数据。

## 失败输出要求

任一验证失败时，报告必须包含：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
违反的不变量：
违反位置：
影响范围：
可能原因：
建议修复点：
建议新增的回归测试：
```

## 清理策略

测试结束必须记录：

```text
测试批次 ID：
创建的数据：
复用的数据：
清理方式：
清理结果：
未清理原因：
```

允许清理：

- 本批次用户、商家、房间、拍品、规则、保证金、订单和 bid logs。
- 本批次 item/room/ticket/idempotency Redis keys。
- 本批次临时配置、runner 文件和本地测试进程。

禁止清理：

- 无批次前缀或无法证明归属的数据。
- 整库、整表、整 Redis DB。
- 线上或共享环境中的非本批次数据。

## 执行审批

执行本计划前，需要用户在对话中明确批准，并说明：

- 使用本地真实依赖还是线上等价依赖。
- 是否允许阻断后端到主 Redis 和 MySQL 的连接。
- 是否允许创建、保留或清理本批次测试数据。
- 是否使用主 agent 串行、subagent 串行或 subagent 并行执行。

批准示例：

```text
批准执行 availability dependency fault 计划，使用本地真实依赖，允许阻断本地后端到主 Redis/MySQL 的连接，批次前缀 agent_availability_，执行后写报告并清理本批次数据。
```
