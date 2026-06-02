---
name: live-auction-online-ops
description: Use when working with the live-auction-backend online server, k3s deployment, deploy webhook, mihomo proxy, production manifests, ingress, observability, or remote operational diagnostics for this repo.
---

# Live Auction Online Ops

Project-local skill for `live-auction-backend` only. Do not copy this context to other repositories.

## Trigger

Use this skill for tasks involving:

- SSH to the online server.
- k3s, kubectl, manifests, Ingress, TLS, Deployments, Services, PVCs, or runtime pods.
- The deploy webhook that patches the backend image.
- The `mihomo` proxy and network/proxy-sensitive diagnostics.
- Online observability, Prometheus, Grafana, Loki, Tempo, Alertmanager, Feishu alerts, or PrometheusAlert.

## Safety

- Default to read-only inspection first.
- Do not modify k3s resources, restart services, scale, roll back, edit manifests, or patch images unless the user explicitly asks.
- Do not print or commit Secret values, kubeconfig contents, database DSNs, Redis credentials, webhook tokens, proxy credentials, or reusable tokens.
- If remote manifests are copied locally for parsing, store them only under `/private/tmp` and delete the copy after use.
- Remote files include secrets in YAML; summarize Secret names and keys, not values.

## Remote Anchors

- SSH entry: `ssh deploy@115.191.46.148`.
- Host observed: `plan-z`.
- Main online files: `/home/deploy`.
- k3s restore manifests: `/home/deploy/live-auction-k3s-restore/k8s/k8s`.
- Deploy webhook restore files: `/home/deploy/live-auction-k3s-restore/webhook`.
- Ignore `._*` files in restore directories; they are macOS resource-fork sidecars.

## k3s Runtime

- Single-node k3s on node `plan-z`, public IP `115.191.46.148`, internal IP `172.31.5.217`.
- Main namespace: `live-auction`.
- Supporting namespaces include `kube-system` and `cert-manager`.
- In `live-auction`, the expected workloads are:
  - Deployments: `live-auction-backend`, `mysql`, `redis`, `otel-collector`, `prometheus`, `grafana`, `loki`, `tempo`, `alertmanager`, `prometheusalert`, `feishu-webhook`, `kube-state-metrics`.
  - DaemonSets: `node-exporter`, `promtail`.
  - PVCs: `mysql-data` 10Gi, `redis-data` 1Gi, `prometheus-data` 10Gi, `loki-data` 5Gi, `tempo-data` 5Gi, `grafana-data` 1Gi.

Useful read-only checks:

```bash
rtk ssh deploy@115.191.46.148 "kubectl get nodes -o wide"
rtk ssh deploy@115.191.46.148 "kubectl get pods,svc,deploy,daemonset,pvc,ingress -n live-auction -o wide"
rtk ssh deploy@115.191.46.148 "kubectl top nodes"
rtk ssh deploy@115.191.46.148 "kubectl top pods -n live-auction"
rtk ssh deploy@115.191.46.148 "kubectl rollout status deployment/live-auction-backend -n live-auction"
```

## Backend Deployment

- Manifest: `11-app.yaml`.
- Deployment: `live-auction-backend`.
- Container: `app`.
- Image in restore manifest may be `ghcr.io/zet-plane/live-auction-backend:latest`.
- Actual runtime image may be patched to a commit tag by the deploy webhook. Always verify:

```bash
rtk ssh deploy@115.191.46.148 "kubectl get deployment live-auction-backend -n live-auction -o jsonpath='{.spec.template.spec.containers[0].image}{\"\\n\"}'"
```

- Backend Service: `LoadBalancer`, port `8088` to targetPort `8080`.
- Backend Ingress host: `auction-api.kirac0on.com`, Traefik, TLS secret `live-auction-tls`.
- Init containers wait for:
  - MySQL: `mysql:8.4`, service `mysql`, user/database `live_auction`, password from `mysql-credentials`.
  - Redis: `redis:7-alpine`, service `redis`.
- App config is Secret `app-config`, mounted as `/config/config.yaml`.
- Redacted config shape: release mode, HTTP `0.0.0.0:8080`, MySQL DSN, Redis `redis:6379`, auth secret/TTL, `allowed_origins: ["*"]`, auction anti-snipe defaults, observability enabled with OTLP `otel-collector:4317`.

## Ingress And TLS

- Manifest: `12-ingress.yaml`.
- Backend: `auction-api.kirac0on.com` -> service `live-auction-backend:8088`.
- Grafana: `grafana.kirac0on.com` -> service `grafana:3000`.
- Ingress class: `traefik`.
- ClusterIssuer: `letsencrypt-prod`, Cloudflare DNS-01.
- Certificates observed: `live-auction-tls` and `grafana-tls`.

## Observability And Alerts

- OTel Collector receives OTLP and exports:
  - metrics to Prometheus exporter `:8889`
  - traces to Tempo `tempo:4317`
- Prometheus scrapes `otel-collector`, `kube-state-metrics`, and `node-exporter`.
- Grafana uses Prometheus, Loki, and Tempo datasources, with a combined dashboard ConfigMap.
- Loki stores logs; Promtail tails `/var/log/pods`.
- Alertmanager sends to `prometheusalert:8080/prometheusalert?type=fs&tpl=prometheus-fs`.
- Alert rules cover HTTP 5xx, P95/P99 latency, bid/order failure rate, node CPU/memory, Pod restarts/NotReady, backend unavailable, and missing app metrics.
- A separate in-cluster Python `feishu-webhook` exists; it reads Feishu webhook and signing secret from `feishu-webhook-secret`.

## Deploy Webhook

- User systemd service, not system-level systemd:

```bash
rtk ssh deploy@115.191.46.148 "systemctl --user status deploy-webhook --no-pager"
rtk ssh deploy@115.191.46.148 "systemctl --user cat deploy-webhook --no-pager"
```

- Unit path: `/home/deploy/.config/systemd/user/deploy-webhook.service`.
- Binary: `/home/deploy/deploy-webhook/deploy-webhook`.
- Env file: `/home/deploy/.deploy-webhook.env` with `WEBHOOK_TOKEN`.
- Listens on `:9000`.
- Routes:
  - `GET /healthz`
  - `POST /deploy` with `Authorization: Bearer <token>`
- Request JSON has `image`, `tag`, and `sha`; the handler patches Deployment `live-auction-backend`, namespace `live-auction`, container `app`.
- Uses kubeconfig `/etc/rancher/k3s/k3s.yaml`.

## mihomo

- systemd service: `mihomo.service`.
- Command: `/usr/local/bin/mihomo -d /etc/mihomo`.
- Config directory: `/etc/mihomo`.
- Observed config shape: mixed port `127.0.0.1:7890`, controller `127.0.0.1:9090`, mode `rule`, log level `error`.
- Drop-in: `/etc/systemd/system/mihomo.service.d/k3s-ingress-bypass.conf`.
- Bypass script: `/usr/local/sbin/mihomo-k3s-ingress-bypass.sh`.
- Bypass intent:
  - Public ingress ports `{22, 80, 443, 9000}` on `115.191.46.148` return before proxy rules.
  - k3s CIDRs `{10.42.0.0/16, 10.43.0.0/16}` return before proxy rules.

Useful read-only checks:

```bash
rtk ssh deploy@115.191.46.148 "systemctl status mihomo --no-pager"
rtk ssh deploy@115.191.46.148 "systemctl cat mihomo --no-pager"
rtk ssh deploy@115.191.46.148 "ss -ltnp"
```
