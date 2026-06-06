# PTS JMeter Public Auction Performance Design

## Context

The local public-domain performance run showed a strong signal that the local machine's outbound public HTTPS/WSS path can become the first bottleneck. Backend resources stayed low, backend restart count stayed at 0, and strict panic/fatal/OOM/killed markers stayed at 0, while client-side public WSS connect and HTTP tail latency degraded.

Alibaba Cloud PTS can provide an external cloud pressure source. The goal is not to replace the Go runner immediately, but to add a JMeter asset that can run from PTS and answer one narrow question first: does public WSS connect and mixed auction traffic look materially better from a cloud pressure source than from the local machine?

## Goals

1. Provide a PTS-uploadable JMeter script for public WSS connect sweep.
2. Provide a second JMeter script for mixed auction load: bid 80%, ranking 10%, item detail 10%, with control/market WebSocket connections.
3. Use CSV input data so PTS does not need to create users, rooms, items, or deposits during the load stage.
4. Make result collection simple: PTS report plus JTL/sample output, later converted into `evidence-redacted.md` and an agent-testing report.
5. Keep all online data scoped by a batch ID and cleanable by local cleanup tooling.

## Non-Goals

- Do not run PTS from this task.
- Do not store production URLs, reusable tokens, DSNs, Redis credentials, or full WebSocket tickets in repo files.
- Do not make PTS create high-volume users during the measured phase.
- Do not use real users, real payments, or non-batch auction items.
- Do not claim formal capacity from a first PTS run without server-side observability and business reconciliation.

## Recommended Approach

Use a three-part asset layout under a new performance-run directory:

```text
docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/
├── README.md
├── jmeter/
│   ├── auction-public-wss-connect-sweep.jmx
│   ├── auction-public-mixed-load.jmx
│   └── sample-users.csv
├── tools/
│   ├── prepare-data.go
│   └── cleanup-data.go
└── evidence-redacted.md
```

The scripts use JMeter CSV parameterization:

```text
user_index,username,password,user_id,room_id,item_id
0,agent_pts_<batch>_u000,<redacted-at-rest-or-generated>,user_xxx,room_xxx,item_xxx
```

The checked-in `sample-users.csv` is only an example. Real CSV files are generated locally and must not be committed if they contain reusable credentials or live entity IDs.

## Data Flow

### Prepare

`prepare-data.go` runs locally or from an approved operator machine against the online public API. It creates:

- one batch-scoped merchant,
- one batch-scoped room,
- one batch-scoped auction item and rule,
- N batch-scoped users,
- paid deposits for all bidding users,
- a CSV file for PTS.

The prepare tool must print a redacted summary:

```text
batch_id:
merchant_created:
room_id: redacted prefix/suffix only
item_id: redacted prefix/suffix only
users_created:
csv_path:
cleanup_command:
```

It must not print tokens, passwords, DSNs, or full online URLs.

### Execute In PTS

Upload to PTS:

- `.jmx` script,
- generated CSV,
- JMeter WebSocket Samplers plugin JAR if PTS does not already provide it.

Provide PTS variables rather than hardcoding:

```text
BASE_URL
WS_BASE_URL
BATCH_ID
TARGET_QPS
TARGET_WS_LOGICAL
CONNECT_TIMEOUT_MS
REQUEST_TIMEOUT_MS
```

The JMeter script logs in each virtual user using CSV username/password, requests `/api/v1/ws-ticket`, connects to WSS, and runs either connect-only or mixed load.

### Collect

After the run, download:

- PTS HTML/report summary,
- JMeter `.jtl` or sample result file,
- any custom sample output emitted by JSR223 listeners.

The downloaded artifacts are stored outside git first. The agent later extracts only redacted summaries into:

```text
docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/evidence-redacted.md
docs/agent-testing/reports/<timestamp>-auction-pts-public-performance.md
```

### Cleanup

`cleanup-data.go` logs in with the batch merchant and users, then:

- closes any temporary WebSocket connections by ending the process,
- cancels the batch item,
- ends the batch room,
- deletes or deactivates batch users through the public API,
- prints attempted and successful cleanup counts.

Cleanup is always limited to the exact batch prefix and CSV contents.

## JMeter Script 1: WSS Connect Sweep

Purpose: isolate public WebSocket ticket and connect path.

Thread group flow:

1. Read a row from `users.csv`.
2. `POST /api/v1/auth/login`.
3. `POST /api/v1/ws-ticket`.
4. WebSocket open: `/ws/v1/rooms/${room_id}?ticket=${ticket}`.
5. Wait for first message or timeout.
6. Close socket.

Metrics:

- login latency,
- ticket latency,
- WSS connect latency,
- first-message wait,
- connect success rate,
- connect error distribution.

Stages:

```text
100 physical WS
400 physical WS
800 physical WS
1200 physical WS
2000 physical WS
```

Stop suggestions:

- connect success below 95%,
- WSS connect P99 above 10s for one stage,
- obvious 5xx or PTS engine errors.

## JMeter Script 2: Mixed Auction Load

Purpose: reproduce public auction load from a cloud pressure source.

Thread groups:

1. `WS Control`: logs in, gets ticket, opens control stream, records `time_sync`.
2. `WS Market`: logs in, gets ticket, opens market stream, records `bid_success` and `user_outbid`.
3. `HTTP Mix`: shared CSV users, logs in, then sends weighted requests:
   - 80% `POST /api/v1/items/{item_id}/bids`,
   - 10% `GET /api/v1/items/{item_id}/ranking?page=1&page_size=20`,
   - 10% `GET /api/v1/items/{item_id}`.

JMeter should use JSR223/Groovy to:

- parse JSON business `code`,
- classify expected rejects such as `40003 price too low`,
- generate monotonically increasing bid prices,
- compute `server_time_unix_ms` arrival delay for `time_sync` and `bid_success`,
- attach metric values to sample labels or variables visible in PTS/JTL.

Initial stages:

```text
50 QPS / 200 physical WS
150 QPS / 800 physical WS
300 QPS / 800 physical WS
```

Only after these are healthy should PTS run higher stages.

## Result Pull Strategy

The user pulls PTS data in two layers:

1. PTS UI report for quick judgment:
   - TPS,
   - success rate,
   - response time P50/P95/P99,
   - HTTP status/error distribution,
   - engine-side resource warnings.
2. Downloaded JTL for agent analysis:
   - per-sampler latency,
   - WebSocket connect sample results,
   - custom arrival delay sample labels,
   - error messages and response codes.

The agent then converts the downloaded files into a redacted summary. The conversion step should ignore or mask:

- full host names if present,
- tokens,
- passwords,
- full ticket query strings,
- user IDs if not needed for aggregate evidence.

## Error Handling

- If prepare fails before creating data, report setup failure and do not run PTS.
- If prepare partially creates data, run cleanup immediately before retrying with a new sub-batch.
- If PTS starts but connect success is below threshold, stop further stages and run cleanup.
- If PTS report lacks JTL or enough detail, mark the result `inconclusive`.
- If cleanup cannot confirm deletion success for some users, record exact batch counts and remediation steps; do not broaden cleanup queries.

## Testing And Verification

Before uploading to PTS:

1. Validate JMX XML can be opened by local JMeter.
2. Run a tiny local smoke with 1-2 users against a non-production or approved endpoint.
3. Confirm no checked-in file contains production URL, token, password, DSN, Redis credential, or full ticket.
4. Confirm `prepare-data.go` and `cleanup-data.go` compile.

After PTS:

1. Confirm PTS report downloaded.
2. Confirm JTL downloaded or exported.
3. Confirm cleanup succeeded or residual items are documented.
4. Write `evidence-redacted.md` and a report under `docs/agent-testing/reports/`.

## Open Decisions

- The first implementation should prioritize WSS connect sweep over full mixed load.
- Real CSV files should stay untracked by default.
- PTS execution itself requires a separate explicit test approval because it touches online dependencies.
