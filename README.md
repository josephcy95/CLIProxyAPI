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
- 配套 UI：[Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)（界面更简洁，Auth Files 筛选更好用）

其它小修小补没有一一列出，直接看 commit 即可。

## 安装

```bash
docker pull ghcr.io/josephcy95/cli-proxy-api:latest
```

服务启动后访问 `/management.html` 即可使用管理界面。

感谢 [LINUX DO](https://linux.do/) 社区的交流。MIT，上游协议和署名照旧保留。
