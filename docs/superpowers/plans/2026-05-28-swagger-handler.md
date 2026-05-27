# Swagger Handler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Swagger documentation generation and UI routing for the live-auction backend handlers.

**Architecture:** Handler comments feed `swaggo/swag`; generated docs live under `internal/swaggerdocs`; `cmd/server` mounts Swagger UI through a small Flamego route helper.

**Tech Stack:** Go, Flamego, swaggo/swag, swaggo/http-swagger.

---

### Task 1: Swagger UI Route

**Files:**
- Create: `cmd/server/swagger_test.go`
- Create: `cmd/server/swagger.go`
- Modify: `cmd/server/server.go`

- [ ] Write a failing test that creates a Flamego instance, calls `registerSwaggerRoutes`, requests `/swagger/index.html`, and expects a non-404 response.
- [ ] Run `rtk go test ./cmd/server -run TestRegisterSwaggerRoutesServesIndex -count=1` and confirm it fails because `registerSwaggerRoutes` does not exist.
- [ ] Add `registerSwaggerRoutes(f *flamego.Flame)` using `github.com/swaggo/http-swagger/v2`.
- [ ] Call `registerSwaggerRoutes(f)` from `buildEngine`.
- [ ] Run the targeted test and confirm it passes.

### Task 2: Swagger Metadata and Annotations

**Files:**
- Modify: `main.go`
- Modify handler files under `internal/app/*/handler/*.go`

- [ ] Add global Swagger metadata to `main.go`, including base path `/api/v1` and bearer auth.
- [ ] Add endpoint annotations beside existing handler functions, using existing DTO and `response.Body` types.
- [ ] Mark authenticated endpoints with `@Security BearerAuth`.

### Task 3: Generate and Verify

**Files:**
- Create generated files under `internal/swaggerdocs`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] Add the `github.com/swaggo/http-swagger/v2` dependency.
- [ ] Generate docs with `rtk swag init -g main.go --parseInternal --parseDependency -o internal/swaggerdocs`.
- [ ] Run `rtk go test ./cmd/server -run TestRegisterSwaggerRoutesServesIndex -count=1`.
- [ ] Run `rtk go test ./...`.
