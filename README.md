# CLI Proxy API（运维二开）

[English](README_EN.md)

基于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的二开，主要给共享 OAuth 账号池用。安装、Provider、SDK 等请看上游文档，这里只列主要改动。

会时不时跟着上游更新版本。

## 主要改动

**xAI / Codex 账号池**

- 额度 / usage-limit 后按模型进入 cooldown，状态写在 auth JSON 里，重载后仍保留
- 流式请求中的额度错误会保留 RetryAfter，避免掉进短重试循环
- 可自动禁用：xAI 反复耗尽免费额度 / 不可用 403；Codex 凭证失效、反复 usage-limit、`402`、`deactivated_workspace`
- 调度会跳过仍在 cooldown 的凭证

**Codex 私有指令（破限提示词）**

- 支持模型标记路由，也可关闭标记
- 按 auth 文件单独开启；可选将带标记账号仅用于私有指令流量

**管理侧**

- 管理 API 暴露 xAI 运行时状态、Codex 套餐信息与失败策略开关
- 请求监控（Realtime / Accounts / Prices）：SQLite 持久化 usage 事件，管理 API 提供列表、汇总、账号用量与模型定价/别名
- 配套 UI：[Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)（界面更简洁，Auth Files 筛选更好用；内置 `/monitoring`）

其它小修小补没有一一列出，直接看 commit 即可。

## 安装

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker Compose 部署（推荐）

持久化目录建议分开挂载：`config.yaml`、认证文件、日志，以及 **请求监控数据**（`usage.db`、模型价格/别名）。

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    volumes:
      # 配置
      - ./config.yaml:/CLIProxyAPI/config.yaml
      # OAuth / API 凭证（auth JSON）
      - ./auths:/root/.cli-proxy-api
      # 日志
      - ./logs:/CLIProxyAPI/logs
      # 请求监控持久化数据（usage.db、model prices/aliases）
      # 宿主机路径请按环境修改，例如 Unraid:
      #   /mnt/user/appdata/cliproxyapi/data:/CLIProxyAPI/data
      - ./data:/CLIProxyAPI/data
    restart: unless-stopped
```

也可用环境变量覆盖挂载路径（见仓库内 `docker-compose.yml`）：

| 变量 | 默认 | 容器内路径 |
|------|------|------------|
| `CLI_PROXY_CONFIG_PATH` | `./config.yaml` | `/CLIProxyAPI/config.yaml` |
| `CLI_PROXY_AUTH_PATH` | `./auths` | `/root/.cli-proxy-api` |
| `CLI_PROXY_LOG_PATH` | `./logs` | `/CLIProxyAPI/logs` |
| `CLI_PROXY_DATA_PATH` | `./data` | `/CLIProxyAPI/data` |

`CLI_PROXY_DATA_PATH` 对应监控库默认路径 `usage-store-path: "data/usage.db"`（容器内即 `/CLIProxyAPI/data/usage.db`）。**不要**把该目录挂到 `auths` 或 `logs` 下，以免重建容器或清理日志时丢掉历史请求。

在 `config.yaml` 中开启统计并确认路径（可选）：

```yaml
usage-statistics-enabled: true
usage-store-path: "data/usage.db"
# usage-retention-days: 30
```

启动：

```bash
mkdir -p auths logs data
docker compose up -d
```

服务启动后访问 `/management.html`，管理界面中打开 **Monitoring**（`/monitoring`）即可查看实时请求、账号用量与定价。也可在 Monitoring 页一键开启 usage statistics。

感谢 [LINUX DO](https://linux.do/) 社区的交流。MIT，上游协议和署名照旧保留。
