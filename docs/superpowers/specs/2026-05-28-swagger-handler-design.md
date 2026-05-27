# Swagger Handler Design

## Goal

Expose generated Swagger documentation for the HTTP API and make handler annotations the source of truth for endpoint documentation.

## Design

Use `swaggo/swag` annotations on existing handler functions and global API metadata on `main.go`. Generated files live in `internal/swaggerdocs` so the server can import them without exposing a new public package.

Mount Swagger UI at `/swagger/index.html` through a small server helper that adapts `github.com/swaggo/http-swagger/v2` to Flamego. Keep API routes unchanged.

## Scope

This change adds the Swagger runtime wiring, generation command support, and representative annotations for existing handlers. Future endpoint changes should update the adjacent handler annotation in the same commit as the route behavior.

## Verification

Run targeted server tests to verify the Swagger route is mounted, run `swag init -g main.go --parseInternal --parseDependency -o internal/swaggerdocs`, then run `go test ./...`.
