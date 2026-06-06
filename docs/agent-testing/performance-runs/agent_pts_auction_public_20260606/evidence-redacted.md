# Evidence Redacted: PTS public auction run

## Status

- Status: assets and real CSV prepared, PTS run not executed.
- JMeter script: `jmeter/auction-public-wss-connect-sweep.jmx`
- CSV example: `jmeter/sample-users.csv`
- Generated CSV: `jmeter/users.csv` (git-ignored, not committed)

## Data Preparation

- Batch ID: `agent_pts_auction_public_20260606_run1`
- Route: `docs/agent-testing/README.md` -> `templates/protocol.md` -> `guides/environment.md`
- Dependencies: user-approved online public API; concrete endpoint omitted
- Created data: batch-scoped merchant, room, started item, and 120 virtual users
- CSV shape: `user_index,username,password,user_id,room_id,item_id`
- CSV rows: 121 total lines, 120 data rows
- Cleanup status: pending until after the PTS run; use `tools/pts-data cleanup` with the same batch

## PTS Engine Debug

- Time: 2026-06-06 17:17:59 CST
- Symptom: PTS failed while loading the JMX before executing traffic
- Root cause: missing `eu.luminis.jmeter.wssampler.OpenWebSocketSampler` class in the PTS engine classpath
- Fix: replaced WebSocket plugin samplers with a built-in JSR223/Groovy sampler using Java 11 `java.net.http.WebSocket`
- Verification: current JMX XML validates and no longer contains `eu.luminis.*` sampler classes

Execution evidence will be added after a separately approved PTS run.
