# AGENTS.md

This file is the Codex entry point for `live-auction-backend`.

Use it to initialize project context before making code, test, documentation, or architecture changes. Keep this file short and route to deeper documents only when needed.

## First Steps

1. Use `rtk` for every shell command.
2. Check the current task type.
3. Read only the deeper docs needed for that task.
4. Do not assume `CLAUDE.md` has been read by Codex; read it explicitly when broader architecture context is useful.

## Shell Commands

Always prefix shell commands with `rtk`.

Examples:

```bash
rtk git status
rtk go test ./...
rtk go run main.go server -c config.yaml
```

## Project Shape

This is a Go backend for live-auction e-commerce. The core domain is real-time auctioning of high-value non-standard goods.

Key concepts:

- `AuctionItem`: the item being auctioned.
- `AuctionRule`: bidding configuration for an item.
- `AuctionPolicy`: platform-level anti-sniping policy.
- User identities: `user` and `merchant`.

Main code layout:

```text
cmd/                 application commands
config/              config loading
internal/app/        business modules
internal/core/       kernel and shared runtime
internal/middleware/ HTTP middleware and response helpers
pkg/                 reusable packages
docs/                product, design, and testing docs
```

Business modules live under:

```text
internal/app/<module>/
```

Typical module structure:

```text
model/    GORM structs and constants
dao/      Store interface and persistence implementation
dto/      request/response/input types
service/  business logic, depending on dao.Store
handler/  Flamego handlers
router/   route registration
init.go   module lifecycle wiring
```

For deeper architecture details, read `CLAUDE.md`.

## Development Rules

- Prefer existing module patterns over new abstractions.
- Keep service logic behind `dao.Store` interfaces so it can be unit-tested with fake stores.
- Handlers should return through `response.OK` or `response.Error`.
- Service-boundary errors should use `pkg/errorx`.
- Routes that bind JSON should guard binding errors with `web.BindingErrors`.
- Do not modify unrelated modules while working on a focused task.

## Testing Rules

Local code unit tests:

- Must not connect to MySQL, Redis, HTTP services, WebSocket, or external systems.
- Must use mock/fake stores, in-process data, fixed config, and fake time/ID where needed.
- Follow the existing `fakeStore` style in service tests.

Agent-driven interface, integration, end-to-end, concurrency, and state-consistency tests:

- May connect to online or online-equivalent real dependencies only when routed by `docs/agent-testing/README.md` and `docs/agent-testing/environment.md`.
- May operate only on data created for the current test batch.
- Must record evidence and cleanup results.
- Must not write online addresses, credentials, passwords, or reusable tokens into reports.

## Agent Testing Docs

Any task that uses `docs/agent-testing/` must start here:

```text
docs/agent-testing/README.md
```

Do not read other files in `docs/agent-testing/` until the README routes you there. The directory uses progressive disclosure.

Common routes:

- Execute tests: README -> `agent-runner-guide.md` -> relevant `modules/<module>.md` or `flows/<flow>.md`.
- Prepare environment, connect DB/Redis, start services, or create test data: README/runner -> `environment.md`.
- Generate missing module contracts: README -> `module-generator-guide.md` -> `template.md`.
- Write or validate reports: README/runner -> `reports/README.md`.

## Useful Commands

```bash
rtk go test ./...
rtk go test ./internal/app/<module>/...
rtk go build ./...
rtk go run main.go server -c config.yaml
```

Use `config.yaml.example` as the local config reference. Local MySQL and Redis defaults are in `docker-compose.yml`.
