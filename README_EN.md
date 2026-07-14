# CLI Proxy API (ops fork)

[中文](README.md)

A fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), mainly for shared OAuth account pools. For install, providers, and SDK, use the upstream docs — this page only covers the main changes.

I merge upstream from time to time.

## Main changes

**xAI / Codex pool**

- Model-level cooldowns after quota / usage-limit errors; state is stored in each auth JSON and survives reloads
- Streaming quota errors keep RetryAfter instead of collapsing into a short retry loop
- Optional auto-disable: xAI free-usage exhaustion / unusable `403`; Codex dead auth, repeated usage-limit, `402`, `deactivated_workspace`
- Scheduler skips credentials still in cooldown

**Codex private instructions (jailbreak / custom prompts)**

- Model-marker routing, or marker-free mode
- Per-auth toggle; optional: reserve marked accounts for private-instruction traffic only

**Management**

- Management API exposes xAI runtime status, Codex plan info, and failure-policy toggles
- Companion UI: [Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) (cleaner UI, better Auth Files filters)

Smaller fixes are not listed here — check the commits.

## Install

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

Management UI is at `/management.html` after the server starts.

Thanks to the [LINUX DO](https://linux.do/) community for the discussion. MIT; upstream license and attribution kept.
