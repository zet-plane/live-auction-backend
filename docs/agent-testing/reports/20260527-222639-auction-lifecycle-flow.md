# 测试报告：auction-lifecycle

## 基本信息

- 测试目标：验证拍卖基础端到端流程中 MySQL、Redis、WebSocket 的状态一致性、状态流转、缓存一致性和业务链路可运行性。
- 测试类型：flow / end-to-end / state-consistency
- 测试时间：2026-05-27 22:26:39 Asia/Shanghai
- 执行 agent：Codex
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/guides/environment.md`、`docs/agent-testing/flows/auction-lifecycle.md`、`docs/agent-testing/modules/{user,room,item,deposit,bid,ws}.md`、`docs/agent-testing/reports/README.md`

## 测试环境

- 服务地址：本地服务 `http://127.0.0.1:8080`
- 配置来源：临时 E2E 配置 `/tmp/live-auction-e2e.yaml`
- MySQL：本地 Docker MySQL，地址和凭据已省略
- Redis：本地 E2E Redis `127.0.0.1:6380`
- Apifox：未使用
- WebSocket：`/ws/v1/rooms/{room_id}`，ticket 已脱敏

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实本地测试库 | 验证注册、房间、拍品、保证金、出价日志、结算状态的持久化一致性 |
| Redis | 真实本地测试 Redis | 验证 room queue、item state、ranking、bidder_names、idempotency key 和缓存清理 |
| WebSocket | 真实 gorilla/websocket 客户端 | 验证 A/B/C 实时通知、广播、单播和结算事件 |
| 外部服务 | 未使用 | 当前流程不依赖真实支付、物流、短信或第三方服务 |

## 测试数据

- 测试批次 ID：`agent_e2e_20260527222639`
- 创建数据：4 个测试用户、1 个直播间、2 个拍品、2 条拍品规则、2 条保证金记录、6 条成功出价日志、Redis room/item/ranking/idempotency keys。
- 复用数据：本地服务、MySQL schema、Redis 实例。

## 执行步骤

1. 启动本地后端服务和 E2E Redis，并确认健康检查通过。
2. 构建临时 runner：`/tmp/agent-runner-auction-e2e/main.go`。
3. 执行编译检查：`rtk env GOCACHE=/tmp/live-auction-go-cache go test`，结果通过。
4. 执行完整 runner，输出保存到 `/tmp/agent-runner-auction-e2e/runner.out`。
5. runner 完成后执行内置 cleanup，仅清理本批次数据。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 用户和商家身份 | 注册 4 个测试账号；商家 `PUT /api/v1/users/me` 后 MySQL `identity=merchant` | 通过 |
| 直播间状态 | `POST /api/v1/merchant/room`、`POST /api/v1/rooms/{room_id}/start`；MySQL `live_rooms.status=live`，Redis room state `status=live` | 通过 |
| 多拍品创建和上架 | 创建 2 个拍品；MySQL item/rule 双向映射正确；`start_price=1000`、`bid_increment=100`、`deposit_amount=5000`；Redis room queue 包含两件拍品 | 通过 |
| 倒计时 | 开始 item1 后 MySQL/HTTP 状态为 `ongoing`，Redis `current_price=1000`，HTTP `remaining_ms` 递减 | 通过 |
| 保证金门禁 | A 未交保证金出价返回 `deposit required`；Redis state/ranking、MySQL bid_logs、WebSocket 快照均未变化 | 通过 |
| WebSocket 通知 | A/B/C 均连接成功，均收到 `auction_started`、6 条 `bid_success`、`auction_ended`；C 未收到 `user_outbid` | 通过 |
| A/B 多轮出价 | A/B 交替出价 1100、1200、1300、1400、1500、1600；Redis 最终价 1600，leader=B；MySQL 6 条 bid_logs 顺序和金额正确 | 通过 |
| 排行榜一致性 | HTTP ranking 第一名 B/1600、第二名 A/1500；MySQL 聚合一致；Redis ranking 包含 A/B、不包含 C | 通过 |
| 结算一致性 | cron 后 MySQL item `ended`、winner=B、deal_price=1600；Redis item state 删除；A/B/C 收到 `auction_ended` | 通过 |
| 结束后拒绝出价 | A 在 ended 后出价 1700 返回 400；MySQL winner/deal/bid_logs、Redis state/ranking/bidder_names、WebSocket 快照均未变化 | 通过 |
| 清理 | cleanup 删除本批次 deposits/bid_logs，软删除 items/room/users，删除本批次 Redis keys 和 idempotency keys | 通过 |

## 通过项

- 14 个 E2E 场景全部通过，runner summary：`PASS: 14  FAIL: 0`。
- 本次覆盖了多件商品上架、倒计时、保证金前置条件、A/B 多轮长链路出价、C 旁观者 WebSocket、结算和结束后拒绝出价。
- MySQL、Redis、HTTP、WebSocket 在最终业务状态上保持一致。

## 失败项

- 无。

## 跳过项

- 并发出价测试：本次按需求不覆盖；风险是 Redis Lua 与 MySQL bid_logs 在并发下的一致性仍需单独验证。
- 一口价成交：本次规则未设置 `price_cap`；风险是一口价路径的 ended 状态和订单创建需单独验证。
- 订单支付/履约：当前流程只验证拍卖结算，不验证订单支付、物流、鉴定。

## Apifox 对齐偏差

- 未执行 Apifox 对齐测试；无接口契约偏差结论。

## 风险和建议

- WebSocket 客户端需要心跳保活；runner 已发送 `ping`，否则等待 cron 结算时连接可能因读超时断开。
- 建议将本 runner 的核心断言沉淀为可重复执行的集成测试脚本，并把 batchID、Redis 地址、DSN 全部改为环境变量。
- 建议后续单独补并发出价、一口价成交、订单创建和 Redis 故障降级场景。

## 建议沉淀的回归测试

- `auction_lifecycle_basic_flow_e2e`：注册、房间、两件商品、WebSocket、保证金、A/B 多轮出价、结算、清理。
- `auction_deposit_gate_e2e`：未交保证金拒绝且 DB/Redis/WS 不变。
- `auction_passive_ws_observer_e2e`：旁观用户只收广播，不产生保证金、出价、ranking 业务数据。
- `auction_settlement_e2e`：cron 结算后 MySQL ended、Redis item state 删除、WebSocket `auction_ended` 广播一致。

## 已知缺口

- 未覆盖并发出价。
- 未覆盖一口价和订单创建。
- 未覆盖服务重启后的 Redis/MySQL 恢复路径。
- 未覆盖 Redis 写失败或 MySQL 写失败的故障注入场景。

## 测试数据清理结果

- 测试批次 ID：`agent_e2e_20260527222639`
- 清理方式：runner 内置 cleanup，仅按已知 item/room/user IDs 和 batchID 前缀清理。
- MySQL 清理结果：deposits 2 行删除；bid_logs 6 行删除；auction_items 2 行软删除；live_rooms 1 行软删除；users 4 行软删除。
- Redis 清理结果：item1 state/ranking/bidder_names 删除 2 个 key；item1 idempotency key 删除 6 个；item2 相关 key 无残留；room state/online_users 删除 1 个 key；room queue 移除 2 个 item。
- 未清理原因：无。
