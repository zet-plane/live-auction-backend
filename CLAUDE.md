# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run server (requires config.yaml at project root)
go run main.go server -c config.yaml

# Generate a blank config file
go run main.go config -p config.yaml

# Run all tests
go test ./...

# Run a single package's tests
go test ./internal/app/item/service/...

# Run a specific test
go test ./internal/app/item/service/... -run TestCreateItemRequiresMerchantAndCreatesDraftItemWithRule
```

## Architecture

### Project domain

Live-auction is a real-time auction backend for live-stream e-commerce (高价值非标品 such as jewelry and luxury goods). Core domain concepts: **AuctionItem** (the thing being auctioned), **AuctionRule** (its bidding configuration), and **AuctionPolicy** (platform-level snipe-protection rules).

### Module system

Business logic is split into self-contained modules under `internal/app/<module>/`. Each module implements the `app.Module` interface (`internal/app/app.go`) with three lifecycle hooks:

- `PreInit(engine)` — runs before any module loads; used for DB migrations via `store.AutoMigrate()`
- `Load(engine)` — wires dependencies and registers routes; reads `AuctionPolicy` overrides from config here
- `Stop(wg, ctx)` — graceful shutdown

Modules are registered via `init()` functions in `internal/app/appInitialize/` and iterated in `cmd/server/server.go`. To add a module: create `internal/app/<module>/init.go` implementing `app.Module`, then add `appInitialize/<module>.go` appending to the `apps` slice.

### Module internal layout

Every module follows the same sub-package split:

```
model/    — GORM structs, type constants (no logic)
dao/      — Store interface + GormStore implementation
dto/      — request/response structs, input types, DTO constructors
service/  — business logic (depends on dao.Store interface, not GormStore)
handler/  — flamego handlers; package-level var svc set via Init()
router/   — route registration wired to handler functions
init.go   — Module implementation
```

### DTO pattern

Request structs expose an `.Input()` method that converts to the service-layer input type. This keeps HTTP binding tags and validation annotations out of service code.

```go
// dto layer
type CreateItemRequest struct {
    Title string `json:"title" binding:"required,min=1,max=128"`
    ...
}
func (r CreateItemRequest) Input() CreateItemInput { ... }

// handler layer
result, err := svc.CreateItem(current, body.Input())
```

### Dependency injection

This project uses [flamego](https://flamego.dev) as the web framework with a built-in DI container. Middleware calls `c.Map(value)` to register a value by its concrete type; downstream handlers receive it as a function parameter. The auth middleware maps the authenticated `*model.User` this way — `verify` must return a concrete pointer so flamego can resolve it by `reflect.TypeOf`.

Handler functions are **not closures** — they are plain functions reading from a package-level `var svc *service.Service` set via `handler.Init(svc)` during module load.

### Error handling

All service-boundary errors must be `*errorx.CodeError` (defined in `pkg/errorx/errorx.go`). Handlers call `response.Error(r, err)` which uses `errors.As` to extract the HTTP status and code, falling back to 500 for unrecognised errors. Never call `response.Fail` directly in handlers.

### Request binding

Routes that accept a JSON body pass `binding.JSON(dto.XxxRequest{})` as middleware. The bound struct is injected by flamego; the handler receives it alongside `binding.Errors`. Always guard with `web.BindingErrors(r, errs)` as the first check in the handler.

### kernel.Engine

The engine (`internal/core/kernel/kernel.go`) is the shared carrier passed to every module:

- `Flame` — flamego instance (routes registered here)
- `DB` — GORM connection (MySQL)
- `Cache` — `*redis.Client`
- `Config` — `*config.Config` (alias for `config.GlobalConfig`)
- `Cron` — robfig/cron instance for scheduled jobs
- `Context` / `Cancel` — root cancellation context

### Config

Config is loaded from `config.yaml` at startup via viper and **live-reloaded on file changes**. `config.GetConfig()` returns the singleton. Key sections: `http`, `database`, `redis`, `auth` (token secret + TTL), `auction` (snipe-protection policy). Helper methods parse duration strings: `AuthTokenTTL()`, `DatabaseConnMaxLifetime()`.

See `config.yaml.example` for all fields and their defaults.

### Item state machine

`AuctionItem.Status` follows a strict linear transition enforced in `service.transition()`:

```
draft → published → ongoing → ended
                 ↘ cancelled (from published or ongoing)
```

Only the owning merchant (checked via `item.MerchantID == current.ID`) can mutate status. Updates are rejected if the item is not in the expected `from` state (`errorx.ErrInvalidRequest`).

### AuctionPolicy (snipe protection)

`dto.AuctionPolicy` defines platform-wide snipe-protection parameters with defaults (`ExtendTriggerSec=30`, `AutoExtendSec=10`, `MaxExtendCount=6`, `MaxTotalExtendSec=300`). These are loaded in the item module's `Load()` from `engine.Config.Auction`, overriding defaults only when the config value is non-zero.

### User identities

`model.UserIdentity` is either `IdentityUser` or `IdentityMerchant`. Service methods that require merchant access call the local `isMerchant(current)` helper and return `errorx.ErrUnauthorized` if the check fails. Merchants manage items; the identity can be updated via `PUT /api/v1/users/me`.

### Authentication

Tokens are custom HMAC-SHA256 JWTs (`internal/app/user/service/token.go`) — no third-party JWT library. The item module's router wires auth using `userhandler.AuthenticateToken` from the user module (cross-module dependency at router registration time only).

### IDs

All entity IDs are generated with `pkg/snowflake` (`"<prefix>_" + snowflake.MakeUUID()`), e.g. `"item_"`, `"rule_"`, `"user_"`.

### Amount convention

All monetary amounts are `int64` in fen (分). Never use `float64` for money.

### Testing strategy

Service-layer tests use a `fakeStore` struct that implements `dao.Store` — no real DB or HTTP server needed. See `internal/app/item/service/service_test.go` for the pattern: `newFakeStore()` with in-memory maps, then exercise service methods directly. The service exposes a `now func() time.Time` field for time injection in tests.

### Agent testing

When doing any testing task that uses `docs/agent-testing/`, **always read `docs/agent-testing/README.md` first**. Do not read other files in `docs/agent-testing/` until the README routes you there. The directory uses progressive disclosure: the README is the entry point, then the agent reads only the specific guide, module contract, flow contract, environment guide, template, or report guide needed for the current task.

- Execute tests by following the README route to `guides/runner.md`, then the relevant `modules/<module>.md` or `flows/<flow>.md`.
- Prepare environment, connect DB/Redis, start services, or create test data only after the README or runner routes you to `guides/environment.md`.
- Generate missing module contracts only after the README routes you to `guides/module-generator.md`.
- Read `templates/module.md` only when generating or updating a module contract.
- Read `reports/README.md` only when writing, updating, or validating a test report.
