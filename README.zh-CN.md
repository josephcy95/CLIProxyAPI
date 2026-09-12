# CLI Proxy API

面向共享账号池、API Key 与 OAuth 凭证的实用多供应商代理，对外提供统一的 OpenAI 兼容 API。

本仓库是 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的 fork，侧重账号池管理、Codex 路由、监控与日常运维。

[English](README.md) | [中文](README.zh-CN.md)

## 本 fork 新增

- 🧭 **自适应 Codex 路由**：综合账号可用性、周配额、续期时间、reset credits、优先级、权重与单账号并发。
- 🛡️ **更安全的忙账号处理**：高并发负载在账号间分散，避免反复打爆同一账号的 429。
- 📝 **Codex 私有 instructions**：模型标记、供应商过滤、API Key 支持与专用管理流程。
- 🆓 **共享模型免费优先路由**：有免费账号时可选优先走免费池。
- ♻️ **更强的 Codex 恢复**：冷却、耗尽账号、过期/停用 auth、reset credits 与过载流式响应。
- 📊 **持久化用量监控**：请求历史、账号状态、API Key 消费、token 明细、价格与模型路由信息。
- 🌏 **Qoder 支持**：国际站与 Qoder 国区登录、模型、配额、区域与失败策略。
- 🔑 **按 Key 路由控制**：OpenAI 兼容账号的优先级与加权轮询。
- 🧩 **自定义模型控制**：上下文窗口与推理元数据覆盖。
- 🔏 **Privacy / 脱敏** — 可逆的 PII/密钥 mask+restore（`{{TYPE_id}}`）、分类开关、作用域（客户端 Key / OAuth / API 供应商）、跳过模型/格式、精确 allowlist，以及保守默认（默认关闭；仅低延迟内存映射）。
- 🐳 **Fork 友好的 Docker 打包**：单一 `/data` 卷，发布镜像到 GHCR。
- ✨ **以及更多** 小修复、兼容性改进、供应商接入与运维便利。

## 管理中心 · Privacy

脱敏配置可在配套 **Management Center** 的 **Privacy / 脱敏** 页面开关与预览；也可通过管理 API `GET/PUT/PATCH /v0/management/desensitization-config` 调整。

- Management UI： [CLI Proxy API Management Center](https://github.com/josephcy95/Cli-Proxy-API-Management-Center)
- 本 API： [josephcy95/CLIProxyAPI](https://github.com/josephcy95/CLIProxyAPI)

安装与 Docker 说明见 [README_EN.md](README_EN.md)（英文安装指南）或仓库中的 `config.example.yaml`。
