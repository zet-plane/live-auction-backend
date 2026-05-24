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
```

Create `/opt/live-auction-backend/config.yaml` with container hostnames:

```yaml
http:
  host: 0.0.0.0
  port: "8080"
database:
  dsn: live_auction:CHANGE_ME@tcp(mysql:3306)/live_auction?charset=utf8mb4&parseTime=True&loc=Local
redis:
  addr: redis:6379
```

Create `/opt/live-auction-backend/.env`:

```dotenv
MYSQL_DATABASE=live_auction
MYSQL_USER=live_auction
MYSQL_PASSWORD=CHANGE_ME
MYSQL_ROOT_PASSWORD=CHANGE_ME
IMAGE_TAG=latest
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
