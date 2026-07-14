# CLI Proxy API（运维分叉）

[English](README_EN.md)

基于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的分叉，给共享 OAuth 账号池用。上游文档、安装、Provider、SDK 都去那边看；这边只列改动。

会定期 merge 上游 `main`。

## 改了什么

**xAI / Codex 账号池**

- 额度 / usage-limit 后按模型 cooldown，状态写在 auth JSON 里，重载不丢
- 流式额度错误保留 RetryAfter，不会掉进短重试循环
- 可配置自动踢掉：xAI 反复耗尽免费额度 / 不可用 403；Codex 凭证死掉、反复 usage-limit、`402`、`deactivated_workspace`
- 调度跳过 cooldown 中的凭证

**Codex 私有指令（破限提示词）**

- 模型标记路由或无标记模式
- 按 auth 文件开关；可选把带标记账号只留给私有指令流量

**管理侧**

- 管理 API 暴露 xAI 运行时状态、Codex 套餐元数据、失败策略开关
- 配套 UI：[Management Center 分叉](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)（更干净一点，Auth Files 筛选更好用）

## 下载

- [Releases](https://github.com/josephcy95/CLIProxyAPI/releases)
- Docker：`ghcr.io/josephcy95/cli-proxy-api:latest`（或 pin 版本如 `v7.2.74`）
- 管理 UI：服务起来后 `/management.html`

## 其它

Auth 文件、token、management key 别提交、别裸奔。

Thanks [LINUX DO](https://linux.do/)。MIT，上游协议与署名保留。
