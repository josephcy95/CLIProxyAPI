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

其它小修小补没有一一列出，直接看 commit 即可。

## 安装

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

### Docker 部署（推荐：单卷 `/data`）

只需挂一个持久化目录到容器 `/data`。应用会自动使用：

```
/data/config.yaml
/data/auths/
/data/logs/
/data/plugins/
/data/usage.db
```

可选环境变量 `CLIPROXY_DATA_DIR`（默认 `/data`）。首次启动若无 `config.yaml`，entrypoint 会从镜像内 `config.example.yaml` 复制一份。

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/josephcy95/cli-proxy-api:latest
    pull_policy: always
    container_name: cli-proxy-api
    ports:
      - "8317:8317"
    volumes:
      # Unraid 示例: /mnt/user/appdata/cliproxyapi-patched:/data
      - ./data:/data
    restart: unless-stopped
```

Unraid `docker run` 精简示例：

```bash
docker run -d --name cli-proxy-api-patched --net cpa \
  -e TZ=Asia/Singapore \
  -p 58317:8317 \
  -v /mnt/user/appdata/cliproxyapi-patched:/data \
  --restart unless-stopped \
  ghcr.io/josephcy95/cli-proxy-api:latest
```

在 `config.yaml` 中开启统计（路径默认可省略）：

```yaml
usage-statistics-enabled: true
# usage-store-path: "usage.db"
# usage-retention-days: 30
```

启动：

```bash
mkdir -p data
docker compose up -d
```

服务启动后访问 `/management.html`，管理界面中打开 **Monitoring**（`/monitoring`）即可查看实时请求、账号用量与定价。也可在 Monitoring 页一键开启 usage statistics。

**迁移旧多挂载部署：** 把原来的 `config.yaml`、`auths/`、`logs/`、`plugins/`、`usage.db`（或 `data/usage.db`）都放到同一 host 目录下再挂到 `/data`。`auth-dir` 改为 `auths`（或绝对路径）。

感谢 [LINUX DO](https://linux.do/) 社区的交流。MIT，上游协议和署名照旧保留。
