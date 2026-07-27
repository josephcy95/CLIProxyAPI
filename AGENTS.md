# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex/Qoder compatible APIs with OAuth and round-robin load balancing.

## Repository
- **Origin (this fork, push/release):** https://github.com/josephcy95/CLIProxyAPI (`origin`)
- **Upstream:** https://github.com/router-for-me/CLIProxyAPI (`upstream`)
- Tags / releases are published on **josephcy95** only. Use `gh -R josephcy95/CLIProxyAPI` when default remote context is wrong.
- Docker image: `ghcr.io/josephcy95/cli-proxy-api` (`:latest` + version on `v*` tag workflows).
- Workspace sibling UI: `../cpa-management-center-forked` (Management Center). Parent workspace notes: `../AGENTS.md`.

## Fork policies

### Upstream sync
- Prefer full merge of upstream release tags / `upstream/main` so the fork is not left “N commits behind”.
- On conflicts: keep fork features; take the more robust upstream fix; combine when both apply.
- After merge: `gofmt`, `go mod tidy` if needed, compile, run targeted tests, then ship only per ship policy.
- **File splits are high-risk.** Upstream regularly splits large files (`service.go`, `conductor.go`, `config.go`, `server.go`, `auth_files.go`, provider executors) into `*_topic.go`. A clean compile does **not** mean fork logic survived — switch cases, route tables, and call sites can disappear silently.
- After any upstream merge that touches those areas, diff pre-merge vs HEAD for:
  - management routes (`mgmt.GET/PUT/...`)
  - provider switch cases in `registerExecutorForAuth` / `registerModelsForAuth*`
  - `baselineExecutorAuths` provider list
  - scheduler selection filters (private-instructions, free-auth, etc.)
  - `usagestore.Configure` call sites
  - auth-file metadata sync hooks (`syncAuthFileMetadataFields`)

### Dedicated fork files (prefer these so future splits cannot drop them)
| File | Purpose |
|---|---|
| `internal/config/fork_failure_policy.go` | xAI/Codex failure policy + Codex private-instructions config types |
| `sdk/cliproxy/auth/conductor_fork_failure_policy.go` | auto-disable, private-instructions policy helpers, exhaustion counters |
| `sdk/api/handlers/handlers_private_instructions.go` | request private-instructions marker injection |
| `internal/runtime/executor/codex_executor_fork_instructions.go` | apply configured Codex instructions |
| `internal/api/handlers/management/auth_files_fork_enrichment.go` | Codex plan / private-instructions / xAI status enrichment |
| `internal/api/handlers/management/auth_files_qoder_oauth.go` | Qoder + Qoder CN device-login management handlers |

### Fork features to preserve (do not silently drop)
- **Qoder CN (`qodercn`) and Qoder international (`qoder`)** device-login providers:
  - executor registration (`NewQoderCNExecutor` / `NewQoderExecutor`) in `sdk/cliproxy/service_executors.go`
  - model registration (`FetchQoderCNModels` / `FetchQoderIntlModels`) in `sdk/cliproxy/service_models.go`
  - `baselineExecutorAuths` must list both providers
  - auth load paths: `sdk/auth/filestore.go`, `internal/watcher/synthesizer/file.go`
  - management routes `/qodercn-auth-url`, `/qoder-auth-url`
  - CLI flags `--qodercn-login`, `--qoder-login`
- **Codex private instructions**: policy must run in **all** auth selection paths (`pickViaBuiltinScheduler`, `pickNext`, `pickNextLegacy`, `pickNextMixed`, `pickNextMixedLegacy`). Helpers live in `conductor_fork_failure_policy.go`. PATCH auth-file must call `syncAuthFileAllowPrivateInstructionsAttribute`.
- xAI auto-disable after surviving 401 (and permission-denied 403) when config enabled
- Codex auto-disable / exhaustion handling (`trackCodexExhaustionCounter` / `disableCodexAuth`)
- Usage monitoring: `usage-store-path` / `usage-retention-days`, `usagestore.Configure` on startup **and** reload, routes `/usage-events`, `/usage-summary`, `/usage-filter-options`, `/usage-account-stats`, `/usage-api-key-stats`, model-prices family
- Distinct auth scheduler behavior (`auth_unavailable` when candidates exist)
- Model context overrides (`model-context-overrides` management API + registry apply path)
- Single data root (`CLIPROXY_DATA_DIR`, default `/data`): config/auths/logs/plugins/usage.db under one mount
- Primary Chinese README / fork README choices; do not reintroduce removed promo assets without ask

### Regression pins (keep these green)
- `TestManagementRoutesAreRegistered` — management routes the UI depends on
- `TestRegisterAvailableExecutors` — must include `qodercn` and `qoder`
- Private-instructions policy unit tests under `sdk/cliproxy/auth/`

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
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket, Qoder/Qoder CN)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` / `internal/usagestore/` — Usage and durable monitoring store
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline). Provider wiring lives in split files: `service_executors.go`, `service_models.go`, `service_auth.go`, …
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
