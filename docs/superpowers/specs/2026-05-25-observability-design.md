# Observability Design

## 背景

live-auction-backend 是实时竞拍后端，核心风险集中在高频出价、Redis 实时状态、MySQL 持久化、订单补偿和定时落槌。第一期监控目标是建立准生产可用的可观测性底座，支持本地完整验证和后续线上演进。

本设计选择 OpenTelemetry 方案，并在本地 docker-compose 中提供 Prometheus、Tempo、Loki、Grafana。第一期不做告警规则和生产部署脚本，但采集的数据要足够支撑后续告警。

## 目标

- 通过 OpenTelemetry 采集后端 traces 和 metrics。
- 通过结构化 JSON 日志接入 Loki，并和 trace 关联。
- 覆盖 HTTP、MySQL、Redis、cron 和竞拍核心链路。
- 提供本地可运行的 Prometheus、Tempo、Loki、Grafana 观测栈。
- Grafana 中能查看系统总览、竞拍业务指标、trace 和日志。

## 非目标

- 不在第一期实现告警通知。
- 不提供生产环境部署脚本。
- 不把日志字段全部做成 Loki label。
- 不在普通单元测试中连接 MySQL、Redis、Prometheus、Tempo、Loki 或 Grafana。

## 总体架构

```text
live-auction-backend
  ├─ OpenTelemetry traces/metrics -> otel-collector
  └─ JSON stdout logs -> promtail

otel-collector
  ├─ metrics -> Prometheus
  └─ traces  -> Tempo

promtail
  └─ logs -> Loki

Grafana
  ├─ Prometheus datasource
  ├─ Tempo datasource
  └─ Loki datasource
```

后端只依赖 OpenTelemetry SDK 和 OTLP exporter，不直接依赖 Prometheus、Tempo、Loki 或 Grafana。这样以后替换采集后端时，不需要改业务埋点。

## 代码结构

新增核心包：

```text
internal/core/observability/
  provider.go      // OTel resource、tracer provider、meter provider、exporter 初始化
  http.go          // Flamego HTTP middleware
  metrics.go       // 系统和业务指标定义
  trace.go         // 业务 span helper
  log.go           // zap 日志 trace_id/span_id 注入
  cron.go          // cron job wrapper
```

新增本地观测栈配置：

```text
deploy/observability/
  otel-collector.yaml
  prometheus.yaml
  tempo.yaml
  loki.yaml
  promtail.yaml
  grafana/
    datasources/
      prometheus.yaml
      tempo.yaml
      loki.yaml
    dashboards/
      live-auction-overview.json
      live-auction-bidding.json
      live-auction-logs.json
```

## 配置

`config.yaml.example` 增加：

```yaml
observability:
  enabled: true
  service_name: live-auction-backend
  service_version: 0.1.0
  environment: local
  otlp_endpoint: 127.0.0.1:4317
  otlp_insecure: true
  trace_sample_ratio: 1.0
  metrics_interval: 15s
  logs:
    format: json
    output: stdout
    include_trace_context: true
```

初始化策略：

- `enabled=false` 时使用 noop provider，不导出遥测数据。
- local/debug 环境默认采样率为 `1.0`。
- OTel 初始化失败时服务降级启动，并记录 error 日志；监控系统不可用不应阻断业务服务启动。
- 关闭服务时调用 observability shutdown，确保 exporter flush。

## HTTP 埋点

在 `cmd/server/server.go` 的 middleware 链中加入 OTel HTTP middleware，并保留或升级现有 `gw.RequestLog()`。

每个请求创建 server span：

```text
HTTP POST /api/v1/items/{item_id}/bids
  attributes:
    http.method
    http.route
    http.status_code
    user.id
    error.code
```

指标：

```text
http.server.request.count
http.server.request.duration
http.server.error.count
```

HTTP 指标必须使用 route 模板作为 label，例如 `/api/v1/items/{item_id}/bids`，不能使用带真实 ID 的 URL。

## MySQL 埋点

在现有 `internal/middleware/gormv2/logger.go` 基础上增加 OTel trace 和 metrics，或新增 GORM plugin。

指标：

```text
db.client.operation.duration
db.client.error.count
db.client.slow_query.count
```

span 属性：

```text
db.system=mysql
db.operation=SELECT|INSERT|UPDATE|DELETE
db.sql.table=auction_items|bid_logs|orders|...
```

SQL 原文只允许在 debug/local 环境进入日志，避免生产泄露敏感数据和产生高基数属性。

## Redis 埋点

在 `internal/core/cache/cache.go` 初始化 Redis client 时注册 hook；竞拍关键 Lua 逻辑在 `internal/app/item/cache/bid.go` 中单独记录业务 span。

通用指标：

```text
redis.command.duration
redis.command.error.count
```

竞拍 Lua 指标：

```text
auction.place_bid.lua.duration
auction.place_bid.lua.result.count{code="0|1|2|3|4|error"}
```

Lua code 语义：

```text
0     成功出价
1     幂等命中
2     竞拍已结束或状态不存在
3     出价低于或等于当前价
4     加价幅度不合法
error Redis 执行错误
```

## Cron 埋点

将直接注册 cron 函数改为 wrapper 形式：

```go
engine.Cron.AddFunc("@every 1m", observability.WrapCron("item.end_expired_auctions", svc.EndExpiredAuctions))
```

覆盖任务：

- `item.end_expired_auctions`
- `order.scan_expired_orders`
- `order.scan_compensation`

指标：

```text
cron.job.run.count
cron.job.duration
cron.job.error.count
cron.job.last_success_timestamp
```

cron span 需要记录 job 名称、执行结果、错误信息和耗时。

## 竞拍核心链路

第一期优先覆盖：

- `item.start`
- `auction.place_bid`
- `auction.get_ranking`
- `auction.end_expired`
- `order.create_from_auction`
- `order.compensation_scan`

`PlaceBid` trace 结构：

```text
POST /api/v1/items/{item_id}/bids
  └─ auction.place_bid
      ├─ item.find_with_rule
      ├─ deposit.check
      ├─ redis.place_bid_lua
      ├─ mysql.bid_log.create
      ├─ mysql.item.end_if_price_cap
      └─ order.create_from_auction
```

业务指标：

```text
auction.bid.count{result="success|idempotent|rejected|error", reason="..."}
auction.bid.amount
auction.bid.duration
auction.active_items
auction.auto_extend.count
auction.price_cap_end.count
auction.redis_mysql_inconsistency.count
order.auction_create.count{result="success|error|compensated"}
```

`reason` 只允许使用有限枚举值，不能把原始错误文本作为 label。

## 日志与 Loki

日志输出改为可配置 JSON 格式。每条请求内日志自动带：

```text
trace_id
span_id
request_id
service_name
environment
level
module
```

业务日志字段：

```text
user_id
room_id
item_id
bid_id
order_id
error_code
```

Loki label 只使用低基数字段：

```text
service_name
environment
level
module
```

以下字段只作为 JSON 内容，不作为 Loki label：

```text
user_id
room_id
item_id
bid_id
order_id
request_id
error_message
```

Promtail 负责采集后端 stdout 或日志文件，解析 JSON 后写入 Loki。

## Grafana

`live-auction-overview` 面板：

- HTTP QPS
- HTTP P95/P99
- HTTP 4xx/5xx
- MySQL query duration
- MySQL slow query count
- Redis command duration
- Redis error count
- Cron job duration/status

`live-auction-bidding` 面板：

- PlaceBid QPS
- PlaceBid success/reject/error rate
- Reject reason 分布
- Redis Lua result code 分布
- 出价金额分布
- 自动延时次数
- 一口价成交次数
- 订单创建成功/失败/补偿次数
- Redis/MySQL 不一致计数

`live-auction-logs` 面板：

- 按 level/module 过滤日志
- 按 trace_id 查询日志
- 按 item_id、bid_id、order_id 做 JSON 字段查询

Grafana datasource 需要支持：

- metric panel 跳转到 trace 查询。
- Tempo span 通过 `trace_id` 跳转到 Loki 日志。
- Loki log line 通过 `trace_id` 跳转到 Tempo trace。

## 本地运行

扩展 `docker-compose.yml`：

```text
mysql
redis
otel-collector
prometheus
tempo
loki
promtail
grafana
```

端口：

```text
otel-collector: 4317, 4318, 8889
prometheus:     9090
tempo:          3200
loki:           3100
grafana:        3000
```

本地验证流程：

```bash
rtk docker compose up -d mysql redis otel-collector prometheus tempo loki promtail grafana
rtk go run main.go server -c config.yaml
```

通过接口造数据：

- 注册/登录用户和商家。
- 创建房间。
- 创建拍品。
- 开播/开拍。
- 支付押金。
- 多次出价。
- 查询排行榜。
- 触发一口价成交或过期落槌。

验收标准：

- Grafana 能看到 HTTP、MySQL、Redis、cron 指标。
- Prometheus 能查到 `auction.*`、`http.*`、`db.*`、`redis.*` 指标。
- Tempo 能看到 `auction.place_bid` trace。
- Loki 能按 `trace_id` 查到同一请求的日志。
- `PlaceBidLua`、`BidLog` 写入、订单创建能挂在同一条 trace 下。
- 日志包含 `trace_id`、`span_id`、`item_id`、`user_id`、`bid_id`。

## 测试策略

普通单元测试不连接外部系统。

测试重点：

- observability 初始化支持 noop provider。
- HTTP middleware 能记录请求状态和耗时。
- HTTP 指标使用 route 模板，不使用真实资源 ID。
- cron wrapper 能记录成功、失败和耗时。
- 业务 recorder 能把有限枚举写入指标。
- `PlaceBidLua code=0` 记录成功。
- `PlaceBidLua code=1` 记录幂等。
- `PlaceBidLua code=2/3/4` 记录拒绝原因。
- 日志 trace context 注入在无 active span 时不会 panic。

本地观测栈验证作为手动集成验证，不进入默认 `go test ./...`。

## 实施顺序

1. 增加配置结构和 `config.yaml.example`。
2. 新增 `internal/core/observability` 初始化和 noop 降级。
3. 接入 HTTP middleware。
4. 接入 GORM 和 Redis 基础埋点。
5. 接入 cron wrapper。
6. 接入竞拍核心业务指标和 span。
7. 调整日志为 JSON 并注入 trace context。
8. 新增 docker-compose 观测服务和 `deploy/observability` 配置。
9. 新增 Grafana datasource 和初始 dashboard。
10. 补充单元测试和本地验证文档。

## 风险与约束

- OpenTelemetry logs 在 Go 生态中成熟度低于 traces/metrics，因此第一期日志通过 zap JSON + Promtail + Loki 完成。
- 高基数字段不能进入 metric label 或 Loki label，否则会拖垮 Prometheus/Loki。
- Flamego 没有官方主流 OTel middleware 时，需要自定义中间件并保证 route 模板可用。
- GORM SQL 原文和错误日志可能包含敏感信息，必须受环境和日志级别控制。
- 监控初始化失败不能阻断业务服务启动。
