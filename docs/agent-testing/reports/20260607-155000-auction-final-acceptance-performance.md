# 测试报告：auction final acceptance performance

## 基本信息

- 测试目标：验收当前后端在 3 副本、多实例 Redis event bus、Redis 热路径 / 缓存预热、WebSocket control / market 分层后的 service-path 承载表现。
- 测试类型：性能压测，`staging_capacity` 风格 service-path guarded probes。
- 测试时间：2026-06-07 15:50-16:50 Asia/Shanghai。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。执行时保持单一发压者，主 agent 串行承担 preflight、load、monitor、recorder、cleanup。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：计划允许 subagent，但本轮为了避免多执行器共享真实依赖和批次数据，未启用 subagent；作为执行编排偏离项记录。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`AGENTS.md`、`skills/agent-testing-gate/SKILL.md`、`skills/live-auction-online-ops/SKILL.md`、`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`types.md`、`online.md`、`runner.md`、`guides/environment.md`、`guides/subagent.md`、`guides/performance/subagent.md`、`flows/auction-lifecycle.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`modules/deposit.md`、本批次 `performance-plan.md`、`reports/README.md`。

## 测试环境

- 服务地址：线上 k3s service path，完整地址已省略。
- 配置来源：已部署 k3s backend，敏感配置未读取或写入报告。
- MySQL：线上 MySQL，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- Apifox：不适用，本轮不是接口契约对齐测试。
- WebSocket：service-path WebSocket，`control_market` 分流。
- Backend：3 个 ready replicas，测试前后 restart 均为 0。
- 压测源：线上服务器临时 runner 二进制，从 service path 访问 backend 与 Prometheus；远端临时二进制和日志已清理。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | service path | 避免 public HTTPS 本机网络噪声污染后端容量判断 |
| WebSocket | service path + control/market 双流 | 验证 WS 分层、fanout 和 time_sync 控制面 |
| MySQL | 线上真实依赖，限本批次数据 | 验证真实出价、排行榜、BidLog 最终一致 |
| Redis | 线上真实依赖，限本批次 key | 验证 hot state、ranking、WS ticket、bid log stream 和 online state |
| Prometheus/kubectl/logs | 只读查询 | 采集服务端、依赖和资源证据 |
| 外部服务 | 未调用 | 本轮只测竞拍核心链路 |

## 测试数据

- 测试批次 ID：父批次 `agent_perf_auction_final_acceptance_20260607`。
- 子批次：
  - `agent_perf_auction_final_acceptance_20260607_smoke`
  - `agent_perf_auction_final_acceptance_20260607_qps300_ws3000`
  - `agent_perf_auction_final_acceptance_20260607_qps500_ws3000`
  - `agent_perf_auction_final_acceptance_20260607_qps700_ws3000`
  - `agent_perf_auction_final_acceptance_20260607_qps300_ws6000`
- 创建数据：每个子批次各自创建测试商家、用户、房间、拍品、WebSocket ticket、出价和 Redis 竞拍状态。
- 复用数据：线上服务、MySQL、Redis、Prometheus；不复用真实业务用户或商品。

## 执行步骤

1. 读取最终压测计划和 agent-testing 性能路线。
2. 线上只读 preflight：backend `3/3` ready、Pod restart、resource baseline、service readiness、Prometheus readiness、strict log marker baseline。
3. 复制既有 service-path runner 到本批次目录，交叉编译 Linux amd64 二进制。
4. 上传临时 runner 到线上服务器 `/tmp`。
5. 执行 smoke：50 QPS / 200 physical WS。
6. 执行 QPS ramp probes：300、500、700 target QPS，固定 3000 physical WS。
7. 执行 WS scaling probe：300 target QPS / 6000 physical WS。
8. 每档采集 runner、Prometheus、kubectl top、restart 和业务对账证据。
9. 每档 runner 完成 cleanup，最后确认远端无 runner 残留、资源回落、restart 和 strict log marker。
10. 删除远端 `/tmp` 临时 runner 和本次远端日志副本。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 3 副本 readiness | preflight 显示 backend `3/3 ready`，三个 Pod `ready=true` | 通过 |
| service readiness | service-path `/readyz` HTTP 200，MySQL/Redis component ok | 通过 |
| Prometheus | readiness endpoint 返回 ready | 通过 |
| runner 编译 | `go test ./docs/agent-testing/performance-runs/agent_perf_auction_final_acceptance_20260607 -count=1` 通过 | 通过 |
| smoke | 49.99 QPS、200 physical WS、0 failure、业务对账通过 | 通过 |
| qps300_ws3000 | 280.80 actual QPS、3000 physical WS、HTTP failure 4、业务失败 0、对账通过 | 健康但未达 95% target QPS |
| qps500_ws3000 | 447.30 actual QPS、3000 physical WS、HTTP failure 0、业务失败 0、对账通过 | 健康但未达 95% target QPS |
| qps700_ws3000 | 602.71 actual QPS、3000 physical WS、HTTP failure 9、业务失败 0、对账通过 | 健康但未达 95% target QPS |
| qps300_ws6000 | 250.63 actual QPS、6000 physical WS、WS connect failure 0、业务失败 0、对账通过 | WS 扩容探针通过，单源接近瓶颈 |
| backend restart | 全阶段 Prometheus / kubectl restart max 0 | 通过 |
| strict log marker | 测试窗口 fatal/panic/OOMKilled/killed marker count 0 | 通过 |
| cleanup | 各子批次关闭 WS、取消 item、下播 room、尝试删除用户；远端临时 runner/log 删除 | 通过 |

## 每档压测结果

| 阶段 | Target QPS | Actual QPS | Physical WS | HTTP failures | Timeouts | Business failures | Expected rejects | Client P99 | Server P99 max | WS connect P99 | WS write P95 max | time_sync write lag P95 max | Backend restarts | 结论 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| smoke | 50 | 49.99 | 200 | 0 | 0 | 0 | 0 | 3.137ms | 98.690ms | 28.249ms | 0.957ms | 1.805ms | 0 | 通过 |
| qps300_ws3000 | 300 | 280.80 | 3000 | 4 | 4 | 0 | 4485 | 39.492ms | 99.467ms | 25.131ms | 0.978ms | 23.382ms | 0 | 健康，吞吐未达标 |
| qps500_ws3000 | 500 | 447.30 | 3000 | 0 | 0 | 0 | 10755 | 43.094ms | 99.467ms | 20.496ms | 29.946ms | 43.750ms | 0 | 健康，吞吐未达标 |
| qps700_ws3000 | 700 | 602.71 | 3000 | 9 | 9 | 0 | 18614 | 47.027ms | 99.467ms | 21.991ms | 9.987ms | 28.515ms | 0 | 健康，吞吐未达标 |
| qps300_ws6000 | 300 | 250.63 | 6000 | 2 | 2 | 0 | 11122 | 105.694ms | 99.467ms | 21.646ms | 4.359ms | 46.558ms | 0 | WS 扩容通过，单源接近瓶颈 |

## 资源观测

- Preflight baseline：backend 单 pod约 `5-6m CPU / 15-17Mi`，MySQL约 `10m / 869Mi`，Redis约 `13m / 389Mi`，节点约 `5% CPU / 67% memory`。
- qps300_ws3000 阶段中 backend 约 `388-400m CPU / 67-81Mi`。
- qps500_ws3000 阶段中 backend 约 `414-435m CPU / 88-89Mi`。
- qps700_ws3000 阶段中 backend 约 `467-492m CPU / 88-93Mi`。
- qps300_ws6000 后资源回落：backend 约 `6-7m CPU / 76-86Mi`，MySQL约 `9m / 869Mi`，Redis约 `11m / 424Mi`。
- 压测源 host sample：
  - qps500_ws3000：CPU `67.22%`，rx 约 `30.84 MB/s`，tx 约 `7.41 MB/s`。
  - qps700_ws3000：CPU `75.80%`，rx 约 `33.27 MB/s`，tx 约 `8.56 MB/s`。
  - qps300_ws6000：CPU `82.14%`，rx 约 `49.92 MB/s`，tx 约 `9.32 MB/s`。

## 通过项

- 3 副本 backend readiness 正常，测试全程 backend restart 为 0。
- service-path smoke 通过，runner、认证、数据准备、WS 分层、业务对账和 cleanup 可运行。
- 在 3000 physical WS 下，实际最高观察到约 `602.71 RPS`，server HTTP P99 max 约 `99.467ms`，业务失败率为 0。
- 在 6000 physical WS 下，WS connect failure 为 0，WS connect P99 约 `21.646ms`，业务对账通过。
- WS control / market 分层可承载大量事件，control time_sync arrival P99 在 6000 physical WS 下约 `136ms`。
- WS write P95 在所有阶段低于 50ms 阈值；send queue depth P95 未持续增长。
- MySQL 和 Redis 资源没有打满迹象。
- 所有子批次均执行 cleanup，远端临时 runner 与日志已删除。

## 失败项

无业务失败项。

容量门槛未通过项：

- qps300/qps500/qps700 阶段均没有达到 `actual_qps >= 95% target_qps` 的 claimed-capacity 阈值。
- qps300_ws6000 阶段 load source CPU 达到 `82.14%`，接近计划中的 `85%` load-source hold/stop 线，因此停止继续 9000 WS。

这不是后端业务失败；当前证据更支持单源 runner / 压测源能力先接近瓶颈。

## 跳过项

- 9000 physical WS：跳过。原因是 6000 physical WS 下压测源 CPU 已到 `82.14%`，继续上探大概率测到单源 runner 限制，而不是 backend 上限。
- 900/1100 target QPS：跳过。原因是 700 target QPS 下 actual 仅 `602.71`，且压测源 CPU 已升至 `75.80%`，继续上探不适合写 claimed capacity。
- peak hold：跳过。原因是没有出现满足 `>=95% target QPS` 的高档 claimed-capacity 组合。
- controlled cold rebuild probe：跳过。原因是本轮优先完成主容量和 WS 扩容验收，未执行 Redis key 故障注入。
- public HTTPS/WSS path check：跳过。原因是本轮目标收敛到 service-path 后端验收；public path 之前已有独立报告显示本机公网路径会先引入 WS dial 尾延迟。
- 直接 MySQL / Redis 内部逐条对账：跳过。runner 已做 HTTP/Redis 间接业务对账和 Prometheus 指标采集；未在报告中写入线上 DSN 或 Redis 地址。

## Apifox 对齐偏差

不适用。本轮是性能压测，不是接口契约测试。

## 风险和建议

- 当前后端 service-side 表现健康，但本轮不能给出“1100 QPS / 9000 WS”正式容量通过结论，因为单源 runner 未能打满目标 QPS，且 6000 WS 时压测源 CPU 已接近停止线。
- 建议下一轮用 k8s Job 或多压测源并行发压，每个 source 使用不同子批次或不同 item，主 agent 汇总服务端 Prometheus 指标。
- 建议给 runner 增加明确的 per-source self metrics 和连接/请求调度延迟，避免用 host sample 粗略判断压测源瓶颈。
- 建议补 controlled cold rebuild probe，专门验证 ranking rebuild lock / cooldown 在 3 副本下不会放大 MySQL 查询。
- 建议把 setup 阶段注册用户的单 pod 倾斜单独诊断；正式阶段负载分布是均衡的，但 setup 会短时把单 pod 推近 1 core。

## 建议沉淀的回归测试

- `50 QPS / 200 physical WS` service-path smoke，作为每次上线前轻量验收。
- `500 target QPS / 3000 physical WS` service-path guarded probe，关注 server P99、WS write、backend restart、业务对账。
- `300 target QPS / 6000 physical WS` WS scaling probe，关注 WS connect、control time_sync arrival、ws delivery、load-source CPU。
- Redis cold ranking rebuild 单独 probe，验证跨 pod rebuild 合并。

## 已知缺口

- 未证明 9000 physical WS。
- 未证明 1100 QPS。
- 未执行 public path 用户体验复测。
- 未执行 peak hold / soak。
- 未执行冷缓存 / ranking rebuild 故障注入。
- 用户删除仍是 runner 的 `attempted` 级证据，不逐条输出删除成功数。

## 测试数据清理结果

线上依赖使用情况：已使用，地址和凭据已省略。

测试数据范围：仅本次父批次和子批次前缀数据。

| 子批次 | 清理结果 |
| --- | --- |
| `agent_perf_auction_final_acceptance_20260607_smoke` | `closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=141` |
| `agent_perf_auction_final_acceptance_20260607_qps300_ws3000` | `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601` |
| `agent_perf_auction_final_acceptance_20260607_qps500_ws3000` | `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601` |
| `agent_perf_auction_final_acceptance_20260607_qps700_ws3000` | `closed_ws=3000 cancel_item=ok end_room=ok delete_users_attempted=1601` |
| `agent_perf_auction_final_acceptance_20260607_qps300_ws6000` | `closed_ws=6000 cancel_item=ok end_room=ok delete_users_attempted=3101` |

远端临时文件清理：

- `/tmp/agent_perf_auction_final_acceptance_20260607_runner` 已删除。
- 本轮远端 `/tmp/agent_perf_auction_final_acceptance_20260607_*.log` 已删除。

未清理原因：

- runner 只输出 delete users attempted，不逐条证明用户删除成功；未发现 cleanup 报错。
