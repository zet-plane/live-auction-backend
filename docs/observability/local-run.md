# Local Observability Runbook

## Start

```bash
rtk docker compose up -d mysql redis otel-collector prometheus tempo loki promtail grafana
mkdir -p logs
rtk go run main.go server -c config.yaml 2>&1 | tee logs/live-auction.log
```

## Verify

```bash
rtk curl http://127.0.0.1:8080/api/v1/health
```

Prometheus checks:

```text
http_server_request_count_total
auction_bid_count_total
auction_place_bid_lua_result_count_total
db_client_operation_count_total
cron_job_run_count_total
```

Grafana checks:

- Open `http://127.0.0.1:3000`.
- Confirm Prometheus, Tempo, and Loki datasources are provisioned.
- Confirm the `Live Auction 应用数据大屏` dashboard is visible.
- After a bid request, confirm Tempo contains `auction.place_bid`.
- In Loki, query `{service_name="live-auction-backend"}` and filter by `trace_id`.
