# CLI Proxy API（运维二开）

[English](README_EN.md)

基于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的二开，主要给共享 OAuth 账号池用。安装、Provider、SDK 这些还是看上游文档，这边只写我加了啥。

会时不时跟着上游更新版本。

## 改了啥

**xAI / Codex 账号池**

- 额度 / usage-limit 之后按模型进 cooldown，状态直接写进 auth JSON，重载也不丢
- 流式请求里的额度错误会把 RetryAfter 留住，不会掉进无意义的短重试循环
- 可以自动踢号：xAI 老是刷完免费额度 / 不可用 403；Codex 凭证挂了、反复 usage-limit、`402`、`deactivated_workspace`
- 还在 cooldown 的凭证，调度直接跳过

**Codex 私有指令（破限提示词）**

- 支持模型标记路由，也可以关标记
- 按 auth 文件单独开；可选：带标记的账号只接私有指令流量

**管理侧**

- 管理 API 能看到 xAI 运行时状态、Codex 套餐信息，以及失败策略开关
- 配套 UI：[Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)（界面干净一点，Auth Files 筛选更好用）

## 安装

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

二进制在 [Releases](https://github.com/josephcy95/CLIProxyAPI/releases)。服务起来之后打开 `/management.html` 就是管理界面。

感谢 [LINUX DO](https://linux.do/) 社区的交流。MIT，上游协议和署名照旧保留。
