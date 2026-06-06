# PTS JMeter Public Auction Run

This directory contains the first PTS-uploadable JMeter asset for public WSS connect testing.

## Upload Files

Upload these files to Alibaba Cloud PTS JMeter scenario:

- `jmeter/auction-public-wss-connect-sweep.jmx`
- generated `jmeter/users.csv`

The checked-in `jmeter/sample-users.csv` is only a format example. Do not use it for a real run.

The JMX uses JMeter's built-in JSR223/Groovy sampler and Java 11 `java.net.http.WebSocket`, so it does not require uploading a WebSocket plugin JAR.

PTS must show the CSV dependency node as:

- node name: `users.csv`
- file name: `users.csv`

## Generate Real CSV

Generate real online test data only after the current batch is approved:

```bash
rtk env \
  PTS_BASE_URL="https://<redacted-public-api-host>" \
  PTS_BATCH_ID="agent_pts_auction_public_20260606_run1" \
  PTS_USER_COUNT=120 \
  go run ./docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/tools/pts-data prepare
```

The command creates a batch-scoped merchant, room, started item, and virtual users, then writes:

```text
docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/jmeter/users.csv
```

This generated CSV contains real test account passwords and online entity IDs. It is ignored by git and must not be committed.

After the PTS run, clean up the same batch:

```bash
rtk env \
  PTS_BASE_URL="https://<redacted-public-api-host>" \
  PTS_BATCH_ID="agent_pts_auction_public_20260606_run1" \
  go run ./docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/tools/pts-data cleanup
```

## PTS Variables

Set or override these variables in PTS:

| Name | Example | Notes |
| --- | --- | --- |
| `BASE_PROTOCOL` | `https` | HTTP API scheme |
| `BASE_HOST` | `<redacted-public-host>` | Do not commit the real host |
| `BASE_PORT` | `443` | Public HTTPS/WSS port |
| `STREAM` | `control` | Use `control` first for connect sweep |
| `CONNECT_TIMEOUT_MS` | `15000` | WSS connect timeout |
| `REQUEST_TIMEOUT_MS` | `15000` | HTTP and first-message timeout |

## Recommended First Run

- Pressure source: public network.
- Pressure mode: virtual user mode.
- Traffic model: step ramp.
- Max virtual users: `100`.
- Duration: `5` minutes.
- Ramp duration: `1` minute.
- Ramp steps: `3`.
- IP count: `1`.
- Region customization: off for the first run.

For this connect sweep script, `1 VU = 1 physical WSS connection`.

## Stop Conditions

Stop and download results if any of these happen:

- connect success rate below `95%`,
- WSS connect P99 above `10s`,
- clear HTTP 5xx increase,
- PTS engine errors.

## Known PTS Engine Error

If the engine log contains:

```text
CannotResolveClassException: eu.luminis.jmeter.wssampler.OpenWebSocketSampler
```

then the uploaded JMX still uses the old WebSocket Samplers plugin format. Re-upload the current `jmeter/auction-public-wss-connect-sweep.jmx`; it no longer contains `eu.luminis.*` sampler classes.

## Result Collection

After the run, download the PTS report and JMeter sample/JTL output. Store raw downloads outside git first. Later, summarize only redacted aggregate evidence into `evidence-redacted.md`.

Never commit production URLs, tokens, passwords, full tickets, DSNs, Redis credentials, or real generated CSV files.
