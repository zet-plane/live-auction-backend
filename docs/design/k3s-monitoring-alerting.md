# K3s 监控与飞书告警设计

## 背景

live-auction-backend 及相关服务部署在 k3s 上。除了应用自身的链路追踪、日志和业务指标，还需要一套面向运行环境的监控与告警能力，用来及时发现：

- 服务或 Pod 异常重启。
- Deployment 出现不可用副本。
- Service 没有可用后端 Endpoint。
- 节点 CPU、内存、磁盘、inode、网络或 IO 压力过高。
- HTTP 健康检查、域名或网关路径不可用。
- 重要告警需要推送到飞书群，而不是只停留在 Grafana 页面里。

本文聚焦 k3s 基础设施监控和告警链路。应用层 OpenTelemetry、Loki、Tempo、本地观测栈等设计见 `docs/superpowers/specs/2026-05-25-observability-design.md`。

## 目标

- Grafana 能查看服务器 CPU、内存、磁盘、网络和 Kubernetes 工作负载详情。
- Prometheus 采集 Kubernetes、节点、Pod、容器和应用指标。
- Alertmanager 负责告警分组、去重、静默和路由。
- 通过 webhook adapter 将告警推送到飞书自定义机器人。
- webhook URL、签名密钥等敏感信息不进入 Git。
- 第一版先建立小而有效的告警规则集，后续根据真实噪音再调优。

## 非目标

- 不替代应用层 OpenTelemetry trace 和 Loki 日志。
- 不直接对所有 Kubernetes Event 告警，只对可行动的症状告警。
- 不在文档或 manifest 中写入线上地址、密码、token、webhook URL。
- 第一版不把 Grafana Alerting 作为主要告警路由；告警路由以 Alertmanager 为准。

## 总体架构

```text
k3s nodes
  +- node-exporter
  |  +- node CPU / memory / disk / network metrics
  +- kubelet + cAdvisor
  |  +- Pod and container CPU / memory / filesystem metrics
  +- kube-state-metrics
  |  +- Deployment / Pod / Service / Endpoint / Job state metrics
  +- application /metrics
     +- live-auction-backend business and HTTP metrics

Prometheus
  +- scrape metrics
  +- evaluate PrometheusRule
  +- send firing alerts

Alertmanager
  +- group alerts
  +- deduplicate alerts
  +- silence alerts
  +- webhook -> Feishu adapter

Feishu adapter
  +- convert Alertmanager payload to Feishu message
  +- calculate timestamp/sign when Feishu signing is enabled
  +- POST to Feishu custom bot webhook

Grafana
  +- dashboards for cluster, node, namespace, Pod, and application views
```

## 推荐组件

建议以 `kube-prometheus-stack` 作为基础监控栈。它通常包含：

- Prometheus
- Alertmanager
- Grafana
- kube-state-metrics
- node-exporter
- Kubernetes 默认 dashboard 和默认告警规则

再额外部署一个飞书 adapter。Alertmanager 的 webhook payload 和飞书自定义机器人的消息格式不同，所以不建议 Alertmanager 直接打飞书 webhook，中间需要一个转换层。

可选但建议补充：

- `blackbox-exporter`：从集群内探测 HTTP/TCP 可用性。
- 外部可用性探测：从集群外探测公网域名、CDN、网关或负载均衡路径。
- Loki/Tempo/OpenTelemetry：用于收到告警后继续下钻日志和 trace。

## 命名空间与职责

建议使用独立命名空间：

```text
monitoring
```

资源归属建议：

```text
monitoring/
  kube-prometheus-stack
  feishu-alertmanager-webhook
  blackbox-exporter
```

职责边界：

- 平台、节点、集群级告警由基础设施负责人维护。
- 应用 SLO、业务指标告警由服务负责人维护。
- 飞书群成员、告警升级路径、值班规则由运维或 on-call 负责人维护。

## Grafana 看板

第一版建议使用这些看板类别：

| 看板 | 用途 |
| --- | --- |
| Node Exporter Full | 服务器 CPU、内存、磁盘、inode、IO、网络详情 |
| Kubernetes Cluster Overview | 集群健康、节点状态、工作负载概览 |
| Kubernetes Compute Resources / Node | 节点维度的 Pod 资源使用和容量 |
| Kubernetes Compute Resources / Namespace | namespace 维度 CPU 和内存使用 |
| Kubernetes Compute Resources / Pod | Pod/container CPU、内存、重启和 limit 使用 |
| live-auction-backend Overview | HTTP、业务指标、Redis/MySQL、出价链路健康 |

服务器侧重点：

- CPU 使用率：user、system、iowait、steal、idle。
- 每核 CPU 使用率。
- load average 与 CPU 核数对比。
- 内存总量、可用内存、已用内存、cache、buffer、swap。
- 磁盘使用率和 inode 使用率。
- 磁盘读写吞吐、IOPS、IO wait。
- 网卡接收/发送流量、丢包、错误包。
- 节点运行时间和重启时间。

Kubernetes 工作负载侧重点：

- namespace、Deployment、Pod、container 维度的 CPU 和内存使用。
- CPU/memory request 与 limit 使用率。
- Pod 重启次数和重启速率。
- Deployment desired/available/unavailable replicas。
- Pending、Failed、Unknown Pod。
- Service Endpoint 可用数量。

## 飞书告警链路

告警链路：

```text
PrometheusRule
  -> Prometheus
  -> Alertmanager
  -> Feishu adapter webhook receiver
  -> Feishu custom bot webhook
  -> Feishu group
```

飞书自定义机器人建议开启安全配置：

- 关键词校验。
- IP 白名单。
- 签名密钥。

生产环境优先使用签名密钥。以下值必须放在 Kubernetes Secret 中：

```text
FEISHU_WEBHOOK_URL
FEISHU_SECRET
```

不要把 webhook URL 或签名密钥提交到 Git。

Alertmanager receiver 指向 adapter：

```yaml
receivers:
  - name: feishu
    webhook_configs:
      - url: http://feishu-alertmanager-webhook.monitoring.svc.cluster.local:8080/alert
        send_resolved: true
```

飞书消息建议包含：

- 告警状态：firing 或 resolved。
- 告警等级：critical、warning、info。
- 告警名称。
- namespace、workload、Pod、container、node。
- summary 和 description。
- 开始时间和恢复时间。
- Grafana dashboard 链接。
- Prometheus 查询链接。
- runbook 链接。

## 告警分组策略

使用 Alertmanager 分组，避免飞书刷屏：

```yaml
route:
  group_by: ["alertname", "namespace", "workload", "node"]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: feishu
```

建议：

- `critical` 告警可 1 到 2 小时重复提醒一次。
- `warning` 告警可 4 到 12 小时重复提醒一次。
- 重启类告警使用 `increase(...[10m])` 或更长窗口，避免重复噪音。
- 资源类告警必须持续一段时间后触发，一般为 5 到 15 分钟。
- critical 告警建议开启恢复通知。

## 首批告警规则

### 节点不可达

```promql
up{job=~".*node-exporter.*"} == 0
```

持续 2 分钟触发。等级：critical。

### 节点 CPU 过高

```promql
100 - (
  avg by (instance) (
    rate(node_cpu_seconds_total{mode="idle"}[5m])
  ) * 100
) > 85
```

持续 10 分钟触发。等级：warning。

### 节点内存过高

```promql
(
  1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes
) * 100 > 90
```

持续 10 分钟触发。等级：warning。

### 磁盘空间快满

```promql
(
  1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay"} /
      node_filesystem_size_bytes{fstype!~"tmpfs|overlay"}
) * 100 > 85
```

持续 10 分钟触发。等级：warning。超过 95% 可升级为 critical。

### inode 快满

```promql
(
  1 - node_filesystem_files_free{fstype!~"tmpfs|overlay"} /
      node_filesystem_files{fstype!~"tmpfs|overlay"}
) * 100 > 85
```

持续 10 分钟触发。等级：warning。

### IO wait 过高

```promql
avg by (instance) (
  rate(node_cpu_seconds_total{mode="iowait"}[5m])
) * 100 > 20
```

持续 10 分钟触发。等级：warning。

### Pod 发生重启

```promql
sum by (namespace, pod, container) (
  increase(kube_pod_container_status_restarts_total{container!="POD"}[10m])
) > 0
```

持续 1 分钟触发。等级：warning。对噪音较大的 Job 或非生产 namespace 可以过滤。

### Pod CrashLoopBackOff

```promql
max by (namespace, pod, container) (
  kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"}
) == 1
```

持续 2 分钟触发。等级：critical。

### Deployment 不可用

```promql
kube_deployment_status_replicas_unavailable > 0
```

持续 5 分钟触发。生产 namespace 为 critical，其他环境可设为 warning。

### Pod Failed 或 Unknown

```promql
sum by (namespace, pod, phase) (
  kube_pod_status_phase{phase=~"Failed|Unknown"} == 1
) > 0
```

持续 5 分钟触发。等级：warning。

### Service 无可用 Endpoint

```promql
sum by (namespace, service) (
  kube_endpoint_address_available
) == 0
```

持续 3 分钟触发。生产服务等级：critical。

### HTTP 健康检查失败

使用 blackbox-exporter：

```promql
probe_success{job="blackbox-http"} == 0
```

持续 2 分钟触发。等级：critical。

## 服务重启语义

"服务重启" 在 Kubernetes 中通常对应以下症状之一：

- container restart count 增加。
- Deployment/ReplicaSet 滚动发布导致 Pod 被替换。
- Pod 进入 `CrashLoopBackOff`。
- Deployment 在发布中或发布后出现 unavailable replicas。

计划内发布不应该频繁打扰飞书群。建议使用以下方式抑制：

- 发布窗口内创建 Alertmanager silence。
- 针对已知发布 annotation 做规则过滤。
- 使用更高阈值，例如 10 到 15 分钟内多次重启才升级。

## 资源阈值建议

初始阈值：

| 指标 | Warning | Critical |
| --- | --- | --- |
| CPU 使用率 | 10 分钟超过 85% | 10 分钟超过 95% |
| 内存使用率 | 10 分钟超过 90% | 5 分钟超过 95% |
| 磁盘使用率 | 10 分钟超过 85% | 5 分钟超过 95% |
| inode 使用率 | 10 分钟超过 85% | 5 分钟超过 95% |
| IO wait | 10 分钟超过 20% | 5 分钟超过 40% |
| Pod 重启 | 10 分钟内大于 0 | 多次重启或 CrashLoopBackOff |
| Endpoint 可用性 | 3 分钟内为 0 | 生产服务 3 分钟内为 0 |

上线后至少观察一周基线，再调整阈值和重复提醒时间。

## 部署说明

高层安装流程：

```bash
rtk helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
rtk helm repo update
rtk helm install monitoring prometheus-community/kube-prometheus-stack -n monitoring --create-namespace
```

之后补充：

- 飞书 webhook adapter 的 Deployment 和 Service。
- Alertmanager receiver 和 route 配置。
- 自定义 `PrometheusRule`。
- 可选 blackbox-exporter 探测配置。

建议使用 Helm values 或 GitOps manifest 固化配置，不要手动修改集群内生成资源。

## 验证清单

部署后检查：

- Grafana 可以打开，Prometheus datasource 状态正常。
- Node Exporter 看板能看到所有 k3s 节点。
- Kubernetes 看板能看到 namespace、Deployment、Pod、container。
- Prometheus 能查询 `up`、`node_cpu_seconds_total`、`kube_pod_container_status_restarts_total`、`kube_deployment_status_replicas_unavailable`。
- Alertmanager UI 能看到 Feishu receiver。
- 测试告警能到达飞书群。
- 如果开启恢复通知，测试恢复告警也能到达飞书群。
- 飞书消息不暴露密码、token、webhook URL 或其他敏感信息。

## Runbook

生产告警最终都应该有 runbook。每个 runbook 至少包含：

- 告警含义。
- 常见原因。
- 首批排查命令。
- 如何确认用户影响面。
- 如何临时缓解。
- 计划内维护时如何 silence。

后续可新增：

```text
docs/runbooks/node-down.md
docs/runbooks/high-node-cpu.md
docs/runbooks/high-node-memory.md
docs/runbooks/disk-almost-full.md
docs/runbooks/pod-crashloopbackoff.md
docs/runbooks/service-no-endpoint.md
```

## 后续改进

- 为 live-auction-backend API 增加 SLO 告警，重点是出价延迟和错误率。
- 增加 Redis exporter 和 MySQL exporter，覆盖依赖服务健康。
- 增加集群外部探测，覆盖公网域名、CDN、网关和负载均衡。
- 按环境拆分告警路由，开发环境走低噪音群，生产 critical 走值班群。
- 发布系统自动创建 Alertmanager silence，避免计划内发布告警刷屏。
- 为飞书卡片补齐 dashboard 链接和 runbook 链接。
