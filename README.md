# live-auction-backend

`live-auction-backend` 是一个面向直播电商场景的实时拍卖后端服务，适用于珠宝、奢侈品等高价值非标品的在线竞拍。

## 目录结构

```text
cmd/                 应用命令入口，包含 server 和 config 子命令
config/              配置结构、默认值、加载和热更新
deploy/              部署文件，包含 k3s、观测组件和历史 Compose 配置
docs/                产品、设计、测试和性能报告文档
internal/app/        业务模块
internal/core/       内核对象、数据库、缓存、可用性和观测能力
internal/cron/       定时任务封装
internal/middleware/ HTTP 中间件、鉴权、响应和 Web 工具
pkg/                 可复用基础包，例如错误码、日志、ID 生成
skills/              项目本地 agent 技能说明
main.go              程序入口
docker-compose.yml   本地依赖和观测栈
config.yaml.example  本地配置示例
```

业务模块位于 `internal/app/<module>/`，常见结构如下：

```text
model/    GORM 模型和常量
dao/      Store 接口和持久化实现
dto/      请求、响应和服务入参结构
service/  业务逻辑，依赖 dao.Store 接口
handler/  Flamego HTTP 处理函数
router/   路由注册
init.go   模块生命周期加载
```

当前主要模块：

```text
base     健康检查、基础能力、图片上传签名
user     用户注册、登录、鉴权、资料管理
room     直播间管理和房间状态
item     拍卖商品、拍卖规则、出价和排行榜
deposit  保证金支付和查询
order    订单查询
payment  订单支付和取消
ws       WebSocket ticket 和房间连接
```

## 依赖环境

本地开发建议准备：

- Go `1.25.0` 或与 `go.mod` 兼容的版本。
- Docker 和 Docker Compose，用于启动 MySQL、Redis 和本地观测组件。
- MySQL `8.4`，本地 Compose 默认映射到 `127.0.0.1:3306`。
- Redis `7`，本地 Compose 默认映射到 `127.0.0.1:6379`。

主要 Go 依赖：

- Flamego：HTTP 框架和依赖注入。
- GORM：MySQL ORM。
- go-redis：Redis 客户端。
- Cobra/Viper：命令行和配置加载。
- OpenTelemetry：指标和链路追踪。
- Gorilla WebSocket：WebSocket 连接。

## 启动步骤

### 1. 准备配置文件

项目根目录已经提供 `config.yaml.example`。本地运行可复制一份为 `config.yaml`：

```bash
cp config.yaml.example config.yaml
```

也可以通过内置命令生成默认配置：

```bash
go run main.go config -p config.yaml
```

如果本地 MySQL、Redis 地址、账号或端口不同，请先修改 `config.yaml`。

### 2. 启动本地依赖

启动 MySQL、Redis、OpenTelemetry Collector、Prometheus、Tempo、Loki、Promtail 和 Grafana：

```bash
docker compose up -d
```

如果只需要运行后端，至少需要保证 MySQL 和 Redis 可访问。

### 3. 启动后端服务

```bash
go run main.go server -c config.yaml
```

默认服务地址：

```text
http://127.0.0.1:8080
```

常用检查地址：

```text
GET /health
GET /readyz
GET /livez
GET /api/v1/health
```

本地观测地址：

```text
Grafana:    http://127.0.0.1:3000
Prometheus: http://127.0.0.1:9090
Loki:       http://127.0.0.1:3100
Tempo:      http://127.0.0.1:3200
OTLP gRPC:  127.0.0.1:4317
OTLP HTTP:  127.0.0.1:4318
```

### 4. 运行测试和构建

```bash
go test ./...
go build ./...
```

运行单个模块测试示例：

```bash
go test ./internal/app/item/service/...
```

### 5. 停止本地依赖

```bash
docker compose down
```

如果需要同时删除本地 MySQL/Redis 数据卷：

```bash
docker compose down -v
```

## 配置说明

配置文件默认路径为项目根目录的 `config.yaml`，启动时通过 `-c` 指定：

```bash
go run main.go server -c config.yaml
```

关键配置项：

```yaml
mode: debug
app:
  name: live-auction-backend
  version: 0.1.0
http:
  host: 127.0.0.1
  port: "8080"
database:
  driver: mysql
  dsn: live_auction:live_auction@tcp(127.0.0.1:3306)/live_auction?charset=utf8mb4&parseTime=True&loc=Local
redis:
  addr: 127.0.0.1:6379
auth:
  token_secret: live-auction-development-secret
  token_ttl: 24h
auction:
  extend_trigger_sec: 30
  auto_extend_sec: 10
  max_extend_count: 6
  max_total_extend_sec: 300
```

配置分组说明：

- `mode`：运行模式，常用 `debug` 或 `release`。
- `app`：应用名称和版本。
- `http`：HTTP 监听地址和端口。
- `database`：MySQL 驱动、DSN、连接池参数。
- `redis`：主 Redis 连接信息。
- `availability`：Redis 探活、本地 Redis 兜底和 MySQL 缓冲窗口配置。
- `auth`：登录 token 签名密钥和有效期。
- `security.allowed_origins`：HTTP/WebSocket 跨域来源白名单，开发环境可使用 `*`。
- `auction`：防狙击延时策略。
- `storage.tos`：火山引擎 TOS 图片上传签名配置，默认关闭。
- `observability`：OpenTelemetry、日志格式、指标采集间隔等观测配置。

配置由 Viper 加载，服务运行期间会监听配置文件变更并热更新配置对象。涉及数据库、Redis 连接等启动期资源的配置变更，通常仍建议重启服务后生效。

## 业务特性

- 用户注册、登录、资料管理和身份切换。
- 商家创建直播间、管理拍卖商品、发布/开始/取消拍卖。
- 用户支付保证金后参与出价。
- 出价排行榜、当前价、拍卖状态通过 Redis 保持实时响应，并异步补偿落库。
- 订单、支付、保证金状态具备幂等检查和补偿机制。
- 支持 WebSocket 房间连接和消息推送。
- 支持健康检查、OpenTelemetry 指标/链路、Prometheus、Tempo、Loki、Grafana 本地观测栈。

一致性重点：

- 订单状态包括 `pending`、`success`、`expired`、`cancel`。
- 支付状态包括 `success`。
- 保证金状态包括 `pending`、`paid`、`refunded`、`forfeited`。
- 出价链路需要协调 Redis 排行榜和 MySQL 出价记录。
- 商品上架链路需要维护直播间当前拍品。
- 订单取消、支付失败、订单过期时，需要补偿保证金。

## 常用 API 入口

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
GET  /api/v1/users/me
PUT  /api/v1/users/me

POST /api/v1/merchant/room
GET  /api/v1/merchant/room
GET  /api/v1/rooms
GET  /api/v1/rooms/{room_id}

GET  /api/v1/items
POST /api/v1/items
GET  /api/v1/items/{item_id}
POST /api/v1/items/{item_id}/publish
POST /api/v1/items/{item_id}/start
POST /api/v1/items/{item_id}/bids
GET  /api/v1/items/{item_id}/ranking

POST /api/v1/items/{item_id}/deposit/pay
GET  /api/v1/items/{item_id}/deposit

GET  /api/v1/orders
GET  /api/v1/orders/{order_id}
POST /api/v1/orders/{order_id}/pay
POST /api/v1/orders/{order_id}/cancel

POST /api/v1/ws-ticket
GET  /ws/v1/rooms/{room_id}
```

需要登录的接口通过请求头传入 token：

```text
Authorization: Bearer <token>
```
