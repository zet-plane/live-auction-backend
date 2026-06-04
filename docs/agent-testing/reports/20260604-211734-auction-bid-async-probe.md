# 测试报告：auction-bid-async-mysql-probe

## 基本信息

- 测试目标：验证出价链路改为 MySQL 异步落库后，线上 bid-only 高并发瓶颈是否缓解。
- 测试类型：线上受控单源回归探针，`single_source_online`。
- 测试时间：2026-06-04 21:01:12 +0800 至 22:00:00 +0800。
- runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`。
- 入口：线上公网 Ingress，完整地址已省略。
- 观测：公开 `/health` 可用；公开 `/metrics` 返回 404；随后经用户授权连接线上服务器，通过服务器上的 `kubectl` 只读查询 Prometheus / Pod / MySQL / Redis 资源时间线。

## 授权和范围

- 用户批准线上并发压测。
- 压测模型：`PERF_REQUEST_MIX=bid_only`，`PERF_DISABLE_WS=true`。
- 初始计划档位：150 / 300 / 500 QPS，每档 3 分钟。
- 判停：P99 明显超过 1.5s、错误率或超时率超过 3%、人工停止、健康异常。

## Preflight

- runner 单测：`go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1` 通过。
- 线上 health：HTTP 200，MySQL ok，Redis ok。
- 公开 metrics：`/metrics` 返回 404。
- 本机 Kubernetes preflight：未执行成功，当前本机 `kubectl` 没有可用上下文，默认指向 `localhost:8080`。
- 服务器侧 Kubernetes preflight：backend 镜像 `ghcr.io/zet-plane/live-auction-backend:ce2c5dbe`，backend Pod Running，restart 0。压测前资源：backend 约 7m CPU / 28Mi，MySQL 9m / 669Mi，Redis 10m / 63Mi。Prometheus ready。

## 执行记录

### setup 失败批次

- batch：`agent_perf_auction_20260604_bid_async_probe`。
- 配置：320 用户，request timeout 5s。
- 结果：setup 在注册第 140 个测试用户时超时，未进入发压阶段。
- cleanup-only：拍品取消 ok，房间下播 ok，商家删除 ok，`user_login_ok=141 user_delete_ok=141 user_delete_err=0 user_accounts_scanned=320`。

### 有效压测批次

- batch：`agent_perf_auction_20260604_bid_async_probe_v2`。
- 配置：220 用户，request timeout 15s。
- 数据：创建测试商家、房间、拍品和 220 个用户。

| 阶段 | 实际 QPS | 成功 | 预期业务拒绝 | HTTP failures | Timeout | Error rate | Client P50 | Client P95 | Client P99 | 结论 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 150 QPS / 3m | 149.92 | 18,341 | 8,605 | 40 | 40 | 0.15% | 359.6ms | 1.582s | 2.486s | 延迟不健康，触发人工 STOP |
| 300 QPS / 17s | 290.97 | 2,243 | 2,258 | 299 | 0 | 6.23% | 462.2ms | 1.909s | 2.767s | STOP 生效后的短跑，不继续加压 |

Reconcile：

- item detail ok。
- ranking ok。
- room ok。
- bid attempts：31,786。

Cleanup：

- `closed_ws=0 cancel_item=ok end_room=ok delete_users_attempted=221`。

Post health：

- HTTP 200，MySQL ok，Redis ok。

### 带服务端指标复测批次

- batch：`agent_perf_auction_20260604_bid_async_probe_v3`。
- 配置：220 用户，50 / 150 / 300 QPS，request timeout 15s。

| 阶段 | 实际 QPS | Timeout rate | Client P95 | Client P99 | Server P95 max | Server P99 max | DB ops max | Stream append max | Worker batch max | Backend CPU max | MySQL CPU max | Redis CPU max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 50 QPS / 3m | 46.46 | 0.16% | 2.176s | 3.837s | 97.5ms | 99.5ms | 57.2/s | 40.3/s | 8.4/s | 344m | 80m | 51m |
| 150 QPS / 3m | 142.48 | 0.42% | 2.589s | 3.451s | 6.2ms | 99.8ms | 164.3/s | 81.2/s | 8.8/s | 274m | 121m | 78m |
| 300 QPS / 3m | 299.52 | 0.35% | 1.729s | 3.079s | 5.0ms | 20.9ms | 312.4/s | 168.9/s | 10.0/s | 498m | 200m | 146m |

Cleanup：

- `closed_ws=0 cancel_item=ok end_room=ok delete_users_attempted=221`。

### 500 QPS 对比批次

- batch：`agent_perf_auction_20260604_bid_async_probe_v4`。
- 配置：220 用户，500 QPS，request timeout 15s。

| 阶段 | 实际 QPS | Timeout rate | Client P95 | Client P99 | Server P95 max | Server P99 max | DB ops max | Stream append max | Worker batch max | Backend CPU max | MySQL CPU max | Redis CPU max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 500 QPS / 3m | 453.88 | 1.27% | 2.604s | 3.762s | 97.9ms | 308.8ms | 518.0/s | 173.1/s | 9.1/s | 683m | 279m | 158m |

Cleanup：

- `closed_ws=0 cancel_item=ok end_room=ok delete_users_attempted=221`。

Post check：

- 压测后 health：HTTP 200，MySQL ok，Redis ok。
- 压测后资源：backend 约 7m CPU / 34Mi，MySQL 11m / 671Mi，Redis 10m / 87Mi。
- backend restart：0。

## 结论

- MySQL 同步写 `bid_logs` 造成的 HTTP hot path 瓶颈已经明显缓解。旧同步版本在 300 QPS 时 server P95 max 451.7ms、DB ops max 1078.9/s；异步版本在 300 QPS 时 server P95 max 5.0ms、DB ops max 312.4/s。
- 旧同步版本 500 QPS 时 server P95 max 849.6ms、P99 max 1.479s、DB ops max 1617.8/s；异步版本 500 QPS 时 server P95 max 97.9ms、P99 max 308.8ms、DB ops max 518.0/s。
- 500 QPS 下 backend / MySQL / Redis CPU 都未打满，backend restart 为 0，health 正常。因此当前瓶颈不再是 MySQL bid log 同步落库。
- 客户端公网 E2E P95/P99 仍然很高，且实际 500 QPS 只达到 453.88 QPS，说明压测源、公网链路、Ingress 或客户端并发调度仍有外部侧瓶颈；但服务端 HTTP 指标已经健康。
- 异步链路有 stream append 和 worker batch 指标：500 QPS 下 stream append max 173.1/s，worker batch max 9.1/s。当前指标显示 worker 在消费，但报告没有直接记录 Redis Stream pending / lag，后续可补。

## 建议

- 后续如果要继续找容量上限，建议改用内网入口或更稳定的压测源，避免公网 E2E 噪声掩盖服务端能力。
- 增加 Redis Stream pending / consumer lag 指标，和 `bid_logs` 最终落库对账。
- 500 QPS 下 server P99 已到 308.8ms，但仍低于旧版本 1.479s；下一轮可在服务端指标看护下做 700 QPS guarded probe。
