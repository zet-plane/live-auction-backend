# K8s 节点资源观测设计

## 背景

当前 `deploy/k8s/` 已经部署了应用可观测性基础组件：

- `otel-collector` 接收 live-auction-backend 通过 OTLP 上报的 metrics 和 traces。
- `prometheus` 抓取 `otel-collector:8889` 暴露的应用指标。
- `grafana` 已配置 Prometheus、Loki、Tempo 数据源。
- `promtail` 以 DaemonSet 运行，但只负责采集 Pod 日志。

这套链路已经能观察应用请求量、错误率、HTTP 延迟、DB 查询速率、日志和 trace。但 Grafana 要看到服务器本身的 CPU、内存、网络、磁盘、load average，还缺少节点级指标采集器。

## 目标

- Grafana 能查看每台 Kubernetes 节点的 CPU、内存、网络、磁盘和负载情况。
- Prometheus 能自动发现并抓取每个节点上的采集端点。
- 第一版保持现有手写 YAML 部署方式，不引入 Helm 作为强依赖。
- 节点观测和应用观测共用现有 Prometheus/Grafana。
- 后续可平滑扩展到 kube-state-metrics、Alertmanager 或 kube-prometheus-stack。

## 非目标

- 不在第一版重构现有 OpenTelemetry、Loki、Tempo 链路。
- 不在第一版引入完整 Kubernetes 控制面监控。
- 不在第一版替换为 Grafana Alloy 或 kube-prometheus-stack。
- 不把服务器 IP、线上域名、token、密码写入 manifest 或文档。

## 当前链路

```text
live-auction-backend
  -> OTLP metrics/traces
  -> otel-collector
  -> Prometheus scrape otel-collector:8889
  -> Grafana

promtail DaemonSet
  -> Loki
  -> Grafana
```

当前 Prometheus 只配置了一个 scrape job：

```yaml
scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ["otel-collector:8889"]
```

因此 Grafana 看到的是应用指标，不是节点指标。

## 推荐方案

第一版建议使用 `node-exporter`。

```text
Kubernetes node
  +- node-exporter DaemonSet
     +- expose :9100/metrics
     +- collect CPU / memory / network / disk / load

Prometheus
  +- kubernetes_sd_configs discovers node-exporter pods
  +- scrape node-exporter metrics

Grafana
  +- Prometheus datasource
  +- Node Exporter dashboard
```

选择理由：

- 改动小，只需新增一个 DaemonSet、ServiceAccount/RBAC、Service，并扩展 Prometheus scrape config。
- 指标命名成熟稳定，Grafana 社区 dashboard 丰富。
- 和当前 Prometheus/Grafana 部署方式兼容。
- 采集服务器网络、CPU、内存、磁盘这类指标最直接。

## 需要新增的 Kubernetes 资源

建议新增文件：

```text
deploy/k8s/06b-node-exporter.yaml
```

包含：

- `ServiceAccount/node-exporter`
- `ClusterRole/node-exporter`
- `ClusterRoleBinding/node-exporter`
- `DaemonSet/node-exporter`
- `Service/node-exporter`

`node-exporter` DaemonSet 建议配置：

- `hostNetwork: true`
- `hostPID: true`
- toleration control-plane 节点，保证单节点 k3s 也能采集。
- 挂载宿主机 `/proc`、`/sys`、`/`。
- 使用 `--path.procfs=/host/proc`、`--path.sysfs=/host/sys`、`--path.rootfs=/host/root`。
- 忽略容器运行时和临时文件系统，降低噪音。

Prometheus 需要获得发现 Pod/Endpoint 的权限。当前 Prometheus Deployment 没有显式 `serviceAccountName`，第一版建议新增：

- `ServiceAccount/prometheus`
- `ClusterRole/prometheus`
- `ClusterRoleBinding/prometheus`

授权资源：

```text
nodes
nodes/metrics
services
endpoints
pods
```

动词：

```text
get
list
watch
```

## Prometheus 配置

在 `prometheus-config` 中增加 `node-exporter` scrape job：

```yaml
scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ["otel-collector:8889"]

  - job_name: node-exporter
    kubernetes_sd_configs:
      - role: endpoints
        namespaces:
          names: [live-auction]
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_label_app]
        regex: node-exporter
        action: keep
      - source_labels: [__meta_kubernetes_endpoint_port_name]
        regex: metrics
        action: keep
      - source_labels: [__meta_kubernetes_pod_node_name]
        target_label: node
      - source_labels: [__meta_kubernetes_pod_host_ip]
        target_label: host_ip
```

这样 Prometheus 不需要写死节点 IP，节点扩缩容后也能自动发现。

## Grafana 看板

第一版推荐直接导入社区看板：

```text
Node Exporter Full
Dashboard ID: 1860
```

如果希望像当前 `Live Auction 应用数据大屏` 一样通过 ConfigMap 自动 provision，可新增：

```text
deploy/k8s/02c-node-dashboard-configmap.yaml
```

然后在 `grafana-dashboard-provider` 里继续使用同一个 dashboard path，或在现有 `grafana-dashboard-live-auction` ConfigMap 中追加节点资源 dashboard JSON。

第一版也可以先手动导入 dashboard，确认指标稳定后再固化为 ConfigMap。

## 核心指标

CPU 使用率：

```promql
100 - avg by (instance, node) (
  rate(node_cpu_seconds_total{mode="idle"}[5m])
) * 100
```

每个节点按 CPU mode 分解：

```promql
sum by (instance, node, mode) (
  rate(node_cpu_seconds_total[5m])
)
```

内存使用率：

```promql
(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100
```

网络接收速率：

```promql
sum by (instance, node) (
  rate(node_network_receive_bytes_total{device!~"lo|veth.*|cali.*|flannel.*|docker.*"}[5m])
)
```

网络发送速率：

```promql
sum by (instance, node) (
  rate(node_network_transmit_bytes_total{device!~"lo|veth.*|cali.*|flannel.*|docker.*"}[5m])
)
```

网络错误包：

```promql
sum by (instance, node) (
  rate(node_network_receive_errs_total[5m]) +
  rate(node_network_transmit_errs_total[5m])
)
```

磁盘使用率：

```promql
100 - (
  node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|squashfs"}
  /
  node_filesystem_size_bytes{fstype!~"tmpfs|overlay|squashfs"}
) * 100
```

节点负载：

```promql
node_load1
node_load5
node_load15
```

负载和 CPU 核数对比：

```promql
node_load5
/
count by (instance, node) (node_cpu_seconds_total{mode="idle"})
```

## 验收标准

部署后检查：

```bash
rtk kubectl -n live-auction get pods -l app=node-exporter
rtk kubectl -n live-auction get svc node-exporter
rtk kubectl -n live-auction port-forward svc/prometheus 9090:9090
```

Prometheus 查询应有数据：

```text
up{job="node-exporter"}
node_cpu_seconds_total
node_memory_MemAvailable_bytes
node_network_receive_bytes_total
node_network_transmit_bytes_total
node_load1
```

Grafana 验收：

- Prometheus 数据源健康。
- `Node Exporter Full` dashboard 能看到节点。
- CPU、内存、网络收发、磁盘、load average 面板有数据。
- 节点重启或 node-exporter Pod 重建后，Prometheus targets 能自动恢复。

## 后续演进

### 增加 kube-state-metrics

`node-exporter` 只能看服务器本身，不能回答 Deployment、Pod、Service 状态问题。后续建议增加 `kube-state-metrics`，补齐：

- Deployment 可用副本数。
- Pod phase。
- Pod restart count。
- Service endpoint 数量。
- Job/CronJob 状态。
- request/limit 配置。

### 增加 kubelet/cAdvisor 指标

如果要看 Pod/container 真实 CPU 和内存使用，应继续采集 kubelet/cAdvisor：

```text
container_cpu_usage_seconds_total
container_memory_working_set_bytes
container_fs_usage_bytes
```

这部分需要额外处理 kubelet HTTPS、token、证书和 RBAC，建议作为第二阶段。

### 增加 Alertmanager

节点指标稳定后，再加告警：

- 节点不可达。
- CPU 持续过高。
- 内存持续过高。
- 磁盘空间不足。
- inode 不足。
- 网络错误包异常。
- load average 持续超过 CPU 核数。

告警推送方案可参考 `docs/design/k3s-monitoring-alerting.md`。

### 评估 kube-prometheus-stack

如果后续希望一次性拥有 Prometheus、Alertmanager、Grafana、node-exporter、kube-state-metrics、默认 dashboard 和默认规则，可以迁移到 `kube-prometheus-stack`。

当前阶段不建议直接迁移，因为已有 `deploy/k8s/` 手写部署，先用 `node-exporter` 补齐节点资源观测成本最低。

## 决策

第一阶段采用：

```text
node-exporter DaemonSet
  + Prometheus kubernetes_sd scrape
  + Grafana Node Exporter dashboard
```

第二阶段再补：

```text
kube-state-metrics
  + kubelet/cAdvisor
  + Alertmanager
```

这样可以快速让 Grafana 看到服务器 CPU、内存、网络和负载，同时保留现有应用观测链路。
