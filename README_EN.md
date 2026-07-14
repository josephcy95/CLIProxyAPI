# CLI Proxy API (ops fork)

[中文](README.md)

Fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) for shared OAuth account pools. Upstream docs cover install, providers, API, and SDK — this page is only the delta.

Upstream `main` is merged regularly.

## Changes

**xAI / Codex pool**

- Model-level cooldowns after quota / usage-limit errors; state lives in each auth JSON (survives reload)
- Streaming quota errors keep RetryAfter instead of collapsing into a short retry loop
- Optional auto-disable: xAI free-usage exhaustion / unusable `403`; Codex dead auth, repeated usage-limit, `402`, `deactivated_workspace`
- Scheduler skips credentials still in cooldown

**Codex private instructions (jailbreak / custom prompts)**

- Model-marker routing or marker-free mode
- Per-auth allow flag; optional reserve marked accounts for private-instruction traffic only

**Management**

- Management API exposes xAI runtime status, Codex plan metadata, failure-policy toggles
- Companion UI: [Management Center fork](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) (cleaner layout, better Auth Files filters)

## Get it

- [Releases](https://github.com/josephcy95/CLIProxyAPI/releases)
- Docker: `ghcr.io/josephcy95/cli-proxy-api:latest` (or pin e.g. `v7.2.74`)
- Management UI: `/management.html` once the server is up

## Misc

Don't commit auth files / tokens / management keys; don't expose management without access control.

Thanks [LINUX DO](https://linux.do/). MIT; upstream license and attribution kept.
