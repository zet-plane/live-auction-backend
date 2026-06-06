# PTS JMeter Public Auction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the first PTS-uploadable JMeter WSS connect sweep asset for the public auction endpoint.

**Architecture:** The first deliverable is a self-contained JMeter directory with a `.jmx`, sample CSV, README, and evidence placeholder. The `.jmx` consumes CSV rows, logs in, issues a WebSocket ticket, opens one WSS connection, reads one first message, and closes the socket. Real data preparation and mixed-load scripting remain separate follow-up tasks.

**Tech Stack:** Apache JMeter 5.x XML, Alibaba Cloud PTS JMeter scenario, JMeter WebSocket Samplers plugin by Peter Doornbosch, CSV Data Set Config, HTTP samplers, JSON extractors.

---

### Task 1: WSS Connect Sweep Upload Asset

**Files:**
- Create: `docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/README.md`
- Create: `docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/evidence-redacted.md`
- Create: `docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/jmeter/auction-public-wss-connect-sweep.jmx`
- Create: `docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/jmeter/sample-users.csv`

- [ ] **Step 1: Create the directory and README**

Create the run directory and document PTS upload fields, required JMeter properties, and the fact that real CSV files must not be committed.

- [ ] **Step 2: Create sample CSV**

Create an example CSV with non-real rows:

```csv
user_index,username,password,user_id,room_id,item_id
0,agent_pts_example_u000,example_password,user_example_000,room_example,item_example
1,agent_pts_example_u001,example_password,user_example_001,room_example,item_example
```

- [ ] **Step 3: Create WSS connect sweep JMX**

The JMX must contain:

- User Defined Variables for `BASE_PROTOCOL`, `BASE_HOST`, `BASE_PORT`, `BATCH_ID`, `STREAM`, `CONNECT_TIMEOUT_MS`, and `REQUEST_TIMEOUT_MS`.
- CSV Data Set Config reading `users.csv`.
- HTTP Cookie Manager.
- HTTP Header Manager with JSON content type.
- HTTP login sampler: `POST /api/v1/auth/login`.
- JSON extractor for `data.token`.
- HTTP ticket sampler: `POST /api/v1/ws-ticket`.
- JSON extractor for `data.ticket`.
- Open WebSocket sampler using:
  - testclass `eu.luminis.jmeter.wssampler.OpenWebSocketSampler`
  - guiclass `eu.luminis.jmeter.wssampler.OpenWebSocketSamplerGui`
  - `server=${BASE_HOST}`
  - `port=${BASE_PORT}`
  - `path=/ws/v1/rooms/${room_id}?ticket=${ticket}&stream=${STREAM}`
  - `TLS=true`
  - `connectTimeout=${CONNECT_TIMEOUT_MS}`
  - `readTimeout=${REQUEST_TIMEOUT_MS}`
- Single Read WebSocket sampler for first message.
- Close WebSocket sampler.

- [ ] **Step 4: Verify XML and sensitive scan**

Run:

```bash
rtk rg -n "auction-api|115\\.191|Bearer|token=|ticket=[A-Za-z0-9]|password:" docs/agent-testing/performance-runs/agent_pts_auction_public_20260606
```

Expected: only placeholder variable references such as `${ticket}` or documentation warnings; no real values.

- [ ] **Step 5: Report completion**

Tell the user to upload `auction-public-wss-connect-sweep.jmx`, rename/copy their generated CSV to `users.csv`, and upload the WebSocket plugin JAR in PTS.
