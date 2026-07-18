# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with OAuth and round-robin load balancing.

## Repository
- **Origin (this fork, push/release):** https://github.com/josephcy95/CLIProxyAPI (`origin`)
- **Upstream:** https://github.com/router-for-me/CLIProxyAPI (`upstream`)
- Tags / releases are published on **josephcy95** only. Use `gh -R josephcy95/CLIProxyAPI` when default remote context is wrong.
- Docker image: `ghcr.io/josephcy95/cli-proxy-api` (`:latest` + version on `v*` tag workflows).

## Fork policies

### Upstream sync
- Prefer full merge of upstream release tags / `upstream/main` so the fork is not left “N commits behind”.
- On conflicts: keep fork features; take the more robust upstream fix; combine when both apply.
- After merge: `gofmt`, `go mod tidy` if needed, compile, run targeted tests, then ship only per ship policy.

### Fork features to preserve (do not silently drop)
- xAI auto-disable after surviving 401 (and permission-denied 403 path) when config enabled
- Codex auto-disable / exhaustion handling and related failure policy
- Usage monitoring: store full client `api_key` (+ hash), filter/options/search
- Distinct auth scheduler behavior where fork intentionally differs (e.g. `auth_unavailable` when candidates exist)
- Single data root (`CLIPROXY_DATA_DIR`, default `/data`): config/auths/logs/plugins/usage.db under one mount
- Primary Chinese README / fork README choices; do not reintroduce removed promo assets without ask

### Ship policy
See workspace parent `../AGENTS.md` if present. In short:
- **Minor** → no push/tag/release unless asked (commit only if needed).
- **Medium** → commit when done; offer push/release.
- **Meaningful** (upstream merge, user-facing/deploy-blocking, multi-file feature) → push + tag + release + docker tags.
- Version tags: next fork `v7.2.x` (may exceed upstream numbers on origin only).

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o cli-proxy-api ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server && rm test-output # Verify compile (REQUIRED after changes)
```
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`, `--no-browser`, `--oauth-callback-port <port>`

## Config
- Data root: `CLIPROXY_DATA_DIR` / `CLI_PROXY_DATA_DIR` (default `/data`)
- Default config: `$DATA/config.yaml` (template: `config.example.yaml`; Docker entrypoint seeds if missing)
- `.env` loaded from data root first, then working directory
- Auth / logs / plugins / usage: `$DATA/auths`, `$DATA/logs`, `$DATA/plugins`, `$DATA/usage.db`
- Docker: single volume host→`/data` (do not mount over `/CLIProxyAPI`)
- Storage backends: file-based default; optional Postgres/git/object store (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`)

## Architecture
- `cmd/server/` — Server entrypoint
- `internal/api/` — Gin HTTP API (routes, middleware, modules)
- `internal/api/modules/amp/` — Amp integration (Amp-style routes + reverse proxy)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` — Usage and token accounting
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `test/` — Cross-module integration tests

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don’t add new non-English comments)
- For user-visible strings, keep the existing language used in that file/area
- New Markdown docs should be in English unless the file is explicitly language-specific (e.g. `README_CN.md`)
- As a rule, do not make standalone changes to `internal/translator/`. You may modify it only as part of broader changes elsewhere.
- If a task requires changing only `internal/translator/`, run `gh repo view --json viewerPermission -q .viewerPermission` to confirm you have `WRITE`, `MAINTAIN`, or `ADMIN`. If you do, you may proceed; otherwise, file a GitHub issue including the goal, rationale, and the intended implementation code, then stop further work.
- `internal/runtime/executor/` should contain executors and their unit tests only. Place any helper/supporting files under `internal/runtime/executor/helps/`.
- Follow `gofmt`; keep imports goimports-style; wrap errors with context where helpful
- Do not use `log.Fatal`/`log.Fatalf` (terminates the process); prefer returning errors and logging via logrus
- Shadowed variables: use method suffix (`errStart := server.Start()`)
- Wrap defer errors: `defer func() { if err := f.Close(); err != nil { log.Errorf(...) } }()`
- Use logrus structured logging; avoid leaking secrets/tokens in logs
- Avoid panics in HTTP handlers; prefer logged errors and meaningful HTTP status codes
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, the management APICall timeout in `internal/api/handlers/management/api_tools.go`, and the `cmd/fetch_antigravity_models` utility timeouts
