# 测试报告：local-authority-failover

## 基本信息

- 测试目标：验证 `live-auction-backend` 在线下环境中的本地 authority failover / switchback 行为，包括控制面切换、WebSocket ticket 存储切换、以及同一拍品在 `cloud -> local -> cloud` 过程中的连续出价。
- 测试类型：regression
- 测试时间：2026-06-09 17:31:00 +0800 至 2026-06-09 21:14:18 +0800
- 执行 agent：Codex（主 agent）
- 主 agent：Codex
- 子 agent：未使用
- 子 agent 结果摘要：未使用
- 主 agent 复核结论：未使用
- 冲突和处理：无
- Subagent cleanup：未使用
- 并行数据隔离证明：不适用
- 读取文档：
  - `docs/agent-testing/README.md`
  - `docs/agent-testing/templates/protocol.md`
  - `docs/agent-testing/guides/environment.md`
  - `docs/agent-testing/guides/failover.md`
  - `docs/agent-testing/reports/README.md`

## 测试环境

- 服务地址：`http://127.0.0.1:18080`
- 配置来源：`tmp/failover.local.yaml`
- MySQL：本地 Docker MySQL 容器；使用隔离数据库 `live_auction_failover_local`
- Redis：本地主 Redis `127.0.0.1:6379` + 本地备用 Redis `127.0.0.1:6380`
- Apifox：未使用
- WebSocket：验证了 `/api/v1/ws-ticket` 的 authority 切换落点；未执行浏览器侧持续连接演练

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 本地 Docker MySQL + 隔离数据库 | 需要真实持久化验证 `bid_logs`、拍品状态和切换后行为 |
| Redis | 本地主 Redis + 本地备用 Redis | 需要真实验证 authority 切换与 ticket/key 落点 |
| WebSocket | 使用真实 `/api/v1/ws-ticket` 接口验证 ticket authority | 验证切换时 ticket 是否落到正确 Redis |
| 外部服务 | 未使用 | 本次仅验证本地依赖与后端行为 |

## 测试数据

- 测试批次 ID：`agent_failover_1780999343296`
- 创建数据：
  - bidder 账号：`agent_failover_1780999343296`
  - 多组 merchant / room / item 数据，统一使用 `agent_failover_merchant_*` 前缀
  - 临时状态文件：`tmp/failover-state.json`
  - 临时配置：`tmp/failover.local.yaml`
  - 临时 compose 覆盖：`tmp/failover-compose.yml`
- 复用数据：
  - 本地 Docker MySQL 容器 `live-auction-mysql`
  - 本地主 Redis 容器 `live-auction-redis`
  - 本地备用 Redis 容器 `live-auction-redis-backup`

## 执行步骤

1. 准备本地 failover 配置，使用隔离数据库 `live_auction_failover_local` 和双 Redis。
2. 启动本地 MySQL、主 Redis、备用 Redis。
3. 写入控制面状态文件 `tmp/failover-state.json`，启动后端。
4. 先验证 `/livez`、`/health`、`/readyz` 的控制面状态响应。
5. 注册隔离 bidder 账号，验证 `/api/v1/ws-ticket` 在 `cloud`、`local`、再回 `cloud` 三个阶段的 Redis 落点。
6. 注册 merchant，创建 room / item，执行 `publish` / `start`。
7. 在 `normal_cloud` 下执行第一笔 bid。
8. 将控制面切换到 `local_redis_active`，在同一拍品上执行第二笔 bid。
9. 将控制面切回 `normal_cloud`，在同一拍品上执行第三笔 bid。
10. 对中途失败场景进行实现修复，并使用全新拍品重新验证 `cloud -> local -> cloud` 三段出价。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 代码级回归通过 | `rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...`、`rtk env GOCACHE=/tmp/live-auction-go-cache go build ./...`、多轮 focused suites 全通过 | 通过 |
| 控制面可在运行时刷新 | 更新 `tmp/failover-state.json` 后，等待一个 watcher 周期，`/health` 从 `degraded(control_plane stale)` 恢复为 `ok` | 通过 |
| `ws-ticket` 云主模式落到主 Redis | phase1：`/health` 返回 `active_redis=cloud`；对 `ws:ticket:1:<ticket>` 在主 Redis `EXISTS=1`，备用 Redis `EXISTS=0` | 通过 |
| `ws-ticket` failover 后落到备用 Redis | phase2：`/health` 返回 `active_redis=local`；对 `ws:ticket:2:<ticket>` 在主 Redis `EXISTS=0`，备用 Redis `EXISTS=1` | 通过 |
| `ws-ticket` switchback 后回到主 Redis | phase3：`/health` 返回 `active_redis=cloud`；对 `ws:ticket:3:<ticket>` 在主 Redis `EXISTS=1`，备用 Redis `EXISTS=0` | 通过 |
| 云主模式第一笔 bid | fresh 全链路脚本输出：`cloudBid.http=200`，`cloudBid.code=0`，`price=1100` | 通过 |
| 本地 authority 模式第二笔 bid | fresh 全链路脚本输出：`localBid.http=200`，`localBid.code=0`，`price=1200` | 通过 |
| business switchback 第三笔 bid | fresh 全链路脚本输出：`switchbackHealth.activeRedis=cloud`，`switchbackBid.http=200`，`switchbackBid.bid.code=0`，`price=1300` | 通过 |

## 性能核心指标

不适用

## 通过项

- 控制面状态文件变化能被运行中的服务刷新并体现在 `/health`。
- `/livez` 保持可用，不因 Redis authority 变化直接退出。
- `/api/v1/ws-ticket` 在 `cloud -> local -> cloud` 三段中都能写到正确 Redis。
- 在同一拍品上，`cloud` 第一笔、`local` 第二笔、`cloud switchback` 第三笔出价全部成功。
- `switchback` 时业务路径能够先把 cloud 侧热状态对齐到当前 authority，再继续接受新 bid。
- 旧 `bid_logs` 数据上的唯一索引迁移不会再直接把本地服务启动卡死。

## 失败项

无

## 跳过项

- 未执行浏览器侧持续 WebSocket 连接与 reconnect 消息序列验证。
  - 原因：本次重点是 authority 切换与业务出价链路。
  - 风险：仍建议补一轮“切换中已有连接 + 新连接 + reconnect snapshot”的线下演练。
  - 后续补测条件：在同一套本地双 Redis 环境中加入真实 WebSocket client。

## Apifox 对齐偏差

无

## 风险和建议

- 当前线下验证依赖外部控制面写入方持续刷新 `updated_at_unix_ms`。若未来真实控制面 writer 心跳中断，服务会再次进入 `control_plane stale` 保护状态。
- `switchback` 的业务闭环现在依赖 item 级 cloud state reconcile；建议后续把这条路径沉淀成更明确的 item 级恢复流程与指标。
- `item.BroadcastTimeSync` 在 authority 切换窗口内仍可能打印 `active redis unavailable` 警告，建议后续单独整理其容错语义与观测指标。

## 建议沉淀的回归测试

- `cloud -> local -> cloud` 同一拍品三段 bid 的自动化集成 runner
- `ws-ticket` 在三段 authority 中的 Redis key 落点检查
- 控制面 state 文件刷新后的 `/health` 变化检查
- 旧 `bid_logs` 迁移数据上的唯一索引安全回归

## 已知缺口

- 本报告未覆盖 MySQL 运行中断后的 10 秒 buffering window、backlog drain 与 settlement 恢复。
- 本报告未覆盖真实 WebSocket 连接在切换中的持续连接、reconnect snapshot 与 presence rebuilding 表现。

## 测试数据清理结果

- 测试批次 ID：`agent_failover_1780999343296`
- 创建的数据：
  - bidder 账号 `agent_failover_1780999343296`
  - 多个 `agent_failover_merchant_*` 用户、room、item、order
  - `tmp/failover-state.json`
  - `tmp/failover.local.yaml`
  - `tmp/failover-compose.yml`
- 清理方式：
  - 已停止本次线下后端进程。
  - 保留隔离数据库 `live_auction_failover_local` 与 `agent_failover_*` 前缀数据，用于后续复跑和追证。
- 清理结果：
  - 服务进程已停止。
  - 业务数据未删除。
- 未清理原因：
  - 本次使用独立本地测试库，保留数据有利于后续继续验证 MySQL buffering、WebSocket reconnect 和 switchback 追证。
