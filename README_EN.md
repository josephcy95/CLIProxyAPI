# CLI Proxy API (fork)

OpenAI/Gemini/Claude/Codex compatible proxy. This fork publishes to:

- GitHub: https://github.com/josephcy95/CLIProxyAPI
- Docker: `ghcr.io/josephcy95/cli-proxy-api`

Upstream: https://github.com/router-for-me/CLIProxyAPI

## Install

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker (recommended: single volume `/data`)

Mount one host directory to container `/data`. The app uses:

```
/data/config.yaml
/data/auths/
/data/logs/
/data/plugins/
/data/usage.db
```

Optional env `CLIPROXY_DATA_DIR` (default `/data`). On first start, if `config.yaml` is missing, the entrypoint seeds it from the image `config.example.yaml`.

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    volumes:
      # Unraid example: /mnt/user/appdata/cliproxyapi-patched:/data
      - ./data:/data
    restart: unless-stopped
```

Unraid `docker run` example:

```bash
docker run -d --name cli-proxy-api-patched --net cpa \
  -e TZ=Asia/Singapore \
  -p 58317:8317 \
  -v /mnt/user/appdata/cliproxyapi-patched:/data \
  --restart unless-stopped \
  ghcr.io/josephcy95/cli-proxy-api:latest
```

Enable statistics in `config.yaml` (path may be omitted):

```yaml
usage-statistics-enabled: true
# usage-store-path: "usage.db"
# usage-retention-days: 30
```

Start:

```bash
mkdir -p data
docker compose up -d
```

Management UI is at `/management.html` after the server starts. Open **Monitoring** (`/monitoring`) for realtime requests, account usage, and pricing.

**Migrating from multi-volume setups:** put former `config.yaml`, `auths/`, `logs/`, `plugins/`, and `usage.db` (or `data/usage.db`) into one host directory and mount that to `/data`. Set `auth-dir: "auths"` (or an absolute path).

MIT; upstream license and attribution retained.
