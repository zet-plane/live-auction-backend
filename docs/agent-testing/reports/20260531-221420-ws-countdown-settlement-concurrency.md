# 测试报告：ws-countdown-settlement

## 基本信息

- 测试目标：验证单实例 Redis 权威倒计时、并发出价、Redis 结算、WebSocket snapshot/time_sync/auction_ended 与 MySQL/Redis/订单最终状态一致性。
- 测试类型：并发一致性 / 状态一致性 / WebSocket 场景测试。
- 测试时间：2026-05-31 22:14:20 +08:00。
- 执行 agent：主 agent Codex。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无 subagent 冲突；当前验收范围为单实例，双实例 WebSocket 广播结果仅作为后续多实例扩容风险记录。
- Subagent cleanup：未使用。
- 并行数据隔离证明：所有真实依赖数据使用批次 `agent_ws_settlement_concurrency_20260531215802` 和同名前缀；未使用 subagent 并行。
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/templates/protocol.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/guides/environment.md`、`docs/agent-testing/guides/concurrency.md`、`docs/agent-testing/guides/go-runner.md`、`docs/agent-testing/flows/auction-lifecycle.md`、`docs/agent-testing/modules/bid.md`、`docs/agent-testing/modules/item.md`、`docs/agent-testing/modules/ws.md`、`docs/superpowers/specs/2026-05-31-ws-countdown-settlement-design.md`。

## 测试环境

- 服务地址：本地服务，地址已省略。
- 配置来源：`config.yaml`；第二 worker 使用 `/private/tmp/live-auction-config-18081.yaml`。
- MySQL：本地测试 MySQL，地址和凭据已省略。
- Redis：本地测试 Redis，地址和凭据已省略。
- Apifox：未执行接口契约对齐。
- WebSocket：真实 WebSocket 客户端，ticket 值未记录。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实本地测试库 | 验证最终拍品、出价日志和订单持久化状态。 |
| Redis | 真实本地 Redis | 验证 Lua 原子状态、`auction:ending`、ranking 和最终 snapshot。 |
| WebSocket | 真实客户端连接 | 验证 `auction_snapshot`、`time_sync`、`auction_ended` 实际投递。 |
| 外部服务 | 未使用 | 支付、短信、物流等不在本次测试范围。 |

## 测试数据

- 测试批次 ID：`agent_ws_settlement_concurrency_20260531215802`
- 创建数据：测试商家 1 个、测试用户 5 个、测试房间、测试拍品 5 个、测试出价日志、测试订单。
- 复用数据：无业务数据复用；复用本地 MySQL/Redis 服务。

## 执行步骤

1. 启动本地 MySQL/Redis 容器并确认 healthy。
2. 启动主后端服务，并通过 `/health` 确认 MySQL/Redis OK。
3. 启动第二后端实例，作为超出当前验收范围的后续多实例探针。
4. 运行真实依赖 Go runner：注册测试用户/商家、创建房间/拍品、建立 WebSocket、发起并发出价、采集 Redis/MySQL/WebSocket 证据并清理。
5. 首轮 runner 结果：当前单实例相关一致性场景通过；双实例探针暴露后续多实例 WebSocket 广播风险。
6. 停止第二后端实例。
7. 单实例重跑 near-end WebSocket 场景，结果 2 PASS / 0 FAIL。
8. 停止主后端服务，并确认本地服务端口已释放。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 环境健康 | `/health` 返回 `mysql.status=ok`、`redis.status=ok` | 通过 |
| 多用户临近结束并发出价，最终 Redis/MySQL/订单一致 | 首轮 runner near-end CASE：Redis `status=ended leader=<最高价用户> deal_price=1500`，DB `status=ended winner=<同用户> deal=1500`，orders=1 | 状态通过 |
| 多用户临近结束并发出价，所有 WebSocket 客户端收到 `auction_ended` | 单实例复测 near-end CASE：`ws_after=auction_ended=3,auction_snapshot=3,bid_success=15,order_created=3,time_sync=12,user_outbid=3` | 通过 |
| 同一幂等 key 10 并发出价 | 首轮 runner same-idempotency CASE：10 个请求 HTTP 200；DB bid_logs=1；Redis `bid_count=1 participant_count=1`；idempotency key 存在 | 通过 |
| settlement 与最后一笔出价竞争 | 首轮 runner settlement-race CASE：late bid HTTP 200；Redis/MySQL 最终 winner/deal 均为 late bidder/1200；`auction:ending` 已移除 | 通过 |
| 两个 settlement worker 竞争同一 due item | 首轮 runner multi-worker CASE：两个后端实例运行；Redis/MySQL 最终 winner/deal 一致；orders=1；`auction:ending` 已移除 | 多实例探针通过，非当前验收范围 |
| 多实例 WebSocket 广播探针 | 首轮 runner near-end CASE：客户端连接主实例，结算可能由第二实例 claim；Redis/MySQL/订单一致但 `ws_after` 缺少 `auction_ended` | 后续多实例风险 |
| 单实例 WebSocket near-end 场景 | 单实例复测：5 个并发出价均 HTTP 200；Redis/MySQL/订单一致；`ws_after=auction_ended=3,auction_snapshot=3,bid_success=15,order_created=3,time_sync=12,user_outbid=3` | 通过 |
| 清理 | 首轮和单实例复测均输出 CLEANUP；Redis item/room key 删除成功，DB orders/deposits/bid_logs/rules/items/rooms/users 按批次删除成功 | 通过 |
| 服务停止 | 停止主服务后 `/health` 连接失败，说明测试服务已释放端口 | 通过 |

## 通过项

- Redis Lua 幂等出价通过真实并发验证：同 key 10 并发只产生一次副作用。
- Redis 结算和最后一笔出价竞争时，最终 Redis/MySQL 状态一致，没有双重最终结果。
- 两个后端实例同时运行 settlement worker 时，Redis claim 和订单创建保持唯一。
- 单实例 WebSocket 场景下，三个客户端能收到 `auction_snapshot`、`time_sync`、`auction_ended` 和 `order_created`，最终字段与 Redis/MySQL 一致。
- 所有测试数据按批次完成清理。

## 失败项

- 当前单实例验收范围内无失败项。

## 后续多实例风险

### 风险场景

双后端实例运行时，near-end 多用户出价完成后，Redis/MySQL/订单已正确结算，但连接在主实例 Hub 上的 WebSocket 客户端没有收到 `auction_ended`。

### 复现步骤

1. 启动主后端实例。
2. 启动第二后端实例，共用同一 MySQL/Redis，但各自拥有独立内存 Hub。
3. 用主实例建立 3 个 WebSocket 客户端连接同一房间。
4. 发起 5 个并发出价，最高价为 1500。
5. 将该 item 的 Redis `end_time_unix_ms` 调整为 due，等待 settlement worker 结算。

### 期望结果

Redis/MySQL/订单最终状态一致，且三个 WebSocket 客户端都收到 `auction_ended`，payload 中 `leader_user_id` 和 `deal_price` 与最终状态一致。

### 实际结果

Redis/MySQL/订单一致，但 WebSocket 消息摘要缺少 `auction_ended`：

```text
ws_after=auction_snapshot=3,bid_success=3,time_sync=15
```

同一场景停止第二实例后单实例复测通过：

```text
ws_after=auction_ended=3,auction_snapshot=3,bid_success=15,order_created=3,time_sync=12,user_outbid=3
```

### 相关证据

首轮 runner near-end CASE：

```text
DB: auction_items status=ended winner=<最高价用户> deal=1500; orders=1
REDIS: state status=ended leader=<同用户> deal_price=1500 end_reason=time_expired ending_contains=false
RESULT: FAIL ... ws_after 缺少 auction_ended
```

单实例复测 near-end CASE：

```text
RESULT: PASS ... ws_after=auction_ended=3 ... redis/db/orders 一致
```

### 可能原因

WebSocket Hub 是进程内内存结构。多实例部署时，如果实例 B 的 settlement worker claim 并广播 `auction_ended`，但客户端连接在实例 A，实例 A 的 Hub 不会收到实例 B 的本地广播。

### 影响范围

单实例部署不受影响；多实例或多 worker 部署下，实时结束事件可能丢给连接在其他实例上的客户端。重连 snapshot/HTTP 查询仍可恢复最终状态，但“所有客户端收到 `auction_ended`”这一设计目标未满足。

### 建议修复点

将业务事件 fanout 从进程内 Hub 扩展为跨实例事件总线，例如 Redis Pub/Sub、Redis Stream 或集中 WebSocket gateway；settlement worker 结算后发布跨实例事件，各实例 Hub 订阅并投递给本地连接。

### 建议新增的回归测试

保留本次双实例 runner 场景作为多实例 WebSocket 广播一致性回归测试：两个服务实例共用 Redis/MySQL，客户端连接实例 A，settlement 可能由实例 B claim，断言实例 A 客户端仍收到 `auction_ended`。

## 跳过项

- Apifox 对齐：本次不是接口契约测试。
- 大规模性能压测：本次只验证一致性，不评估吞吐或容量。
- 真实第三方支付/短信/物流：不在目标范围。

## Apifox 对齐偏差

- 不适用，本次未执行接口契约测试。

## 风险和建议

- 当前 Redis/MySQL 终态一致性表现良好，订单唯一性也通过双 worker 验证。
- 多实例 WebSocket 广播是明确缺口；如果生产计划横向扩容 WebSocket 或 settlement worker，需要优先修复。
- Runner 中为加速测试，通过测试夹具方式调整本批次 Redis `end_time_unix_ms` 和 `auction:ending`；这不影响业务不变量判断，但应在后续端到端测试中增加自然倒计时版本。

## 建议沉淀的回归测试

- 单实例 WebSocket 完整链路：snapshot/time_sync/bid_success/auction_ended/order_created。
- 双实例 WebSocket 广播一致性：跨实例 settlement 后本地 Hub 客户端也收到结束事件。
- 幂等 key 并发出价：DB/Redis 副作用只发生一次。
- settlement vs last bid：最终结果必须能由 Redis `end_time_unix_ms` 和 Lua claim 顺序解释。

## 已知缺口

- 未验证 Apifox 文档字段。
- 未跑性能压测。
- 未覆盖真实客户端断网重连后的 HTTP + WS 全链路 UI 行为，只验证了服务端消息和状态。

## 测试数据清理结果

- 测试批次 ID：`agent_ws_settlement_concurrency_20260531215802`
- 创建的数据：测试用户、商家、房间、拍品、出价日志、订单、Redis item/room/idempotency key。
- 清理方式：runner cleanup 按 item ID、room ID、account/title 前缀和 Redis key 精确清理。
- 清理结果：Redis DEL/ZREM 均 `err=<nil>`；DB 删除 orders=3+1、bid_logs=5+5、auction_rules=4+1、auction_items=4+1、live_rooms=1+1、users=6+6；deposits=0。
- 未清理原因：无。
