# 测试报告：ws logging regression

## 基本信息

- 测试目标：WebSocket ticket 日志脱敏回归
- 测试类型：regression / module / integration
- 测试时间：2026-05-25 18:10:37-18:14:00 Asia/Shanghai
- 执行 agent：Codex
- 读取文档：
  - `docs/agent-testing/modules/ws.md`
  - `docs/agent-testing/reports/README.md`
  - `docs/agent-testing/reports/20260525-175921-ws-module.md`

## 测试环境

- 服务地址：本地服务 `http://127.0.0.1:8080`，WebSocket `ws://127.0.0.1:8080`
- 配置来源：`config.yaml`
- MySQL：本地测试数据库，地址和凭据已省略
- Redis：本地测试 Redis，地址和凭据已省略
- WebSocket：使用真实 gorilla websocket 客户端连接

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地测试库；通过注册/注销接口创建和清理本批次用户 | 验证真实鉴权链路 |
| Redis | 本地测试 Redis；只读写本批次 ticket 和 room key | 验证 ticket 与在线状态 |
| WebSocket | 真实连接 | 验证真实 handshake 和日志路径 |
| 外部服务 | 不使用 | 不属于本次回归 |

## 测试数据

- 测试批次 ID：`agent_ws_20260525181037`
- 创建数据：
  - 测试用户账号前缀：`agent_ws_20260525181037_`
  - 测试房间 ID：`room_agent_ws_20260525181037`、`room_agent_ws_20260525181037_other`
  - Redis key：`ws:ticket:<masked>`、`auction:room:room_agent_ws_20260525181037*:state`、`auction:room:room_agent_ws_20260525181037*:online_users`
- 复用数据：本地 MySQL / Redis 服务

## 执行步骤

1. 增加 `internal/middleware/gw/http_test.go`，先验证缺少 `sanitizeRequestURI` 时测试失败。
2. 修改 `internal/middleware/gw/http.go`，访问日志使用脱敏后的 request URI。
3. 执行 `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/middleware/gw ./internal/app/ws/... ./pkg/wsevent/...`。
4. 执行 `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...`。
5. 启动本地服务并执行 WebSocket runner。
6. 检查服务 stdout 访问日志中 WebSocket query 参数是否脱敏。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| RED | `go test ./internal/middleware/gw` 使用 `/tmp/live-auction-go-cache` 失败，原因是 `sanitizeRequestURI` 未定义 | PASS |
| 中间件单测 | `ok github.com/zet-plane/live-auction-backend/internal/middleware/gw` | PASS |
| ws 相关测试 | `internal/app/ws/hub`、`pkg/wsevent` 通过 | PASS |
| 全仓测试 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...` 通过 | PASS |
| WebSocket runner | 6 个场景全部 PASS，SUMMARY `PASS: 6 FAIL: 0` | PASS |
| 日志脱敏 | 服务 stdout 中 WebSocket 请求记录为 `GET /ws/v1/rooms/<batch-room>?ticket=REDACTED ...`，未出现完整 ticket | PASS |
| 清理 | Runner cleanup：两个测试用户 DELETE 均 `200`；Redis batch keys 删除 `3`，`err=<nil>` | PASS |

## 通过项

- WebSocket ticket query 在访问日志中已脱敏为 `REDACTED`。
- ticket 签发、握手、ping/pong、重复 ticket 拒绝、并发一次性消费、在线状态同步仍全部通过。
- 本地单元测试和全仓测试通过。

## 失败项

- 无。

## 跳过项

- 未重新执行完整拍品 / 出价业务事件链路；本次只验证 ws 模块和日志脱敏回归。
- 未同步 Apifox；Apifox 写入需要单独批准。

## Apifox 对齐偏差

- 沿用上一份报告结论：当前 Apifox 缺少 `POST /api/v1/ws-ticket`，且 `/ws/v1/rooms/{room_id}` query 参数仍是 `token`，应改为 `ticket`。

## 风险和建议

- 继续同步 Apifox，使接口文档和当前实现一致。
- 后续可在 HTTP 日志中间件增加更多敏感 query key 的覆盖，如业务后续出现 `secret`、`signature` 等参数。

## 建议沉淀的回归测试

- `sanitizeRequestURI` 应脱敏 `ticket`、`token`、`access_token`、`refresh_token`、`jwt`。
- 真实 WebSocket 握手后，服务日志不得出现完整 ticket。

## 已知缺口

- 当前回归通过人工读取本地服务 stdout 片段验证日志脱敏；后续可以为日志中间件加可注入 logger，使端到端日志断言自动化。

## 测试数据清理结果

- 测试批次 ID：`agent_ws_20260525181037`
- 创建的数据：2 个测试用户、多个短期 ticket、2 个测试 room 相关 Redis key。
- 清理方式：
  - 通过 `DELETE /api/v1/users/me` 清理两个测试用户。
  - 通过 Redis `DEL` 清理本批次 room state / online users / ticket key。
- 清理结果：
  - `DELETE userA by API -> status=200`
  - `DELETE userB by API -> status=200`
  - `DEL redis batch keys -> deleted=3 err=<nil>`
  - 无未清理项。
