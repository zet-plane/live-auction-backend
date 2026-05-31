# Agent 测试环境准备

本文档定义 agent 执行测试前如何准备环境、依赖和测试数据。

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只记录环境准备和阻塞处理的附加规则。

它只描述环境准备，不定义具体业务规则。具体测试边界和通过标准必须来自 `modules/<module>.md` 或 `flows/<flow>.md`。

## 1. 测试类型判定

执行前必须先判定本次测试类型：

| 测试类型 | 是否允许连接数据库 | 依赖策略 |
| --- | --- | --- |
| 本地代码单元测试 | 不允许 | 使用 mock/fake store、进程内数据、fake 时间和固定配置 |
| Agent 接口契约测试 | 允许 | 使用真实 handler 或本地服务，可连接线上数据库或线上等价依赖 |
| Agent 模块集成测试 | 允许 | 使用真实 DAO / Service，可连接线上数据库或线上等价依赖 |
| Agent 全流程测试 | 允许 | 使用真实服务、真实数据库、真实 Redis 和真实 HTTP/WebSocket |
| Agent 并发一致性测试 | 允许 | 使用真实数据库、Redis 和锁机制，不能 mock 并发控制 |
| Agent 性能压测 | 允许 | 使用准生产或线上等价依赖，必须隔离测试数据、记录监控指标和停止条件 |
| Agent 状态一致性测试 | 允许 | 使用真实查询验证 HTTP、MySQL、Redis、WebSocket 和日志 |

如果测试目标不清晰，必须先询问用户，不能自行扩大测试范围。

## 2. 本地单元测试环境

本地代码单元测试用于验证纯业务逻辑。

准备要求：

- 不启动 MySQL。
- 不启动 Redis。
- 不启动 WebSocket。
- 不调用真实 HTTP 服务。
- 不读取线上或本地数据库中的既有数据。
- 使用 mock/fake store 构造数据。
- 使用固定 token secret、fake time、fake ID 或可预测输入。

推荐命令：

```bash
rtk go test ./internal/app/<module>/...
rtk go test ./...
```

如果单元测试因为数据库不可用而失败，应优先判断测试是否错误依赖了数据库，而不是尝试连接数据库。

## 3. Agent 真实依赖测试环境

Agent 执行接口契约、模块集成、全流程、并发一致性、性能压测或状态一致性测试时，可以使用真实依赖。

允许使用：

- 线上数据库。
- 线上 Redis。
- 线上等价的真实数据库或 Redis。
- 本地启动的后端服务。
- 真实 HTTP 请求。
- 真实 WebSocket 连接，如果模块支持。

限制要求：

- 只能创建或修改本次测试数据。
- 测试数据必须带明确前缀或测试批次 ID。
- 清理、数据隔离和敏感信息边界遵守 `templates/protocol.md`。

## 4. 本地服务准备

如果本次测试需要本地服务，先确认配置文件存在。

项目提供配置样例：

```text
config.yaml.example
```

本地默认依赖来自：

```text
docker-compose.yml
```

该 compose 文件包含：

- MySQL 8.4，默认端口 `3306`。
- Redis 7，默认端口 `6379`。

常用命令：

```bash
rtk docker compose up -d
rtk go run main.go server -c config.yaml
```

如果本次任务明确要求连接线上数据库，可以不启动本地 MySQL；如果模块不依赖 Redis，可以不启动 Redis。

### 本地依赖异常处理

执行本地真实依赖测试时，agent 应先区分“依赖不可用”和“沙箱权限受限”：

- 如果服务启动或 runner 连接 `127.0.0.1` 失败并出现 `operation not permitted`、`connect: operation not permitted` 等错误，优先按当前执行环境的权限规则申请在沙箱外重试；不要把它直接记录成业务失败。
- 如果 `docker compose up -d redis` 失败并提示 `6379` 端口已被占用，先检查是否已有可用 Redis 监听在配置地址。端口占用不等于 Redis 不可用；必须用后端启动结果、runner Redis client、或安全的 Redis 探测命令确认。
- 如果 `docker compose ps` 显示某个服务未运行，但端口已被其他容器或本机进程占用，应记录实际证据，并使用 `config.yaml` 中的目标地址验证可用性。
- 本地服务启动成功后，必须用 HTTP 探测目标接口或 runner 首个场景确认服务可访问。
- 如果 agent 为本次测试临时启动了后端服务，测试结束后应停止该进程并确认端口释放，避免影响下一次测试。

## 5. 配置检查

启动服务前检查：

- `config.yaml` 是否存在。
- `http.host` 和 `http.port` 是否为本次测试目标地址。
- `database.dsn` 是否指向本次允许使用的数据库。
- `redis.addr` 是否指向本次允许使用的 Redis。
- `auth.token_secret` 和 `auth.token_ttl` 是否满足 token 测试需要。

安全要求见 `templates/protocol.md`。环境文档只补充本项目记录方式：

- 不在测试报告中输出完整 DSN。
- 不在测试报告中输出 Redis 密码。
- 不在测试报告中输出可复用 token。
- 线上地址只能记录为“线上数据库，地址和凭据已省略”。

## 6. 测试数据命名

Agent 创建测试数据时必须使用可追踪命名。

推荐格式：

```text
测试批次 ID：agent_<target>_<YYYYMMDDHHMMSS>
账号前缀：agent_user_<YYYYMMDDHHMMSS>_
商家前缀：agent_merchant_<YYYYMMDDHHMMSS>_
拍品前缀：agent_item_<YYYYMMDDHHMMSS>_
Redis key 前缀：agent:<target>:<YYYYMMDDHHMMSS>:
```

报告必须记录：

```text
测试批次 ID：
创建的数据：
复用的数据：
清理方式：
清理结果：
未清理原因：
```

## 7. 清理规则

通用清理边界见 `templates/protocol.md`。本项目允许清理：

- 本次测试批次 ID 创建的数据。
- 带明确 agent 测试前缀的数据。
- 本次测试写入的 Redis key。

禁止清理：

- `DROP DATABASE`
- `DROP TABLE`
- `TRUNCATE`
- `FLUSHALL`
- `FLUSHDB`
- 无条件 `DELETE`
- 无测试前缀或无批次 ID 的批量删除

如果无法安全清理，必须在报告中记录未清理原因和建议人工处理方式。

## 8. 就绪检查清单

执行本地单元测试前确认：

```text
- 目标模块明确。
- 测试不连接数据库、Redis 或真实 HTTP 服务。
- mock/fake 数据足以覆盖目标业务规则。
- 命令使用 rtk 前缀。
```

执行 agent 真实依赖测试前确认：

```text
- 已读取 docs/agent-testing/README.md。
- 已读取 docs/agent-testing/guides/environment.md。
- 已读取目标模块或流程测试文档。
- go test ./... 无编译错误，或已记录阻塞原因。
- 目标模块单元测试通过，或已记录阻塞原因。
- 数据库连接已确认可用。
- Redis 连接已确认可用，如果模块依赖 Redis。
- 本地 HTTP 服务已确认可访问；如果由 agent 临时启动，已记录测试结束后的停止方式。
- 测试批次 ID 已生成。
- 测试数据前缀已确定。
- 清理策略已确定。
- 报告不会写入地址、密码或 token。
```

## 9. 阻塞处理

如果环境未就绪，agent 必须停止真实依赖测试并输出：

```text
阻塞项：
影响的测试类型：
已验证的证据：
不能继续的原因：
建议处理方式：
是否需要用户确认：
```

不得通过猜测、跳过关键依赖或扩大测试范围来绕过环境问题。
