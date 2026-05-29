# Legacy Production Compose

These files are kept for historical reference only.

Current production deployment runs on k3s through `deploy/k8s`.

Current local development uses the root `docker-compose.yml`, which mounts `deploy/observability` for Prometheus, Loki, Tempo, Promtail, and Grafana provisioning.

Files in this directory:

```text
config.prod.example.yaml
docker-compose.prod.yml
pull-and-restart.sh
```

Do not use this directory as the default production deploy path.
