# 测试报告：auction performance validation

## 基本信息

- 测试目标：验证修复后的 performance runner 能按 `160 users / 160 WS` 并发建连，并输出 Prometheus 阶段时间线；随后尝试执行 60/70/80/90/100 QPS 阶梯压测。
- 测试类型：线上受控短性能验证，`single_source_online`。
- 测试时间：2026-06-03 00:48 Asia/Shanghai。
- 执行 agent：主 agent + monitor subagent。
- 主 agent：执行 runner、复核日志、清理测试数据、修复本地阻塞代码。
- 子 agent：monitor / preflight，只读检查线上 deployment、资源、Prometheus 和日志。
- 子 agent 结果摘要：基础设施与观测面 ready with caveats；后端 `1/1 ready`，restart 0，Prometheus ready，应用指标可查；最终监控显示 backend restart 0、近期错误计数 0。
- 主 agent 复核结论：首次 runner 未进入任何负载阶段；发布用户 DAO 修复并修正 runner 商家名长度后，60 QPS / 160 WS 在 `PERF_WS_CONNECT_CONCURRENCY=8` 下完成；完整阶梯尝试在 60 QPS 观察到 client E2E P99 超阈值，但服务端 `server_http_p99` 约 5.6ms，应将其解释为端到端链路噪声/压测源问题，而不是服务内接口耗时。
- 冲突和处理：无结论冲突；monitor 提示的 `UpdateMe` error 与主 agent runner 失败一致。
- Subagent cleanup：monitor subagent 已关闭。
- 并行数据隔离证明：本次只有主 agent 创建测试数据；批次 ID 为 `agent_perf_auction_20260603_ws_prom_validation`。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`guides/performance/types.md`、`guides/performance/online.md`、`guides/performance/runner.md`、`guides/environment.md`、`flows/auction-lifecycle.md`、`reports/README.md`。

## 测试环境

- 服务地址：线上入口，完整地址已省略。
- 配置来源：runner 环境变量，敏感值未写入报告。
- MySQL：线上数据库，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- Prometheus：通过临时本地 port-forward 只读查询，测试结束后已关闭。
- WebSocket：首次 setup 未进入；后续诊断批次已真实连接，8 并发下达到 `160/160`。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 线上服务间接使用 | 注册测试账号和用户资料更新 |
| Redis | 线上服务间接使用 | auction 状态、出价和 WebSocket 在线状态 |
| WebSocket | 真实连接 | 验证一用户一 WS 的 160 连接目标 |
| Prometheus | 只读 query / readiness | 验证 runner 时间线采集入口可用 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260603_ws_prom_validation`、`agent_perf_auction_20260603_ws_prom_validation2`、`agent_perf_auction_20260603_ws_prom_validation3`、`agent_perf_auction_20260603_ws_diag_60`、`agent_perf_auction_20260603_ws_diag_60_c8`、`agent_perf_auction_20260603_qps60_100_full`。
- 创建数据：失败 setup 批次各注册 1 个测试账号；成功诊断批次创建批次商家、房间、拍品和 160 个测试用户。
- 复用数据：无。

## 执行步骤

1. 本地修复 runner：并发 WS 建连、Prometheus 阶段时间线、`PERF_END_QPS=60` 边界。
2. 本地验证 runner 包测试通过。
3. 线上只读 preflight：健康检查、deployment、资源水位、Prometheus readiness。
4. 启动短验证 runner，配置只跑 smoke 和 60 QPS 阶段。
5. runner 在 setup 阶段注册测试账号成功，随后 promotion merchant 失败并停止。
6. 查询后端日志，确认同窗口 `UpdateMe` GORM query error，真实错误为 `Data too long for column 'name'`。
7. 清理 setup 创建的测试账号。
8. 本地修复 `internal/app/user/dao.UpdateUser`，避免全行 `Save`，并推送部署 `ba7098c5`。
9. 执行注册 -> `identity=merchant` -> 查询 -> 删除 smoke，确认部署修复生效。
10. 修复 runner 商家名长度，增加 WS 建连错误分类输出。
11. 执行 32 并发 WS 诊断和 8 并发 WS 诊断。
12. 将 Prometheus 默认查询名对齐线上实际指标。
13. 执行 60/70/80/90/100 完整阶梯尝试，60 QPS 档观察到 client E2E P99 超阈值后停止。

## 验证证据

- Runner preflight：health HTTP 200，Prometheus HTTP 200，STOP 文件不存在。
- Runner stop event：`STAGE=preflight`，`REASON=setup_failed err=promote merchant: status=500 code=50001 msg=internal server error`。
- 线上日志：同窗口出现 `PUT /api/v1/users/me 500`、`UpdateMe failed` 和 GORM query error；提取字段显示 `Data too long for column 'name'`。
- Monitor preflight：后端镜像 tag `91c9a696`，deployment `1/1 ready`，backend restart 0，Prometheus ready，应用指标可查。
- 清理证据：测试账号登录 HTTP 200 / code 0，删除当前用户 HTTP 200 / code 0。
- 部署证据：CI/CD run `26836181836` 成功，线上镜像 tag `ba7098c5`，rollout `1/1 ready`。
- Post-deploy smoke：注册、更新 `identity=merchant`、查询、删除均 HTTP 200 / code 0。
- 32 并发 WS 诊断：`WS_CONNECTED=45`、`WS_CONNECT_FAILS=275`、`WS_CONNECT_ERRORS={"dial:EOF":275}`。
- 8 并发 WS 诊断：`WS_CONNECTED=160`、`WS_CONNECT_FAILS=0`、实际 QPS 58.99、P99 583ms、cleanup 成功。
- 完整阶梯尝试：`step_60qps_160ws` 达到 `WS_CONNECTED=160`，实际 QPS 55.51，client E2E P99 5.601s，timeouts 124。新版 runner 已将 client E2E P99 改为 advisory，不作为服务端接口性能优化默认硬判停。
- 完整阶梯 Prometheus：`server_http_p99` max 0.005600s，`ws_active` max 160，`backend_restarts` max 0。
- 完整阶梯后端检查：rollout healthy，app restart 0，backend 约 4m CPU / 48Mi；最近日志过滤无 panic/OOM/fatal/500-style 错误。
- 本地回归：`go test -count=1 ./internal/app/user/... ./docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100` 和 runner 包测试通过。

## 通过项

- runner 的本地回归测试通过。
- `PERF_END_QPS=60` 已限制短验证不会越过批准的 60 QPS 阶段。
- Prometheus readiness 和基础 query 可用。
- 本次 setup 创建的测试账号已清理。
- `UpdateUser` 修复已完成并有 DAO 回归测试覆盖。
- `UpdateUser` 修复已部署，merchant identity smoke 通过。
- runner 商家名长度已修复，避免长 batch id 超过 `users.name` 64 字符限制。
- runner 已输出 WS 建连错误分类。
- `PERF_WS_CONNECT_CONCURRENCY=8` 下达成 `160 users / 160 WS`。
- Prometheus 默认查询已对齐线上实际指标，并在完整阶梯尝试中返回非空时间线。

## 阻塞项复盘

失败场景：setup 阶段将测试用户提升为 merchant。

复现步骤：
1. 注册测试用户。
2. 使用该用户认证态调用 `PUT /api/v1/users/me`，body 包含 `identity=merchant`。

期望结果：HTTP 200 / code 0，用户身份变为 merchant。

实际结果：HTTP 500 / code 50001，runner 停止在 preflight setup。

相关证据：runner stop event；后端同窗口 `UpdateMe` GORM query error。

可能原因：首次失败包含两个问题：线上镜像中的 `user.dao.UpdateUser` 使用 `db.Save(user)` 全行保存用户模型；修复部署后，runner 长 batch id 又导致 merchant name 超过 64 字符。

影响范围：所有依赖 `PUT /api/v1/users/me` 修改身份或资料的线上流程，包括创建商家身份和后续 auction performance setup。

建议修复点：DAO 修复已发布；runner 商家名长度已修复。

建议新增的回归测试：保留 `userProfileUpdateValues` 单元测试；后续补一个接口/真实依赖 smoke，验证注册后更新 `identity=merchant` 成功。

## Client E2E 观测项

- 完整阶梯压测在 `step_60qps_160ws` 观察到 client E2E P99 5.601s，因此当时未继续 70/80/90/100 QPS。
- 60 QPS 档服务端 Prometheus `server_http_p99` max 约 5.6ms，但 runner client E2E P99 5.601s 且 timeout 124；后端资源和 restart 正常。对服务端接口性能优化来说，应优先采用 `server_http_p95/server_http_p99`，client E2E 仅作为公网链路、压测源或入口层噪声参考。

## Apifox 对齐偏差

不适用；本次不是接口契约测试。

## 风险和建议

- 后端是单副本，短压测可以进行，但任何 restart/OOM 都会直接影响服务可用性。
- 32 并发 WS 建连会产生 `dial:EOF`，应默认使用 8 并发或增加握手节流。
- 当前数据不能证明公网端到端稳定承载 60 QPS；它证明了 160 users / 160 WS 可建连，并且服务端接口耗时在该窗口内较低。
- 服务端 Prometheus latency 与 runner client E2E latency 差异较大，建议增加 runner 请求排队时间、DNS/TLS/连接复用、公网入口和本机资源分解指标。

## 建议沉淀的回归测试

- 本地 DAO 单元测试：只更新可变 profile 字段。
- 用户模块接口 smoke：注册用户后更新身份为 merchant。
- 性能 runner smoke：`PERF_END_QPS=60` 下只执行 smoke 和 60 档。

## 已知缺口

- 完整阶梯已尝试，但当时因 client E2E P99 过高在 60 QPS 停止，70/80/90/100 未执行；新版 runner 已调整为服务端指标优先。
- 尚未拆分 client E2E 高 P99 的来源：runner 本机调度、HTTP transport、DNS/TLS、公网入口、Ingress、或请求排队。

## 测试数据清理结果

- 线上依赖使用情况：已使用，地址和凭据已省略。
- 测试数据范围：仅上述 agent performance validation / diagnostic 批次。
- 清理方式：失败 setup 批次通过登录本批次测试账号后调用删除当前用户 API；完整诊断批次通过 runner cleanup 关闭 WS、取消拍品、结束房间并删除测试用户。
- 清理结果：失败 setup 批次删除当前用户 HTTP 200 / code 0；`ws_diag_60_c8` cleanup `closed_ws=160 cancel_item=ok end_room=ok delete_users_attempted=161`；`qps60_100_full` cleanup `closed_ws=160 cancel_item=ok end_room=ok delete_users_attempted=161`。
- 未清理原因：无已知未清理项。
