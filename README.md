# CLI Proxy API — 运维增强分叉版

[English](README_EN.md)

这是基于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的运维增强分叉版。它为受支持的 CLI 账号和 API Key 提供兼容 OpenAI、Gemini、Claude、Codex 与 Grok 的代理接口。

上游项目仍是通用 Provider 支持、安装、API 行为、SDK 文档和排错的权威来源。本分叉版会定期合并 `router-for-me/CLIProxyAPI:main`，并专注于大型共享账号池的稳定性与更简洁的管理体验。

## 为什么做这个分叉

共享 OAuth 账号池需要可预测的凭证选择：额度耗尽或已经失效的账号应尽快离开候选池；重启或重载后不应丢失状态；代理也不应在无额度账号之间反复轮询而拖慢每一次请求。

本分叉版针对这些运维场景增加了控制与可观测性。

## 分叉版功能

### 面向账号池的 xAI 与 Codex 凭证管理

- 识别到额度或 usage-limit 错误后，按模型进入 cooldown。
- Cooldown 状态和计数器直接持久化在各个 auth JSON 文件中，凭证重载后仍会保留。
- 流式请求中的额度错误会保留正确的 RetryAfter / cooldown 信息，不会退化为短时间重试循环。
- 可配置地自动禁用反复耗尽免费额度的 xAI 账号，以及反复出现不可用 `403` 的账号。
- 可配置地自动禁用已确认失效的 Codex 凭证、反复 usage-limit 耗尽、`402` 与 `deactivated_workspace`。
- 调度器会排除仍在 cooldown 中的凭证；已耗尽的候选账号会被正确报告为不可用，而不是不存在。

### Codex 私有指令

可选的私有指令注入，用于自定义提示词 / **破限提示词** 工作流。

- 支持模型标记路由或无标记模式。
- 支持每个 auth 文件单独允许，只有明确授权的账号会接收私有指令。
- 可选地保留带标记账号，仅服务私有指令流量。

### 运维可观测性

- 通过管理 API 暴露 xAI auth 的运行时状态和凭证状态。
- 向管理客户端暴露已保存的 Codex 套餐元数据。
- 提供 xAI 与 Codex 失败策略的管理控制项。

### 更简洁的管理 UI

配套的 [CLI Proxy API Management Center 分叉版](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) 聚焦于简洁、干净的管理体验：

- 精简导航与排版，Provider / 配置页面使用全宽布局，并减少装饰性动画。
- 重新设计可视化配置编辑器，使用固定的设置布局。
- 更紧凑的 API Key 表格，以及更清晰的 Codex 错误处理控制项。
- Auth Files 提供更好的搜索和筛选：xAI 状态、Codex 套餐 / 状态、私有指令资格。

## 安装

- **发行版文件：** [GitHub Releases](https://github.com/josephcy95/CLIProxyAPI/releases)
- **Docker：**

  ```bash
  docker pull ghcr.io/josephcy95/cli-proxy-api:latest
  # 或固定到指定版本：
  docker pull ghcr.io/josephcy95/cli-proxy-api:v7.2.74
  ```

- **内置管理 UI：** 服务启动后访问 `/management.html`。

Provider 配置、完整 config 参考、API 兼容说明、SDK 用法与 OAuth 流程请先参考[上游项目文档](https://github.com/router-for-me/CLIProxyAPI)，再按需要配置本分叉版的账号池和失败策略。

## 上游更新策略

上游 `main` 使用正常的 Git merge commit 合并，以便持续比较和更新。只要分叉版逻辑确实改善账号池运维，就会保留相应的自定义行为。

## 安全提示

Auth 文件、refresh token、API Key 和 management key 都是敏感信息。不要提交到仓库；不要在缺少访问控制时公开管理页面；不要分享导出的 auth JSON 文件。

## 致谢

- [**LINUX DO**](https://linux.do/) — 社区交流与用户反馈。

## 许可证

MIT。本分叉版保留上游项目的许可证与署名。
