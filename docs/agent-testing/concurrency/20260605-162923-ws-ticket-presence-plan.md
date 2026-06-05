# WebSocket 并发一致性测试计划

## 基本信息

- 计划来源：用户要求在 `codex/millisecond-auction-sync` 分支上按 `docs/agent-testing` 对 WebSocket 修改先做模块测试，再做并发一致性测试。
- 计划时间：2026-06-05 16:29:23 Asia/Shanghai
- 计划状态：已执行
- 批准方式：用户在对话中回复 `gogogo！` 批准执行
- 执行结果：通过；runner summary 为 `PASS: 8 FAIL: 0`

## 涉及模块

- 目标模块：`ws`
- 关联模块：`user`、`room`
- 关联 flow：无

## 测试目标

验证 WebSocket 模块在真实 HTTP、WebSocket、Redis 和数据库依赖下的 P0 并发一致性：

- 同一 WebSocket ticket 被并发使用时，只能有 1 条连接升级成功。
- 多用户并发进出同一房间时，Redis 在线用户集合和 `online_count` 最终收敛一致。

## 读取文档

- `docs/agent-testing/README.md`
- `docs/agent-testing/templates/protocol.md`
- `docs/agent-testing/guides/runner.md`
- `docs/agent-testing/guides/concurrency.md`
- `docs/agent-testing/guides/go-runner.md`
- `docs/agent-testing/guides/environment.md`
- `docs/agent-testing/modules/ws.md`
- `docs/agent-testing/reports/README.md`

## 测试范围

- `POST /api/v1/ws-ticket`
- `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`
- WebSocket `ping` / `pong`
- Redis `ws:ticket:{ticket}`
- Redis `auction:room:{room_id}:online_users`
- Redis `auction:room:{room_id}:state.online_count`

## 禁止范围

- 不做生产容量、QPS、长时间稳定性或性能压测。
- 不清空 Redis 或数据库。
- 不操作非本批次数据。
- 不在报告中写入完整 token、完整 ticket、完整 WebSocket query string、数据库 DSN、Redis 凭据或线上地址。
- 不把 WebSocket 消息作为最终业务事实；最终状态以 Redis / HTTP / 数据库证据交叉验证。

## 测试数据

- 测试批次 ID：`agent_ws_concurrency_20260605162923`
- 账号前缀：`agent_ws_concurrency_20260605162923_`
- 房间名前缀：`agent_ws_concurrency_20260605162923_`
- Redis key 范围：
  - 本批次生成的 `ws:ticket:{ticket}`
  - 本批次房间的 `auction:room:{room_id}:online_users`
  - 本批次房间的 `auction:room:{room_id}:state`

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 本地或用户批准的线上等价后端服务 | 验证真实 handler、鉴权和序列化边界 |
| WebSocket | 真实 WebSocket 客户端 | 验证真实 upgrade、消息收发和断开行为 |
| Redis | 真实测试 Redis 或线上等价 Redis | 验证 `GETDEL`、在线集合和 `online_count` 收敛 |
| MySQL | 真实测试库或线上等价数据库 | 创建/清理本批次用户和房间，验证数据边界 |
| 第三方服务 | 不使用 | 不属于 WebSocket 并发状态 |

## 并发场景设计

### 场景 1：同一 ticket 并发握手

- 场景名称：`same_ticket_concurrent_handshake`
- 竞争对象：同一个 Redis key `ws:ticket:{ticket}` 和同一个 WebSocket ticket 的一次性消费权。
- 并发请求：12 个 WebSocket 客户端使用同一个 ticket 同步起跑连接同一个本批次 room。
- 预期成功：最多 1 个请求返回 101 并能发送 `ping` 收到 `pong`。
- 预期失败：其余请求返回 401，响应文本为 `invalid or expired ticket`。
- 最终不变量：
  - Redis `ws:ticket:{ticket}` 不存在。
  - 成功连接数量为 1。
  - 本批次房间 `online_users` 至多包含成功连接的用户 ID。
  - `online_count` 等于 `SCARD online_users`。
  - 每个请求有开始时间、结束时间、状态码或 WebSocket 结果证据，实际重叠窗口大于 0。

### 场景 2：多用户并发连接和离开同一房间

- 场景名称：`multi_user_concurrent_presence`
- 竞争对象：同一房间 Redis `auction:room:{room_id}:online_users` 集合和 `auction:room:{room_id}:state.online_count` 字段。
- 并发请求：8 个不同测试用户分别使用独立 ticket，同步起跑连接同一个本批次 room；全部连接成功后，再同步起跑发送 `leave_room` 或关闭连接。
- 预期成功：8 个用户连接均成功，连接期间均可 `ping` 收到 `pong`。
- 预期失败：无业务失败；若出现网络或握手失败，必须作为失败项记录。
- 最终不变量：
  - 连接稳定期 Redis `SCARD online_users = 8`。
  - 连接稳定期 Redis `state.online_count = 8`。
  - 离开后 Redis `SCARD online_users = 0`。
  - 离开后 Redis `state.online_count = 0` 或不存在但可解释为本批次房间状态已清理。
  - 每个连接/离开请求有开始时间、结束时间、状态码或 WebSocket 结果证据，实际重叠窗口大于 0。

## 执行步骤

1. 确认全仓 `go test ./...` 通过。
2. 确认 WebSocket 模块本地单元测试通过。
3. 按 `guides/environment.md` 确认服务、数据库和 Redis 可用。
4. 使用 Go runner 创建本批次测试用户、测试房间和 WebSocket ticket。
5. 执行 `same_ticket_concurrent_handshake`。
6. 执行 `multi_user_concurrent_presence`。
7. 查询 Redis、HTTP 或数据库证据并完成最终状态对账。
8. 清理本批次创建的数据和 Redis key。
9. 按 `reports/README.md` 写入测试报告。

## 验证方式

- Go runner 输出每个并发请求的 `CASE`。
- Go runner 输出每个场景的 summary `CASE`。
- Redis 证据包含 `EXISTS ws:ticket:{ticket}`、`SCARD online_users`、`SMEMBERS online_users`、`HGET state online_count`。
- 清理证据包含关闭 WebSocket 连接、本批次 Redis key 删除、本批次用户/房间清理结果。

## 预计输出

- 测试报告：`docs/agent-testing/reports/<timestamp>-ws-concurrency.md`
- Runner stdout 摘要，脱敏记录 token、ticket 和 WebSocket query。
