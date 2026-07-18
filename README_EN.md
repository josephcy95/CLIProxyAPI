# CLI Proxy API (fork)

[中文](README.md)

OpenAI/Gemini/Claude/Codex compatible proxy. This fork publishes to:

- GitHub: https://github.com/josephcy95/CLIProxyAPI
- Docker: `ghcr.io/josephcy95/cli-proxy-api`

Upstream: https://github.com/router-for-me/CLIProxyAPI

## Install

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker (recommended: single volume `/data`)

Mount **one** host directory to container `/data`. Do **not** mount over `/CLIProxyAPI` (that hides the binary).

Optional env `CLIPROXY_DATA_DIR` (default `/data`). On first start, if `config.yaml` is missing, the entrypoint seeds it from the image `config.example.yaml`.

#### Layout (host `./data` → container `/data`)

```
data/                          # only this root is mounted to /data
├── config.yaml                # main config
├── .env                       # optional
├── auths/                     # OAuth / API credential JSON
│   ├── codex-xxx.json
│   └── xai-xxx.json
├── logs/                      # app logs when logging-to-file is on
├── plugins/                   # optional plugins
│   └── codex-token-usage/
└── usage.db                   # request-monitoring SQLite
```

Suggested `config.yaml` paths:

```yaml
auth-dir: "auths"              # relative to /data — not ~/.cli-proxy-api
# usage-store-path: "usage.db" # optional; default is fine
plugins:
  dir: "plugins"
```

#### docker-compose example

Repo `docker-compose.yml` already uses a single volume. Minimal:

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    # environment:
    #   CLIPROXY_DATA_DIR: /data
    volumes:
      # Unraid: /mnt/user/appdata/cliproxyapi/data:/data
      - ${CLI_PROXY_DATA_PATH:-./data}:/data
    restart: unless-stopped
```

```bash
mkdir -p data
docker compose up -d
# UI:  http://localhost:8317/management.html
# Mon: http://localhost:8317/management.html#/monitoring
```

#### Unraid `docker run`

```bash
docker run -d --name cliproxyapi --net cpa \
  -e TZ=Asia/Singapore \
  -e CLIPROXY_DATA_DIR=/data \
  -p 8317:8317 \
  -v /mnt/user/appdata/cliproxyapi/data:/data \
  --restart unless-stopped \
  ghcr.io/josephcy95/cli-proxy-api:latest
```

#### vs upstream multi-volume (not recommended here)

Upstream-style:

```yaml
volumes:
  - ./config.yaml:/CLIProxyAPI/config.yaml
  - ./auths:/root/.cli-proxy-api
  - ./logs:/CLIProxyAPI/logs
```

This fork:

```yaml
volumes:
  - ./data:/data
```

| Role | Upstream-style path | This fork |
|------|---------------------|-----------|
| Config | `/CLIProxyAPI/config.yaml` | `/data/config.yaml` |
| Auths | `/root/.cli-proxy-api` | `/data/auths` |
| Logs | `/CLIProxyAPI/logs` | `/data/logs` |
| Plugins | workdir `plugins` | `/data/plugins` |
| usage DB | `/CLIProxyAPI/data/usage.db` | `/data/usage.db` |

#### Migrate from multi-volume

```bash
mkdir -p ./data
cp -a config.yaml ./data/
cp -a auths logs plugins ./data/ 2>/dev/null || true
cp -a data/usage.db ./data/usage.db 2>/dev/null || true
# set auth-dir: "auths" in ./data/config.yaml
docker compose up -d
```

```yaml
usage-statistics-enabled: true
# usage-store-path: "usage.db"
```

If you see `failed to create watcher: too many open files` on Unraid/Docker hosts, raise inotify limits on the **host** (keep them raised long-term):

```bash
sysctl -w fs.inotify.max_user_instances=1024
sysctl -w fs.inotify.max_user_watches=524288
```

MIT; upstream license and attribution retained.
