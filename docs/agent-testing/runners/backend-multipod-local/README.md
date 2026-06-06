# Backend Multipod Local Runner

This runner verifies the local two-backend equivalent of the Redis event bus path:

- two backend instances share the same local MySQL and Redis;
- WebSocket clients connect to the subscriber instance;
- auction business actions are triggered through the producer instance;
- room fanout events cross instances through Redis;
- winner user unicast crosses instances through Redis;
- final state is recovered through HTTP on the subscriber instance;
- cleanup only touches the current batch prefix and known room/item/ticket keys.

## Prerequisites

Start local MySQL and Redis first:

```bash
rtk docker compose up -d mysql redis
```

Set `TEST_DSN` in the shell that runs the runner. Do not paste the real value into reports.

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN='<omitted>' \
  go run ./docs/agent-testing/runners/backend-multipod-local -start-backends
```

By default `-start-backends` starts two local backend processes on:

- producer: `http://127.0.0.1:18080`
- subscriber: `http://127.0.0.1:18081`

The generated backend config files are written under a temporary directory and removed during cleanup.

## Reusing Already Running Backends

If two local instances are already running, omit `-start-backends`:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN='<omitted>' \
  go run ./docs/agent-testing/runners/backend-multipod-local \
  -producer-url http://127.0.0.1:18080 \
  -subscriber-url http://127.0.0.1:18081
```

Useful flags and matching env vars:

| Flag | Env | Default |
| --- | --- | --- |
| `-batch` | `TEST_BATCH_ID` | `agent_multipod_<timestamp>_` |
| `-producer-url` | `TEST_PRODUCER_URL` | `http://127.0.0.1:18080` |
| `-subscriber-url` | `TEST_SUBSCRIBER_URL` | `http://127.0.0.1:18081` |
| `-redis-addr` | `TEST_REDIS_ADDR` | `127.0.0.1:6379` |
| `-repo-root` | `TEST_REPO_ROOT` | `.` |
| `-start-backends` | `TEST_START_BACKENDS` | `false` |

## Expected Evidence

The runner prints `=== CASE`, `=== SUMMARY`, and `=== CLEANUP` blocks in the format required by `docs/agent-testing/guides/go-runner.md`.

Expected passing cases include:

- producer and subscriber `/readyz`;
- batch user registration;
- merchant promotion;
- room creation and start;
- item creation and publish;
- subscriber WebSocket connections;
- producer `auction_started` fanout observed on subscriber;
- producer price-cap bid fanout and winner `order_created` unicast observed on subscriber;
- subscriber HTTP final state recovery.

## Safety

- `TEST_DSN` is read from the environment and never printed.
- Ticket values are omitted from output.
- Redis cleanup deletes only known room/item keys and tickets issued by this runner.
- MySQL cleanup deletes only rows tied to this batch prefix or known item/room IDs.
