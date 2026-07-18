# CLI Proxy API（运维二开）

[English](README_EN.md)

基于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的二开，主要给共享 OAuth 账号池用。安装、Provider、SDK 等请看上游文档，这里只列主要改动。

会时不时跟着上游更新版本。

## 主要改动

**xAI / Codex 账号池**

- 额度 / usage-limit 后按模型进入 cooldown，状态写在 auth JSON 里，重载后仍保留
- 流式请求中的额度错误会保留 RetryAfter，避免掉进短重试循环
- 可自动禁用：xAI 反复耗尽免费额度 / 不可用 403 / 刷新后仍 401；Codex 凭证失效、反复 usage-limit、`402`、`deactivated_workspace`
- 调度会跳过仍在 cooldown 的凭证

**Codex 私有指令（破限提示词）**

- 支持模型标记路由，也可关闭标记
- 按 auth 文件单独开启；可选将带标记账号仅用于私有指令流量

**管理侧**

- 管理 API 暴露 xAI 运行时状态、Codex 套餐信息与失败策略开关
- 请求监控（Realtime / Accounts / Prices）：SQLite 持久化 usage 事件，管理 API 提供列表、汇总、账号用量与模型定价/别名
- 配套 UI：[Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)（界面更简洁，Auth Files 筛选更好用；内置 `/monitoring`）

**Docker 单卷数据目录（相对上游）**

- 上游常见多挂载（config / `~/.cli-proxy-api` / logs 分开）
- 本 fork：一个 host 目录挂到容器 `/data`，配置、凭证、日志、插件、usage 都在同一树下

其它小修小补没有一一列出，直接看 commit 即可。

## 安装

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker 部署（推荐：单卷 `/data`）

只需挂 **一个** 持久化目录到容器 `/data`。不要挂到 `/CLIProxyAPI`（会盖住二进制）。

可选环境变量 `CLIPROXY_DATA_DIR`（默认 `/data`）。首次启动若无 `config.yaml`，entrypoint 会从镜像内 `config.example.yaml` 复制一份。

#### 目录结构（host `./data` → 容器 `/data`）

```
data/                          # 只挂这一层到 /data
├── config.yaml                # 主配置（可手改；也可被管理面板改）
├── .env                       # 可选
├── auths/                     # OAuth / API 凭证 JSON
│   ├── codex-xxx.json
│   └── xai-xxx.json
├── logs/                      # 应用日志（logging-to-file 时）
├── plugins/                   # 插件（可选）
│   └── codex-token-usage/
└── usage.db                   # 请求监控 SQLite（开启 usage-statistics 后）
```

对应 `config.yaml` 里建议：

```yaml
auth-dir: "auths"              # 相对 /data，不要用 ~/.cli-proxy-api
# usage-store-path: "usage.db" # 默认可省略
plugins:
  dir: "plugins"
```

#### docker-compose 示例

仓库根目录 `docker-compose.yml` 已是单卷写法。最小示例：

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    # 可选：覆盖数据根（镜像默认 /data）
    # environment:
    #   CLIPROXY_DATA_DIR: /data
    volumes:
      # Unraid 示例: /mnt/user/appdata/cliproxyapi/data:/data
      - ${CLI_PROXY_DATA_PATH:-./data}:/data
    restart: unless-stopped
```

启动：

```bash
mkdir -p data
docker compose up -d
# 管理界面: http://localhost:8317/management.html
# 监控:     http://localhost:8317/management.html#/monitoring
```

#### Unraid `docker run` 示例

```bash
docker run -d --name cliproxyapi --net cpa \
  -e TZ=Asia/Singapore \
  -e CLIPROXY_DATA_DIR=/data \
  -p 8317:8317 \
  -v /mnt/user/appdata/cliproxyapi/data:/data \
  --restart unless-stopped \
  ghcr.io/josephcy95/cli-proxy-api:latest
```

Host 上准备：

```
/mnt/user/appdata/cliproxyapi/data/
├── config.yaml
├── auths/
├── logs/
├── plugins/
└── usage.db
```

#### 与上游多挂载对比

上游 / 旧模板常见（**本 fork 不推荐**）：

```yaml
volumes:
  - ./config.yaml:/CLIProxyAPI/config.yaml
  - ./auths:/root/.cli-proxy-api
  - ./logs:/CLIProxyAPI/logs
  # 有的还要再挂 data → /CLIProxyAPI/data
```

本 fork（**推荐**）：

```yaml
volumes:
  - ./data:/data
```

| 用途 | 上游常见容器路径 | 本 fork（单卷） |
|------|------------------|-----------------|
| 配置 | `/CLIProxyAPI/config.yaml` | `/data/config.yaml` |
| 凭证 | `/root/.cli-proxy-api` | `/data/auths` |
| 日志 | `/CLIProxyAPI/logs` | `/data/logs` |
| 插件 | 工作目录 `plugins` | `/data/plugins` |
| usage DB | `/CLIProxyAPI/data/usage.db` | `/data/usage.db` |

#### 从上游多挂载迁移

把原来的 `config.yaml`、`auths/`、`logs/`、`plugins/`、`usage.db`（或 `data/usage.db`）都放进 **同一个** host 目录，再挂到 `/data`：

```bash
mkdir -p ./data
cp -a config.yaml ./data/
cp -a auths logs plugins ./data/ 2>/dev/null || true
cp -a data/usage.db ./data/usage.db 2>/dev/null || true
# 编辑 ./data/config.yaml: auth-dir: "auths"
docker compose up -d
```

在 `config.yaml` 中开启统计（路径默认可省略）：

```yaml
usage-statistics-enabled: true
# usage-store-path: "usage.db"
# usage-retention-days: 30
```

服务启动后访问 `/management.html`，管理界面中打开 **Monitoring**（`/monitoring`）即可查看实时请求、账号用量与定价。

**提示（Unraid 等）：** 若启动后出现 `failed to create watcher: too many open files`，在宿主机提高 inotify 上限（长期建议保留），例如：

```bash
sysctl -w fs.inotify.max_user_instances=1024
sysctl -w fs.inotify.max_user_watches=524288
```

感谢 [LINUX DO](https://linux.do/) 社区的交流。MIT，上游协议和署名照旧保留。
