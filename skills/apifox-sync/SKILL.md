---
name: apifox-sync
description: Use when syncing API changes to Apifox, importing or reconciling a desired OpenAPI document with the current Apifox project, or preserving native WebSocket API records while updating HTTP APIs, schemas, examples, and folders.
---

# Apifox Sync

## Prerequisite

First check whether an Apifox MCP server for the target project is loaded and callable.

**If MCP tools are available**: use them to refresh and read the current OAS.

**If MCP tools are unavailable** (not yet connected, session not reloaded): fall back to the locally cached OAS at `~/.apifox-mcp-server/project-{projectId}/original.json`. This file is written by a previous MCP session and is usually fresh enough for diffing. Do not block the sync on MCP availability — proceed with the cached file and note the fallback in your plan.

Typical MCP JSON config:

```json
{
  "mcpServers": {
    "<project-name>": {
      "command": "npx",
      "args": [
        "-y",
        "apifox-mcp-server@latest",
        "--project-id=<project-id>"
      ],
      "env": {
        "APIFOX_ACCESS_TOKEN": "<access-token>"
      }
    }
  }
}
```

For Codex `config.toml`, use the equivalent `[mcp_servers.<project-name>]` and `[mcp_servers.<project-name>.env]` tables. After MCP is loaded, use it to refresh/read the current OAS before planning writes.

## Required Human Inputs

Ask the human for any missing required parameters before configuring MCP or writing Apifox. Do not guess these values:

- Apifox project id (`--project-id`)
- Apifox Personal Access Token (`APIFOX_ACCESS_TOKEN`)
- MCP server name / project name to use in config
- Target OpenAPI source: code-generated command, docs file/path, or provided OpenAPI file
- Whether Apifox should be pruned exactly, meaning endpoints/folders/schemas absent from the desired OpenAPI may be deleted
- Whether native Apifox WebSocket APIs should be changed. Default is preserve/update only matching WebSocket records; never prune them from an OpenAPI-only sync.
- Which Apifox project/environment/branch to target if multiple exist

If a value can be discovered safely from local config, read it and confirm it with the human before write operations. Never expose the token in messages or logs.

## Core Rule

Use Apifox MCP for reading and verification, and Apifox Open API for writes. The installed `apifox-mcp-server` is read-only: it only reads OAS, reads `$ref` resources, and refreshes OAS. Before any Apifox create, update, delete, or import operation, show the proposed changes and ask the human for explicit approval.

## Workflow

1. Read current OAS: use MCP if available, otherwise use `~/.apifox-mcp-server/project-{projectId}/original.json`.
2. Read relevant path/schema `$ref` files.
3. Make backend/docs changes first.
4. Build a narrow action plan: create/update/delete/import by `METHOD path`, include affected API IDs when known, and separate Apifox-only metadata preservation from intended spec changes.
5. Before writing to Apifox, present the create/update/delete/import plan and wait for explicit human approval.
6. Apply approved changes only, preferably with a temporary targeted script that reads credentials in memory and prints only IDs, methods, and paths.
7. Verify in two layers: re-fetch changed Open API records, then refresh MCP OAS and re-read changed `$ref` resources.
8. Report exact changed endpoint IDs and any remaining intended or unintended differences.

## Credentials

Find project config. The TOML layout has a main table and a separate `.env` sub-table, so a simple section-stop sed misses the token. It is acceptable to print the project id, but never print the token. Prefer reading both values inside a temporary script so the token stays in memory:

```js
import fs from "node:fs";
const config = fs.readFileSync(`${process.env.HOME}/.codex/config.toml`, "utf8");
const projectId = config.match(/--project-id=(\d+)/)?.[1];
const token = config.match(/APIFOX_ACCESS_TOKEN\s*=\s*"([^"]+)"/)?.[1];
if (!projectId || !token) throw new Error("missing Apifox config");
```

If using shell, assign the token to a variable through command substitution or a secret manager and do not echo it. Never paste token values into chat, reports, filenames, or logs.

Required headers:

```text
Authorization: Bearer $TOKEN
X-Apifox-Api-Version: 2024-03-28
Content-Type: application/json
```

Network calls usually need escalated shell permission.

## Open API Endpoints

```text
GET    /api/v1/projects/{projectId}/http-apis
GET    /api/v1/projects/{projectId}/http-apis/{apiId}
POST   /api/v1/projects/{projectId}/http-apis
PUT    /api/v1/projects/{projectId}/http-apis/{apiId}
DELETE /api/v1/projects/{projectId}/http-apis/{apiId}
GET    /api/v1/projects/{projectId}/websocket-apis
GET    /api/v1/projects/{projectId}/websocket-apis/{apiId}
POST   /api/v1/projects/{projectId}/websocket-apis
PUT    /api/v1/projects/{projectId}/websocket-apis/{apiId}
GET    /api/v1/projects/{projectId}/api-folders
DELETE /api/v1/projects/{projectId}/api-folders/{folderId}
GET    /api/v1/projects/{projectId}/data-schemas
POST   /v1/projects/{projectId}/import-openapi
```

Base host: `https://api.apifox.com`.

## Targeted HTTP API Sync

Use this loop for one or a few endpoint changes.

1. Refresh/read OAS and relevant refs.
2. List HTTP APIs and find existing records by `METHOD path`.
3. For each existing endpoint, `GET /http-apis/{apiId}` before updating; never update from the compact list item alone.
4. Inspect the record shape (`parameters`, `auth`, `securityScheme`, `requestBody`, `responses`, `responseExamples`, `folderId`, `moduleId`, `tags`, `advancedSettings`).
5. Build the plan and ask for approval with affected method/path/id list.
6. Apply the minimal write. For `PUT`, preserve the existing record shape and change only required fields: `name`, `description`, `operationId`, `parameters`, `requestBody`, `responses`, `responseExamples`, etc.
7. Verify by Open API re-fetch and MCP OAS refresh.

When creating a sibling endpoint, clone a similar API payload and preserve `folderId`, `moduleId`, `tags`, `advancedSettings`, `visibility`, `status`, and auth settings unless intentionally changing them. Strip server-managed fields (`id`, `projectId`, creator/editor IDs, created/updated timestamps) before `POST` or `PUT`.

### Record Shape Normalization

Apifox accepts more than one internal shape for the same concept, but not every shape exports correctly to OpenAPI. Before writing an endpoint, normalize the fields that affect OAS output:

- `parameters` should be the grouped object form: `{ "path": [], "query": [], "header": [], "cookie": [] }`. Array form can appear in old records and may cause path/query parameters to disappear from exported OAS.
- For JSON responses set both `contentType: "json"` and `mediaType: "application/json"`. Empty `mediaType` may export as `*/*` or otherwise display oddly.
- For no-body endpoints set `requestBody.type: "none"` and `required: false`.
- For JSON request endpoints set `requestBody.type: "application/json"`, `required: true`, and `jsonSchema` on the record.
- If a response is the project envelope (`{code,message,data}`), put the actual payload schema directly under `data`. Do not put an entire `EmptyResponse` envelope under `data`; for empty successes use `data: { "type": "null" }`.
- If examples were generated from old schemas, update or clear `responseExamples` in the same write. Stale examples survive otherwise.

### Temporary Node Script Pattern

For fragile writes, create a short ESM script in `/tmp` instead of hand-writing long curl commands. This avoids shell quoting mistakes and keeps the token out of stdout. The script should:

- Parse `projectId` and token from config in memory.
- Define one `request(path, init)` helper with required Apifox headers.
- List APIs and match by uppercase method + path.
- GET full records for every API that will be updated or cloned.
- Use `structuredClone`, then remove server-managed fields before writes.
- Print only `{id, method, path}` for changed records.

Use ESM syntax (`import fs from "node:fs"`) when using top-level `await`; do not mix `require()` and top-level `await`.

### Example: Sibling Endpoint + Existing Endpoint Update

When adding `POST /api/v1/ws-ticket` next to `GET /ws/v1/rooms/{room_id}`:

- Update the existing WebSocket endpoint by ID: change query parameter `token` to `ticket`, update description/example, and set success response to `101 Switching Protocols`.
- Create the ticket endpoint by cloning the WebSocket module record to preserve `folderId`, `moduleId`, and tags, then change method/path/name/operationId/auth/request/response fields.
- Refresh MCP OAS and read `/paths/_api_v1_ws-ticket.json` plus `/paths/_ws_v1_rooms_%7Broom_id%7D.json` to verify the exported spec, not just the write response.

## Native WebSocket API Sync

Use Apifox native WebSocket APIs for connection-time message protocols. OpenAPI/MCP path refs can document the HTTP upgrade handshake (`GET ... -> 101`), but they do not create an Apifox WebSocket debugging surface for client messages and server events.

Discovery and matching:

1. List existing native WS records with `GET /api/v1/projects/{projectId}/websocket-apis`.
2. Match by exact `path`, e.g. `/ws/v1/rooms/{room_id}`. If no record exists, create one with `POST /websocket-apis`; an empty POST creates an `Unnamed` record, so only use POST when you are prepared to immediately update or delete the created id.
3. For an existing record, `GET /websocket-apis/{apiId}` before `PUT`.
4. Preserve `folderId`, `moduleId`, `status`, `visibility`, `serverId`, `advancedSettings`, and tags unless intentionally changing them. Strip server-managed fields before `POST`/`PUT`.

Record fields that matter:

- `name`: human-readable WS interface name.
- `path`: WS path without scheme/host, such as `/ws/v1/rooms/{room_id}`.
- `parameters`: grouped object form with `path`, `query`, `header`, `cookie`; include ticket/query params used by the handshake.
- `requestBody`: client-to-server Message panel content. For JSON WS messages, use WS-style `type: "json"` rather than HTTP-style `type: "application/json"`, include a directly sendable sample in `data`/`example`/`raw`/`content`, and keep the schema in `jsonSchema`. `data` is important because Apifox's Message editor/cases often read the editable payload from `requestBody.data`, while HTTP API design examples may use `example`. For multiple client messages, use `oneOf` keyed by the `type` field and choose the safest default sample, usually `ping`.
- `description`: document connection URL (`ws://` and `wss://`), ticket acquisition flow, client messages, server-pushed event types, payload fields, and examples. Native WS records may not have a separate response schema surface for every server event, so the description is the durable place for server event contracts unless the API exposes a richer messages field in that project.

When the backend uses an event envelope such as `{ "type": "...", "payload": ... }`, define the message variants structurally, not only in prose:

- Put every known `type` into `requestBody.jsonSchema.oneOf`.
- For each variant, make `type` a single-value enum and define that variant's `payload` fields from the code DTOs.
- Include both directions when the native WS API has no stable server-event response field: client-send variants such as `ping`/`leave_room`, plus server-push variants such as `pong` and business events. State the direction in each variant description and in the API description.
- Keep `data`/`example`/`raw`/`content` as the safest client-send sample so the Apifox Message tab can be used directly.

For WebSocket endpoints that also appear as HTTP upgrade routes, keep both records aligned:

- HTTP API record: documents OpenAPI export, path/query parameters, `101 Switching Protocols`, and handshake failure responses such as `400 text/plain` or `401 text/plain`.
- Native WebSocket record: documents and enables Apifox WS debugging for messages after connection.

Verification for native WS:

- Re-fetch `GET /websocket-apis/{apiId}` and confirm `path`, `parameters`, `requestBody`, folder/module, and protocol description.
- Check the Apifox UI Message tab after refresh; if it still shows empty `Text`, the API write likely used an HTTP request-body shape instead of the native WS message shape.
- List `GET /websocket-apis` and confirm there are no leftover `Unnamed` or duplicate path records.
- Refresh MCP OAS only verifies the HTTP upgrade route; it will not prove the native WS record exists.

## Desired OpenAPI Reconcile

Use this when the user has, or wants to generate, a desired OpenAPI document from code or docs and then make Apifox match it.

Sources:

- Code-generated OpenAPI: run the repo's existing generator/export command if present. Do not invent a framework; inspect scripts/docs first.
- Docs-generated OpenAPI: convert the product/API docs into a temporary OpenAPI JSON with stable `operationId`, tags, request schemas, response examples, and paths.

### OpenAPI Document Sync Mode

Use this mode when the human says to sync Apifox from an OpenAPI file, generated Swagger/OpenAPI output, or a checked-in OpenAPI document.

Principles:

- Treat the desired OpenAPI document as authoritative for HTTP API records only.
- OpenAPI can document a WebSocket HTTP upgrade route (`GET ...` with `101 Switching Protocols`), but it cannot represent the Apifox native WebSocket debugging record. Preserve existing `/websocket-apis` records unless the human explicitly approves changing them.
- In prune/exact-sync mode, prune only HTTP APIs and folders by default. Never delete native WebSocket APIs just because they are absent from the desired OpenAPI document.
- If an OpenAPI path looks like a WebSocket handshake, update or create the HTTP API handshake record, then run **Native WebSocket API Sync** only for the matching native WS record.

Preflight:

1. Produce or read the desired spec and save it to `/tmp/<project>-desired-openapi.json`.
2. Validate it is OpenAPI 3.x or Swagger 2.0 JSON/YAML and normalize it to JSON before diffing.
3. Refresh/read the current Apifox OAS through MCP or cached `original.json`.
4. List current HTTP APIs and native WebSocket APIs. Keep these sets separate:
   - `httpApis`: records from `GET /http-apis`, matched by `METHOD path`.
   - `nativeWsApis`: records from `GET /websocket-apis`, matched by exact `path`.
5. Classify desired operations:
   - **HTTP**: normal OpenAPI operations.
   - **WS handshake**: usually `GET` with a `101` response, `ws`/`websocket` tags, or a `/ws/` path. These still write to HTTP APIs for OpenAPI export.
   - **Not native WS**: message protocols after upgrade; OpenAPI alone is insufficient for the Apifox WS Message tab.

Write strategy:

- For a small number of changed operations, prefer targeted `http-apis` create/update/delete so Apifox-only metadata is preserved.
- For a broad first-time sync or many changed endpoints, propose `POST /v1/projects/{projectId}/import-openapi` with the full desired spec string. Warn that import may create duplicate APIs or empty folders and include cleanup steps in the approval plan.
- After an import, immediately list HTTP APIs, remove duplicate `METHOD path` records only with approval, and delete empty import-created folders only with approval.
- Do not use OpenAPI import as proof of native WS sync. If the desired spec contains a WS handshake path, verify the HTTP API record through exported OAS, then separately verify or update the native WS record with `GET/PUT /websocket-apis/{apiId}`.

Approval plan format:

```text
Desired source: /tmp/<project>-desired-openapi.json
Write mode: targeted http-apis | import-openapi
HTTP create: METHOD path
HTTP update: METHOD path (apiId)
HTTP delete: METHOD path (apiId) [only if prune approved]
WS handshake HTTP records: METHOD path (apiId or create)
Native WS records: preserve | update path (apiId) | create path | delete path (requires explicit approval)
Schemas/examples: create/update/inline fallback list
Cleanup after import: duplicates/folders to inspect
```

Reconcile steps:

1. Save desired spec to `/tmp/<project>-desired-openapi.json`.
2. Refresh current Apifox OAS through MCP; if available, use `~/.apifox-mcp-server/project-{projectId}/original.json` for the full current spec.
3. Normalize both specs before diffing: compare by uppercase method + path, ignore cosmetic ordering, and resolve obvious `$ref` differences only when needed.
4. Build an action plan:
   - **create**: endpoint exists in desired but not current.
   - **update**: same method+path exists but summary, description, request body, responses, examples, tags, or auth changed.
   - **delete**: endpoint exists in current but not desired. Treat this as destructive; proceed only when the user explicitly asked to prune/sync exactly.
   - **schema update/create**: reusable schemas changed or new schemas appear. Prefer schema APIs when they work; otherwise inline schemas in affected HTTP APIs.
5. Separately list matching native WebSocket records and state whether they will be preserved, updated, created, or left untouched.
6. Show the action plan and ask for explicit human approval before any create, update, delete, or import.
7. Apply only the approved actions. Use targeted `http-apis` changes first; use full OpenAPI import only when many schemas/endpoints must be reconciled together and cleanup is approved.
8. Verify by refreshing MCP OAS and re-running the diff until no intended HTTP differences remain. Verify native WS records separately because MCP OAS does not prove they exist.

Matching rules:

- Primary identity: `METHOD path`.
- Secondary clue: `operationId`, useful for rename detection but not enough to overwrite a different path automatically.
- If a path changed, create the new endpoint first; delete the old endpoint only in prune mode.
- Preserve Apifox-only metadata such as `folderId`, `moduleId`, processors, auth inheritance, and advanced settings when updating an existing endpoint.
- Preserve native WebSocket APIs across OpenAPI document sync. Only modify a native WS record when the desired OpenAPI path is a matching handshake and the human approved a WS update/create, or when the task explicitly asks for native WebSocket protocol changes.

## Schema Sync

Reusable schemas are listed at `GET /data-schemas`. The response includes `id` (numeric), `name`, and `jsonSchema`.

**`$ref` format**: Inside Apifox http-api records and data-schema records, cross-references use numeric IDs — `"$ref": "#/definitions/276728368"` — not the string name form `#/components/schemas/Name` that appears in the cached OAS. Always use the numeric form when writing back to the API.

**If `PUT /data-schemas/{id}` redirects to `/help/index.html`**: the platform blocks this endpoint. Do not claim success. Use the inline-schema fallback:

1. `GET /http-apis/{apiId}` — read the full endpoint record.
2. Locate every affected `requestBody.jsonSchema` and `responses[n].jsonSchema`; response-only fallback is not enough when request DTOs changed.
3. Add or change fields directly in those JSON objects. Use `"$ref": "#/definitions/{numericId}"` when referencing other schemas, or inline the schema completely when the exported OAS must be independent of stale reusable components.
4. `PUT /http-apis/{apiId}` — write back the entire record (preserve all other fields).
5. Verify by re-fetching and checking that new properties appear.
6. Refresh MCP OAS and read the path refs that consume the schema. If they still show old component fields, inline the schema on those endpoint records and clear stale examples.

When this fallback is used, the reusable component files under `/components/schemas/*.json` may remain stale because Apifox blocked component writes. Do not report the endpoint sync as complete until the exported path refs are correct. In the final report, explicitly separate "endpoint OAS aligned" from "reusable component remains stale due to blocked data-schema API".

**Response examples**: After adding fields to a response schema, check whether existing `responseExamples` on that endpoint are now stale (missing the new fields). Update them in the same PUT call by editing the `responseExamples[n].data` JSON string.

## OpenAPI Import Warning

`POST /v1/projects/{projectId}/import-openapi` expects:

```json
{ "input": "<OpenAPI JSON or YAML string>", "options": {} }
```

Not:

```json
{ "input": { "type": "data", "data": "..." } }
```

Full import can create duplicate APIs and empty folders. Only use it when broad reconciliation is intended and the human explicitly approves the import plan; immediately clean up afterwards.

OpenAPI import only affects exported HTTP-style API documentation. It does not replace **Native WebSocket API Sync**. After importing an OpenAPI document that contains WebSocket handshake paths, still list `/websocket-apis` and confirm matching native WS records were preserved or updated according to the approved plan.

## Cleanup Checklist

After import or bulk sync:

1. List HTTP APIs and group by `METHOD path`; delete duplicate new IDs.
2. List `api-folders`; delete empty folders created by import.
3. Refresh MCP OAS.
4. Read changed path refs and schema index.
5. Confirm old endpoints/fields are gone from active APIs.

For code-vs-Apifox reconciliation, also compare the final HTTP API count and `METHOD path` set against the code route set. Treat extra Apifox-only endpoints as prune candidates only when the human approved pruning or explicitly asked to align exactly to code.

For OpenAPI document sync, native WebSocket APIs are not cleanup candidates unless the human explicitly approved native WS deletion by `websocket-api` id/path.

Avoid `seq` for large Apifox IDs on macOS/zsh because it may output scientific notation. Use explicit IDs or Node.

## Verification

Verification must prove both the Apifox internal record and exported OAS are correct.

- Re-fetch changed records with `GET /http-apis/{apiId}` and inspect changed fields.
- Refresh MCP OAS after writes.
- Read changed `$ref` resources and confirm exported OpenAPI has the intended paths, parameters, response codes, schemas, security, and examples.
- For endpoint contracts, trust exported path refs over reusable component refs. Components can remain stale when `PUT /data-schemas/{id}` is blocked; path refs must still be correct via inline schemas.
- If you created an endpoint, list APIs again and confirm no duplicate `METHOD path` records appeared.
- If you deleted endpoints, list APIs again and confirm they are absent; delete the now-empty folder when appropriate.
- If aligning to code, compare final Apifox `METHOD path` set and count to the code route set.
- If syncing from OpenAPI, compare final Apifox exported HTTP `METHOD path` set against the desired OpenAPI document and separately confirm native WebSocket records were preserved or updated as planned.
- Report remaining drift separately from successful changes.

## Verification Snippets

Schema field presence (after updating an http-api response schema):

```bash
curl -s "https://api.apifox.com/api/v1/projects/{projectId}/http-apis/{apiId}" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Apifox-Api-Version: 2024-03-28" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
resp200 = next(r for r in d['responses'] if r['code']==200)
print(list(resp200['jsonSchema']['properties'].keys()))
"
```

Duplicate APIs:

```bash
rtk node -e 'const fs=require("fs");const apis=JSON.parse(fs.readFileSync("/tmp/apifox-http-apis.json","utf8")).data;const m=new Map();for(const a of apis){const k=`${a.method.toUpperCase()} ${a.path}`;(m.get(k)||m.set(k,[]).get(k)).push(a.id)}for(const [k,v] of m)if(v.length>1)console.log(k,v.join(","))'
```

Folder leftovers:

```bash
rtk node -e 'const fs=require("fs");const f=JSON.parse(fs.readFileSync("/tmp/apifox-folders.json","utf8")).data;for(const x of f)console.log(x.id,x.name,"parent",x.parentId,"children",x.children.length,"created",x.createdAt)'
```

## Common Mistakes

- Assuming Apifox MCP can write.
- Using `curl -L` on write endpoints and silently landing on HTML help pages.
- Treating a successful Open API write response as proof the exported OAS changed.
- Documenting WebSocket message protocols only on an HTTP `GET ... -> 101` record and forgetting to create/update the native `/websocket-apis` record used by Apifox WS debugging.
- Probing `POST /websocket-apis` with `{}` and leaving the resulting `Unnamed` record behind.
- Expecting MCP/OpenAPI refresh to verify native WebSocket records; it only verifies exported HTTP paths.
- Updating Apifox internal records in array `parameters` form and missing that exported OAS lost path/query parameters.
- Setting `contentType: "json"` but leaving response `mediaType` empty.
- Relying on reusable schema updates after `PUT /data-schemas/{id}` is blocked; inline the affected endpoint schemas instead.
- Updating only response schemas when the drift is in request DTOs too.
- Importing a full OAS without duplicate API and empty folder cleanup.
- Treating OpenAPI import as native WebSocket sync. It only covers HTTP/exported OAS records; `/websocket-apis` must be handled separately.
- Deleting native WebSocket API records during prune/exact OpenAPI sync without a separate explicit approval.
- Performing create/update/delete/import without explicit human approval.
- Reporting success before re-fetching the changed record and refreshing MCP OAS.
- Printing the Personal Access Token, including by running a grep command whose output is shown in chat.
- Using `#/components/schemas/Name` format in http-api records — Apifox internal API requires `#/definitions/{numericId}`.
- Blocking on MCP unavailability — the cached OAS at `~/.apifox-mcp-server/project-{projectId}/original.json` is sufficient for diffing.
- Forgetting to update `responseExamples` after adding fields to a response schema.
