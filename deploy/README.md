# k3s Deploy

The active production deployment path is k3s. The backend, MySQL, Redis, observability stack, Grafana, and the deployment webhook all run as Kubernetes workloads or services inside the cluster.

## Layout

```text
deploy/
  README.md
  legacy-compose/
    README.md
    config.prod.example.yaml
    docker-compose.prod.yml
    pull-and-restart.sh
  observability/
    grafana/
      dashboards/
        dashboard-provider.yaml
        live-auction-dashboard.json
      datasources/
        loki.yaml
        prometheus.yaml
        tempo.yaml
    loki.yaml
    otel-collector.yaml
    prometheus.yaml
    promtail-local.yaml
    promtail.yaml
    tempo.yaml
  k8s/
    kustomization.yaml
    00-namespace.yaml
    01-secrets.example.yaml
    01-secrets.yaml              # local/server only, gitignored
    02-configmaps.yaml
    02b-dashboard-configmap.yaml
    03-redis.yaml
    04-mysql.yaml
    05-otel-collector.yaml
    06-prometheus.yaml
    07-tempo.yaml
    08-loki.yaml
    09-promtail.yaml
    10-grafana.yaml
    11-app.yaml
    12-ingress.yaml
```

`deploy/k8s` is the k3s manifest entrypoint. Use `kubectl apply -k deploy/k8s` so the example secret file is not applied.

The root `docker-compose.yml` is still the local development and local observability path. It mounts `deploy/observability` into the containers, including Grafana datasources and dashboards.

The production Docker Compose files are grouped under `deploy/legacy-compose/` for historical reference. They are not the active production path.

## One-Time Setup

Create the real secrets file from the example:

```bash
cp deploy/k8s/01-secrets.example.yaml deploy/k8s/01-secrets.yaml
```

Edit `deploy/k8s/01-secrets.yaml` and replace every `CHANGE_ME` value:

```text
CHANGE_ME_MYSQL_PASSWORD
CHANGE_ME_MYSQL_ROOT_PASSWORD
CHANGE_ME_AUTH_TOKEN_SECRET
CHANGE_ME_GRAFANA_ADMIN_PASSWORD
```

Create the GHCR pull secret in the same namespace:

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl -n live-auction create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=CHANGE_ME_GITHUB_USERNAME \
  --docker-password=CHANGE_ME_GITHUB_TOKEN \
  --docker-email=CHANGE_ME_EMAIL
```

## Deploy

Apply the full stack:

```bash
kubectl apply -k deploy/k8s
```

Check rollout status:

```bash
kubectl -n live-auction get pods
kubectl -n live-auction get svc
kubectl -n live-auction rollout status deployment/live-auction-backend
```

The app talks to in-cluster services through Kubernetes DNS:

```text
mysql:3306
redis:6379
otel-collector:4317
prometheus:9090
loki:3100
tempo:3200
grafana:3000
```

Do not add `hostAliases` or hard-coded ClusterIP values; Service DNS is the stable contract.

## Observability

The k3s stack includes:

```text
otel-collector
prometheus
tempo
loki
promtail
grafana
```

The backend exports OTLP metrics and traces to `otel-collector:4317`. Prometheus scrapes the collector at `otel-collector:8889`. Promtail runs as a DaemonSet and reads pod logs from `/var/log/pods`, then ships them to Loki.

Grafana provisions the `Live Auction 应用数据大屏` dashboard from `02b-dashboard-configmap.yaml`.

## Local Docker

Use Docker Compose locally when you want MySQL, Redis, Prometheus, Tempo, Loki, Promtail, and Grafana without k3s:

```bash
docker compose up -d
```

Then run the backend on the host with the local config:

```bash
go run main.go server -c config.yaml
```

Local service URLs:

```text
backend:        http://127.0.0.1:8080
grafana:        http://127.0.0.1:3000
prometheus:     http://127.0.0.1:9090
otel grpc:      127.0.0.1:4317
otel http:      127.0.0.1:4318
loki:           http://127.0.0.1:3100
tempo:          http://127.0.0.1:3200
mysql:          127.0.0.1:3306
redis:          127.0.0.1:6379
```

Local Docker Compose keeps Grafana provisioning files in git:

```text
deploy/observability/grafana/datasources/
deploy/observability/grafana/dashboards/dashboard-provider.yaml
deploy/observability/grafana/dashboards/live-auction-dashboard.json
```

Do not remove these files. The local Grafana container mounts them directly, and the k3s dashboard configmap mirrors the same dashboard content in `deploy/k8s/02b-dashboard-configmap.yaml`.

To stop local Docker services:

```bash
docker compose down
```

## Webhook Deploys

GitHub Actions should continue to build and push the image. The webhook service runs inside k3s and should update the backend Deployment in the `live-auction` namespace.

For a tag such as `abc1234`, the webhook should perform the Kubernetes equivalent of:

```bash
kubectl -n live-auction set image deployment/live-auction-backend \
  app=ghcr.io/zet-plane/live-auction-backend:abc1234
kubectl -n live-auction rollout status deployment/live-auction-backend
```

The webhook service account only needs permission to get and patch the `live-auction-backend` Deployment and watch its rollout status.

## Useful Commands

Restart the backend without changing the image:

```bash
kubectl -n live-auction rollout restart deployment/live-auction-backend
```

Inspect backend logs:

```bash
kubectl -n live-auction logs deployment/live-auction-backend -f
```

Port-forward Grafana:

```bash
kubectl -n live-auction port-forward svc/grafana 3000:3000
```
