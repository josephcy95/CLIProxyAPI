# CLI Proxy API (ops fork)

[中文](README.md)

A fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), mainly for shared OAuth account pools. Install, providers, SDK — still use the upstream docs. This page is just what I added.

I merge upstream from time to time.

## What changed

**xAI / Codex pool**

- Model-level cooldowns after quota / usage-limit errors; state is stored in each auth JSON and survives reloads
- Streaming quota errors keep RetryAfter instead of collapsing into a short retry loop
- Optional auto-kick: xAI free-usage exhaustion / unusable `403`; Codex dead auth, repeated usage-limit, `402`, `deactivated_workspace`
- Scheduler skips credentials still in cooldown

**Codex private instructions (jailbreak / custom prompts)**

- Model-marker routing, or marker-free mode
- Per-auth toggle; optional: reserve marked accounts for private-instruction traffic only

**Management**

- Management API exposes xAI runtime status, Codex plan info, and failure-policy toggles
- Companion UI: [Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) (cleaner UI, better Auth Files filters)

## Install

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

Binaries are on [Releases](https://github.com/josephcy95/CLIProxyAPI/releases). Management UI is at `/management.html` once the server is up.

Thanks to the [LINUX DO](https://linux.do/) community for the discussion. MIT; upstream license and attribution kept.
