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
- Request monitoring (Realtime / Accounts / Prices): durable SQLite usage events, management APIs for list/summary/account stats and model prices/aliases
- Companion UI: [Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) (cleaner UI, better Auth Files filters; includes `/monitoring`)

Smaller fixes are not listed here — check the commits.

## Install

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker Compose (recommended)

Mount config, credentials, logs, and **request-monitoring data** (`usage.db`, model prices/aliases) as separate volumes:

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    volumes:
      # Config
      - ./config.yaml:/CLIProxyAPI/config.yaml
      # OAuth / API credentials (auth JSON)
      - ./auths:/root/.cli-proxy-api
      # Logs
      - ./logs:/CLIProxyAPI/logs
      # Persistent request-monitoring data (usage.db, model prices/aliases)
      # Adjust the host path for your environment, e.g. Unraid:
      #   /mnt/user/appdata/cliproxyapi/data:/CLIProxyAPI/data
      - ./data:/CLIProxyAPI/data
    restart: unless-stopped
```

You can override mount paths via env vars (see `docker-compose.yml` in this repo):

| Variable | Default | Container path |
|----------|---------|----------------|
| `CLI_PROXY_CONFIG_PATH` | `./config.yaml` | `/CLIProxyAPI/config.yaml` |
| `CLI_PROXY_AUTH_PATH` | `./auths` | `/root/.cli-proxy-api` |
| `CLI_PROXY_LOG_PATH` | `./logs` | `/CLIProxyAPI/logs` |
| `CLI_PROXY_DATA_PATH` | `./data` | `/CLIProxyAPI/data` |

`CLI_PROXY_DATA_PATH` backs the default `usage-store-path: "data/usage.db"` (inside the container: `/CLIProxyAPI/data/usage.db`). **Do not** nest this under `auths` or `logs`, or you may lose history when rotating credentials or clearing logs.

Enable statistics in `config.yaml` (optional; path can be left default):

```yaml
usage-statistics-enabled: true
usage-store-path: "data/usage.db"
# usage-retention-days: 30
```

Start:

```bash
mkdir -p auths logs data
docker compose up -d
```

Management UI is at `/management.html` after the server starts. Open **Monitoring** (`/monitoring`) for realtime requests, account usage, and pricing. You can also enable usage statistics from that page.

Thanks to the [LINUX DO](https://linux.do/) community for the discussion. MIT; upstream license and attribution kept.
