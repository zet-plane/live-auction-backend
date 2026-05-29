# Production Deploy

GitHub Actions only builds and pushes the image. The server pulls the image and restarts containers by webhook or cron.

Prepare the server once:

```bash
mkdir -p /opt/live-auction-backend
cd /opt/live-auction-backend
docker login ghcr.io
```

Copy these files to the server deploy directory:

```text
docker-compose.prod.yml
pull-and-restart.sh
config.prod.example.yaml
observability/
```

Create `/opt/live-auction-backend/config.yaml` from `config.prod.example.yaml`, then replace every `CHANGE_ME` value. The important production hostnames are:

```yaml
http:
  host: 0.0.0.0
  port: "8080"
database:
  dsn: live_auction:CHANGE_ME@tcp(mysql:3306)/live_auction?charset=utf8mb4&parseTime=True&loc=Local
redis:
  addr: redis:6379
security:
  allowed_origins:
    - "*"
observability:
  enabled: true
  otlp_endpoint: otel-collector:4317
  trace_sample_ratio: 0.1
  logs:
    format: json
    output: stdout
```

`security.allowed_origins: ["*"]` is convenient for deployment smoke tests and frontend integration. Before a public production launch, replace it with the exact frontend origin, such as `https://your-frontend.example.com`.

Create `/opt/live-auction-backend/.env`:

```dotenv
MYSQL_DATABASE=live_auction
MYSQL_USER=live_auction
MYSQL_PASSWORD=CHANGE_ME
MYSQL_ROOT_PASSWORD=CHANGE_ME
IMAGE_TAG=latest
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=CHANGE_ME
GRAFANA_BIND_ADDR=127.0.0.1
GRAFANA_PORT=3000
PROMETHEUS_RETENTION=15d
```

## Production Observability

The production compose file starts the app plus:

```text
otel-collector
prometheus
tempo
loki
promtail
grafana
```

Only the app and Grafana publish host ports by default:

```text
app:     127.0.0.1:8080 -> container 8080
grafana: 127.0.0.1:3000 -> container 3000
```

Prometheus, Loki, Tempo, and the OpenTelemetry collector stay on the internal Compose network. Put Grafana behind a reverse proxy, VPN, or SSH tunnel before exposing it outside the server.

The app sends metrics and traces to `otel-collector:4317`. Logs are JSON on stdout; promtail reads the Docker logs for the `live-auction-backend` container and sends them to Loki with:

```text
service_name=live-auction-backend
environment=production
```

Grafana provisions the `Live Auction 应用数据大屏` dashboard from:

```text
observability/grafana/dashboards/live-auction-dashboard.json
```

Bring up or refresh the full production stack:

```bash
docker compose -f docker-compose.prod.yml up -d --remove-orphans
```

## Webhook Pull

Configure GitHub Secrets:

```text
DEPLOY_WEBHOOK_URL
DEPLOY_WEBHOOK_TOKEN
```

The webhook service should verify `DEPLOY_WEBHOOK_TOKEN`, read the JSON `tag`, then run:

```bash
DEPLOY_DIR=/opt/live-auction-backend /opt/live-auction-backend/pull-and-restart.sh "$tag"
```

## Cron Pull

If you do not want a webhook, leave `DEPLOY_WEBHOOK_URL` empty and run the puller from cron:

```cron
*/2 * * * * DEPLOY_DIR=/opt/live-auction-backend /opt/live-auction-backend/pull-and-restart.sh latest
```

Cron tracks `latest`; webhook can deploy the exact short SHA tag from the CI run.
