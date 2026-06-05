# 测试报告：ws connect path diagnosis

## 基本信息

- 测试目标：定位线上 WebSocket connect P95/P99 约 `5-6s` 的来源，区分本地压测源/公网路径、public ingress/TLS/Traefik、k3s service path、backend accept/register path。
- 测试类型：single_source_online 分层诊断；不是容量上限测试。
- 测试时间：2026-06-06 Asia/Shanghai。
- 执行 agent：主 agent Codex；子 agent Volta 实现 probe，Godel 做规格审查，Fermat/Turing 做代码质量审查和复审。
- 读取文档：`AGENTS.md`、`skills/live-auction-online-ops/SKILL.md`、`skills/agent-testing-gate/SKILL.md`、`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/*.md`、`guides/environment.md`、`guides/subagent.md`、`reports/README.md`。
- 线上地址脱敏说明：报告只写 public ingress、online host、service port-forward 等摘要，不写完整线上地址、凭据、token、DSN 或完整 WS query string。
- 子 agent 结果摘要：probe 实现通过规格审查；代码质量审查发现 hostname 脱敏和 duration 校验问题，已修复并复审通过。
- 主 agent 复核结论：子 agent 结论已复核，线上诊断命令由主 agent 执行，所有结论基于脱敏 evidence。
- Subagent cleanup：所有已使用子 agent 均已关闭。

## 结果矩阵

| Path | Source | Concurrency | Target WS | Ticket P95 | Ticket P99 | Dial P95 | Dial P99 | First Msg P95 | First Msg P99 | Cleanup |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Public WSS | local minimal probe | 8 | 80 | `1.849s` | `1.910s` | `5.527s` | `5.767s` | `21.198ms` | `50.681ms` | OK |
| Public WSS | local full runner | 8 | 160 | n/a | n/a | `5.477s` | `6.091s` | n/a | n/a | OK |
| Public WSS | online host | 8 | 80 | `248.595ms` | `308.545ms` | `443.214ms` | `475.875ms` | `1.609ms` | `26.328ms` | OK |
| Service port-forward | online host | 8 | 80 | `10.744ms` | `12.326ms` | `11.230ms` | `13.714ms` | `2.857ms` | `3.405ms` | OK |
| Public WSS sweep | local | 1 | 80 | `264.376ms` | `339.483ms` | `5.745s` | `5.819s` | `242µs` | `16.631ms` | OK |
| Public WSS sweep | local | 4 | 80 | `329.247ms` | `342.826ms` | `4.950s` | `5.764s` | `312µs` | `1.198ms` | OK |
| Public WSS sweep | local | 8 | 80 | `298.954ms` | `324.025ms` | `5.749s` | `5.892s` | `1.150ms` | `11.664ms` | OK |
| Public WSS sweep | local | 16 | 80 | `364.116ms` | `424.859ms` | `5.818s` | `5.969s` | `9.337ms` | `48.323ms` | OK |
| Public WSS sweep | local | 32 | 80 | `396.304ms` | `724.096ms` | `5.857s` | `6.023s` | `1.202ms` | `26.570ms` | OK |

## 分层判断

分类：`client_or_public_network_likely`。

证据链：

- local minimal probe 和 full runner 都复现 public WSS connect P95/P99 `5-6s`，所以 full runner 调度不是主因。
- 同样 public WSS 从 online host 发起时，Dial P95/P99 下降到约 `443ms/476ms`。
- online host 通过 service port-forward 绕过 public ingress 后，Dial P95/P99 约 `11ms/14ms`，backend accept/register 和 k3s service path 都很快。
- local public sweep 在 concurrency `1` 就有 Dial P95/P99 约 `5.745s/5.819s`，并发升到 `32` 后仍是同一量级；这不像 backend 或 ingress 的 burst threshold。

## Prometheus / lifecycle 证据

- baseline `ws_connection_active`：`0`。
- sweep 后 `ws_connection_active`：`0`。
- `ws_connection_lifecycle_total` 查询可用，accepted 总量 `2014`，总 lifecycle 计数 `4028`，与 accepted/closed 成对量级一致。
- backend 严格错误标记数：baseline `0`，sweep 后 `0`。

## 资源和日志复核

- baseline backend 约 `7m CPU / 52Mi`，sweep 后约 `8m CPU / 48Mi`。
- MySQL、Redis、OTel Collector、Prometheus 资源无异常尖峰迹象。
- backend 和 Traefik 相关 pod 保持 Running，backend restart count `0`。
- 所有 probe connect errors `{}`，first-message timeout `0`。

## 结论

本轮不支持 `service_or_backend_likely`，也不支持“full runner 测量实现是主因”。最可信结论是：当前本地 runner 到 public ingress 的公网/客户端网络路径引入了稳定的 `5-6s` WS dial tail；线上 host 到同一 public endpoint 已降到亚秒级，service-local 路径是毫秒级。

## 下一步建议

- 后续评估真实用户体验时，应区分压测源网络位置；本地单机 public runner 不适合作为 backend connect 性能结论。
- 若需要继续缩小 public ingress/TLS 影响，可从另一台独立地域机器或云上压测源复跑 public WSS c1/c8。
- backend 侧无需立即进入多实例验证；当前证据没有显示单 pod accept/register 是瓶颈。

## 清理结果

- 所有 minimal probe 批次均报告 closed WS、cancel item、end room、delete users、delete merchant OK。
- full runner comparison 报告 closed WS `160`、cancel item OK、end room OK、delete users attempted `101`。
- service path 的远端临时 port-forward PID 已停止，日志尾部仅显示正常连接处理。
- 远端临时二进制不包含凭据，诊断结束后已从 `/tmp` 删除；未写入报告敏感内容。
