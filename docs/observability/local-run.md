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
http_server_request_count
auction_bid_count
auction_place_bid_lua_result_count
db_client_operation_count
cron_job_run_count
```

Grafana checks:

- Open `http://127.0.0.1:3000`.
- Confirm Prometheus, Tempo, and Loki datasources are provisioned.
- Confirm `Live Auction Overview`, `Live Auction Bidding`, and `Live Auction Logs` dashboards are visible.
- After a bid request, confirm Tempo contains `auction.place_bid`.
- In Loki, query `{service_name="live-auction-backend"}` and filter by `trace_id`.
