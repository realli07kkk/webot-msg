# Repository Guidelines

## Project Structure & Module Organization

This is a small Go CLI/API service. The executable entry point lives in
`cmd/webot-msg/main.go`. Internal packages are grouped by responsibility:
`internal/app` coordinates startup, login, monitoring, and shutdown;
`internal/api` exposes HTTP bot actions; `internal/config` owns persistent
configuration; `internal/console` handles interactive commands; and
`internal/ilink` wraps iLink HTTP calls. Runtime credentials are written to
`config/auth.json`, which is intentionally ignored by Git.

## CodeStable Documentation

This repository uses CodeStable under `.codestable/`. AI agents should read
`.codestable/attention.md` before CodeStable work, then consult
`.codestable/requirements/` for user-facing capabilities and
`.codestable/architecture/` for the current system map. Use
`.codestable/features/`, `.codestable/issues/`, and `.codestable/roadmap/` for
future feature, bug, and planning workflows. Shared CodeStable conventions and
scripts live in `.codestable/reference/` and `.codestable/tools/`.

## Build, Test, and Development Commands

- `go run ./cmd/webot-msg -port 26322`: run the service locally with the
  default API port.
- `go build -o bin/webot-msg ./cmd/webot-msg`: build a local binary under
  `bin/`.
- `go test ./...`: run all package tests.
- `go vet ./...`: run Go static checks before opening a PR.
- `go fmt ./...`: format all Go files using the standard formatter.

The module targets Go `1.26.1`; keep local tooling aligned with `go.mod`.

## Coding Style & Naming Conventions

Use idiomatic Go and standard formatting. Keep packages focused and avoid
exporting identifiers unless they are used across packages. Prefer direct,
descriptive names such as `Store`, `UserConfig`, `SendMessage`, and
`handleBotAction`. Return wrapped errors with context at package boundaries
using `fmt.Errorf("operation failed: %w", err)`.

## Testing Guidelines

Place tests next to the code they cover using Go's standard `*_test.go`
pattern, for example `internal/config/store_test.go`. Prefer table-driven
tests for validation logic and API handlers. Use `httptest` for HTTP behavior
and inject fake clients or generators where external network calls or token
generation would make tests flaky. Run `go test ./...` before committing.

## Commit & Pull Request Guidelines

The history currently contains only `Initial commit`, so no strict convention is
established. Use short, imperative commit messages such as `Add bot config
tests` or `Handle invalid API tokens`. PRs should describe the user-visible
change, list validation commands run, link related issues when available, and
include request/response examples for API changes.

## Security & Configuration Tips

Never commit `config/auth.json`, `.env`, API tokens, QR login credentials, or
captured bot tokens. Keep new secrets covered by `.gitignore`. When adding logs,
avoid printing `BotToken`, `APIToken`, `ContextToken`, or full request bodies
that may contain message content.
