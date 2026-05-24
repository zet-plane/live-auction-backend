# Design: Agent Testing Go Runner Template

Date: 2026-05-24  
Status: Approved

## Problem

The current `docs/agent-testing/` system constrains agent behavior via Markdown contracts but leaves evidence collection entirely to the agent. Agents construct curl commands and SQL queries ad-hoc, producing verification evidence that is inconsistent, hard to reproduce, and sometimes incorrect. This is the primary quality bottleneck.

## Goal

Add a code-based evidence collection layer to the agent-testing system. Agents executing interface contract, integration, or state-consistency tests should use a structured Go runner instead of ad-hoc curl commands. The runner produces machine-readable, structured output that maps directly to the test report format.

## Scope

- Two new files: `docs/agent-testing/go-runner-guide.md` and `docs/agent-testing/runner-template.go`
- Small updates to `docs/agent-testing/README.md` and `docs/agent-testing/agent-runner-guide.md`
- No changes to module contracts (`modules/*.md`), flow contracts, or report format
- The template does not enter the repository at runtime — agents copy it to `/tmp`, use it, and discard it

## Architecture

```
docs/agent-testing/
├── README.md              ← routing table: add one row for go-runner-guide.md
├── go-runner-guide.md     ← when to use, setup steps, output format (NEW)
├── runner-template.go     ← standalone Go template agent copies to /tmp (NEW)
├── agent-runner-guide.md  ← add one reference in the dependency strategy section
└── ... (unchanged)
```

### Responsibility boundaries

| File | Type | Responsibility |
|---|---|---|
| `go-runner-guide.md` | Guide (prose) | When to choose Go runner, four-step setup, output format contract |
| `runner-template.go` | Code template | Executable skeleton agent copies to `/tmp/agent-runner-<batch>/main.go` |
| `README.md` | Routing entry | New row: interface contract / integration / state-consistency → `go-runner-guide.md` |
| `agent-runner-guide.md` | Execution guide | One-line note in dependency strategy: prefer Go runner for interface/integration tests |

The Go runner does not replace module contracts. Contracts define test boundaries; the runner is only an evidence-collection tool.

## go-runner-guide.md Structure

1. **When to use Go runner**
   - Interface contract tests, module integration tests, state-consistency tests
   - Any scenario requiring simultaneous verification of HTTP response + DB state + Redis state
   - Concurrency tests needing structured multi-goroutine results
   - Not for unit tests (use `go test` directly)

2. **Four-step setup**
   ```
   1. Read runner-template.go, copy to /tmp/agent-runner-<batch>/main.go
   2. Create go.mod in the same directory (content provided in this guide, pinned deps)
   3. Fill CONFIG: baseURL, batchID, redisAddr; set TEST_DSN env var
   4. Fill scenarios(), run: go run main.go
   ```

3. **Scenario authoring rules**
   - Each scenario is a `func() Result` appended to the `scenarios()` slice
   - Shared state between sequential scenarios (e.g., item_id created in step 1 used in step 2) passes via package-level variables
   - Concurrency scenarios use the provided `runConcurrent(n, fn)` helper

4. **Output format contract (agents must not alter)**
   ```
   === CASE: <name>
     REQUEST:  <method> <path> <body summary>
     RESPONSE: <status> <key fields>
     DB:       <query and result summary>
     REDIS:    <key + value summary> (N/A if not applicable)
     RESULT:   PASS / FAIL — <reason>

   === SUMMARY
     PASS: n  FAIL: m  SKIP: k
     BATCH_ID: <batchID>

   === CLEANUP
     <each cleanup action and result>
   ```

5. **Cleanup contract**
   - `cleanup()` is deferred at the start of `main`; runs even if a scenario panics
   - Only deletes rows/keys with the batchID prefix
   - Cleanup output is required evidence in the test report

6. **Connection to the report system**
   - Runner stdout is the raw evidence for the report's "验证证据" table
   - SUMMARY maps to the report's "通过项 / 失败项"
   - CLEANUP maps to the report's "测试数据清理结果"

## runner-template.go Design

Standalone `package main`. External dependencies only:

```
github.com/go-sql-driver/mysql  v1.x  — MySQL driver
github.com/redis/go-redis/v9    v9.x  — Redis client
```

### Five sections

**CONFIG (agent fills)**
```go
const (
    baseURL   = "http://127.0.0.1:8080"
    batchID   = "agent_<module>_<YYYYMMDDHHMMSS>"
    redisAddr = "127.0.0.1:6379"
)
// DSN read from os.Getenv("TEST_DSN") — credentials never written to file
```

**TYPES (no changes)**
```go
type Result struct {
    Name, Request, Response, DB, Redis, Reason string
    Pass bool
}
```

**SCENARIOS (agent fills)**
```go
func scenarios() []func() Result { ... }
```

**HELPERS (no changes)**
- `httpDo(method, path string, body any, token string) (int, map[string]any)`
- `dbRows(query string, args ...any) []map[string]string`
- `redisGet(key string) string`
- `redisHGetAll(key string) map[string]string`
- `redisZMembers(key string) []string`
- `runConcurrent(n int, fn func() Result) []Result`

**MAIN (no changes)**
1. `defer cleanup()`
2. Run `scenarios()` sequentially, collect `[]Result`
3. Print each result in the specified format
4. Print SUMMARY + CLEANUP block

### Key decisions

- DSN from `os.Getenv("TEST_DSN")` — no credentials in the file, consistent with existing prohibition
- `defer cleanup()` at top of `main` — guaranteed execution even on panic
- HTTP helper returns `map[string]any` — no dependency on project DTO types, agent asserts fields by name
- `runConcurrent` returns `[]Result` with one entry per goroutine — agent prints them in the CASE block

## README Routing Change

Add one row to the second-layer routing table:

| User intent | Next read |
|---|---|
| Running interface contract, integration, or state-consistency tests and needs structured evidence | `go-runner-guide.md` |

## Constraints Preserved

- Unit tests continue to use `go test` with fake stores — Go runner is not used for unit tests
- Agents still read module contracts before writing scenarios — the runner does not encode business rules
- No credentials, production addresses, or real tokens appear in the template
- Batch ID prefix rules from existing contracts apply to all data created by the runner
