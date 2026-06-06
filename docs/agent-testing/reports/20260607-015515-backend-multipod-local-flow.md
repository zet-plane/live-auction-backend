# 测试报告：backend multipod local

## 基本信息

- 测试目标：验证刚实现的多实例 Redis event bus、WebSocket 跨实例投递、本地 readiness、最终 HTTP 状态恢复，以及相关 lease/Redis event bus 单元回归。
- 测试类型：本地真实依赖 flow + WebSocket 状态一致性 + 本地单元回归。
- 测试时间：2026-06-07 01:50-01:56 Asia/Shanghai。
- 执行 agent：主 agent，本地串行执行。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/templates/protocol.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/guides/environment.md`、`docs/agent-testing/flows/auction-lifecycle.md`、`docs/agent-testing/modules/ws.md`、`docs/agent-testing/reports/README.md`。

## 测试环境

- 服务地址：本机双实例，`127.0.0.1:18080` 作为 producer，`127.0.0.1:18081` 作为 subscriber。
- 配置来源：临时配置目录 `/private/tmp/live-auction-agent-multipod-local`，基于 `config.yaml.example`，仅修改 HTTP 端口并关闭 OTLP 导出。
- MySQL：本地 Docker MySQL，地址和凭据不写入报告。
- Redis：本地 Docker Redis，地址和凭据不写入报告。
- Apifox：本次未执行接口规范对齐。
- WebSocket：真实 WebSocket 连接，两个客户端连接到 subscriber 实例。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地真实依赖 | 验证用户、房间、拍品、出价、订单最终状态 |
| Redis | 本地真实依赖 | 验证 ticket、在线状态、auction state、Redis event bus |
| WebSocket | 本地真实连接 | 验证跨实例 room fanout 和 user unicast |
| 外部服务 | 未使用 | 本次只做本地测试 |

## 测试数据

- 测试批次 ID：`agent_multipod_20260607015515_`
- 创建数据：3 个测试账号、1 个直播间、1 个拍品、1 条价格封顶出价、1 个订单。
- 复用数据：本地 MySQL/Redis 容器。

## 执行步骤

1. 复用本地健康 Docker 容器：MySQL、Redis。
2. 启动两个本地后端实例：`18080` 和 `18081`，共用同一 MySQL/Redis。
3. 对两个实例执行 `/livez` 和 `/readyz`。
4. 创建批次账号：merchant、bidder、observer。
5. 将 merchant 更新为商家身份，创建并开播直播间。
6. 创建、发布拍品，规则为起拍价 1000、加价 100、封顶价 1200、保证金 0。
7. 在 `18081` 为 observer 和 bidder 获取 WS ticket，并连接同一房间。
8. 在 `18080` 开始拍品竞拍，验证 `18081` observer 收到 `auction_started`。
9. 在 `18080` 用 bidder 出价 1200，验证 `18081` observer 收到 `bid_success` 和 `auction_ended`。
10. 验证 `18081` bidder 收到用户单播 `order_created`。
11. 通过 `18081` HTTP 查询拍品最终状态。
12. 清理本批次 MySQL 数据和 Redis key，关闭两个本地服务。

## 验证证据

- 本地健康检查：
  - `18080 /livez` 返回 200，`status=ok`。
  - `18080 /readyz` 返回 200，MySQL/Redis 组件均为 `ok`。
  - `18081 /livez` 返回 200，`status=ok`。
  - `18081 /readyz` 返回 200，MySQL/Redis 组件均为 `ok`。
- 本地 runner 输出：
  - `pod_a_ready PASS`
  - `pod_b_ready PASS`
  - `register_batch_users PASS`
  - `create_room_on_a PASS`
  - `start_room_on_a PASS`
  - `create_item_on_a PASS`
  - `publish_item_on_a PASS`
  - `connect_ws_clients_on_b PASS`
  - `start_item_on_a PASS`
  - `pod_b_observer_receives_started_from_a PASS`，events: `auction_started`
  - `place_price_cap_bid_on_a PASS`
  - `pod_b_observer_receives_bid_and_end_from_a PASS`，events: `bid_success`, `auction_ended`
  - `pod_b_bidder_receives_unicast_order_from_a PASS`，events included `order_created`
  - `http_state_recovered_on_b PASS`，status `ended`，deal_price `1200`，leader_user_id 为本批次 bidder。
  - `SUMMARY PASS batch=agent_multipod_20260607015515_ producer=18080 subscriber=18081`
- 清理证据：
  - `CLEANUP batch=agent_multipod_20260607015515_ mysql_deleted=8 redis_deleted=6 user_ids=3`
  - `lsof` 检查 `18080`、`18081` 无监听进程。
- 本地单元回归：
  - `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/... ./internal/core/cronlease ./internal/core/redislease ./internal/app/base/... ./internal/app/item/... ./internal/app/order/... -count=1`
  - 结果：通过，覆盖 `internal/app/ws/bus`、`internal/app/ws/handler`、`internal/app/ws/hub`、`internal/core/cronlease`、`internal/core/redislease`、`internal/app/base/handler`、`internal/app/item/service`、`internal/app/order/service` 等相关包。

## 通过项

- 两个本地后端实例均能通过 readiness，且 readiness 同时检查 MySQL 和 Redis。
- Redis ticket + WebSocket 握手在 subscriber 实例成功。
- producer 实例触发的 `auction_started` 通过 Redis event bus 到达 subscriber 实例本地 WS 客户端。
- producer 实例触发的 `bid_success`、`auction_ended` 通过 Redis event bus 到达 subscriber 实例本地 WS 客户端。
- producer 实例触发的 winner 单播 `order_created` 到达 subscriber 实例上的 bidder 连接。
- 断线/刷新后的最终事实来源可由 subscriber 实例 HTTP 查询恢复，拍品状态为 `ended`，成交价为 `1200`，领先用户为本批次 bidder。
- 本批次测试数据已清理，本地测试后端端口已释放。
- 相关单元回归全部通过。

## 失败项

无。

## 跳过项

- 线上 k3s 两副本验证：未执行。原因是用户明确要求改为本地测试；本报告只覆盖本地双实例、线上等价依赖路径。
- 完整 auction-lifecycle 中的保证金支付、订单支付、默认罚没分支：未执行。原因是本次目标聚焦多实例 event bus 和 readiness，runner 使用保证金 0 的价格封顶竞拍最短路径触发 `auction_started`、`bid_success`、`auction_ended`、`order_created`。
- 并发一致性和性能压测：未执行。`auction-lifecycle.md` 明确基础流程不执行并发专项。
- Apifox 对齐：未执行。本次为本地功能验证，不作为接口契约测试结论。

## Apifox 对齐偏差

未执行接口契约对齐。

## 风险和建议

- 本地双实例能证明同一 Redis/MySQL 下的跨实例 WS 事件路径有效，但不能替代 k3s Service/Ingress/镜像/探针在真实集群中的验证。
- cron lease 相关代码已由 `internal/core/cronlease` 单元测试覆盖；本次本地 flow 未构造“两个实例同时争抢同一 due job 且只有一个写入”的专项断言。建议后续补一个本地真实依赖 cron lease runner。

## 建议沉淀的回归测试

- 将临时 runner 的核心场景沉淀为可复用的 agent-testing Go runner：双实例启动、跨实例 room fanout、跨实例 user unicast、HTTP 最终状态恢复、批次清理。
- 为 cron lease 增加本地真实 Redis 集成测试，验证同一 job name 同一 tick 只有一个实例进入业务函数。

## 已知缺口

- 未覆盖线上 k3s 两副本 rollout。
- 未覆盖 Ingress/WebSocket 代理层。
- 未覆盖完整保证金和支付生命周期。

## 测试数据清理结果

- 测试批次 ID：`agent_multipod_20260607015515_`
- 创建的数据：3 个用户、1 个房间、1 个拍品、1 个出价、1 个订单、相关 Redis key。
- 清理方式：MySQL 仅按本批次账号/标题前缀删除；Redis 仅按本批次 room_id、item_id 和临时 ticket key 删除。
- 清理结果：`mysql_deleted=8`、`redis_deleted=6`、`user_ids=3`。
- 未清理原因：无。
