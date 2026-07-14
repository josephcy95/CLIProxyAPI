# CLI Proxy API — Operational Fork

[中文](README.md)

This is an operational fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI): a proxy that exposes OpenAI-, Gemini-, Claude-, Codex-, and Grok-compatible APIs for supported CLI accounts and API keys.

The upstream project remains the source of truth for general provider support, setup, API behavior, SDK documentation, and troubleshooting. This fork regularly merges `router-for-me/CLIProxyAPI:main`, while concentrating on reliable large credential pools and a cleaner management experience.

## Why this fork exists

Shared OAuth account pools need predictable credential selection. An exhausted or dead account should leave the candidate pool promptly, survive a restart without losing its state, and not slow every request while the proxy cycles through it.

This fork adds the operational controls needed for that workflow.

## Fork-specific features

### Pool-grade xAI and Codex credential handling

- Model-level cooldowns after recognized quota and usage-limit failures.
- Cooldown state and counters persisted in each auth JSON file, including across credential reloads.
- Streaming quota failures preserve their retry/cooldown information instead of falling back to a short retry loop.
- Configurable xAI auto-disable for repeatedly exhausted free-usage accounts and repeated unusable `403` responses.
- Configurable Codex auto-disable for confirmed auth death, repeated usage-limit exhaustion, `402`, and `deactivated_workspace` failures.
- Scheduler selection excludes credentials that are actively cooling down; exhausted candidates are reported as unavailable rather than as missing credentials.

### Codex private instructions

Optional private instruction injection for custom prompting / **破限提示词** workflows.

- Model-marker routing or marker-free mode.
- Per-auth allow flag, so only explicitly permitted accounts receive private instructions.
- Optional reservation of marked accounts for private-instruction traffic.

### Operational visibility

- xAI auth runtime/status metadata exposed through management APIs.
- Stored Codex plan metadata exposed to management clients.
- Management controls for xAI and Codex failure policies.

### Cleaner management UI

The companion [CLI Proxy API Management Center fork](https://github.com/josephcy95/Cli-Proxy-API-Management-Center) focuses on a simpler, cleaner interface:

- Streamlined navigation, typography, full-width provider/config pages, and fewer decorative animations.
- Redesigned visual configuration editor with a sticky settings layout.
- Compact API-key tables and clearer Codex error-handling controls.
- Better Auth Files search and filters for xAI status, Codex plan/status, and private-instructions eligibility.

## Install

- **Release assets:** [GitHub Releases](https://github.com/josephcy95/CLIProxyAPI/releases)
- **Docker:**

  ```bash
  docker pull ghcr.io/josephcy95/cli-proxy-api:latest
  # Or pin a release:
  docker pull ghcr.io/josephcy95/cli-proxy-api:v7.2.74
  ```

- **Bundled management UI:** open `/management.html` after the server starts.

For provider setup, config reference, API compatibility details, SDK usage, and OAuth flows, use the [upstream documentation](https://github.com/router-for-me/CLIProxyAPI) and then apply this fork's pool/failure-policy configuration as needed.

## Upstream update policy

Upstream `main` is merged with normal Git merge commits so the fork remains easy to compare and update. Fork-specific behavior is preserved where it improves credential-pool operations.

## Security

Auth files, refresh tokens, API keys, and the management key are sensitive. Do not commit them, expose the management interface publicly without access controls, or share exported auth JSON files.

## Thanks

- [**LINUX DO**](https://linux.do/) — Community discussion and user feedback.

## License

MIT. This fork retains the upstream project's license and attribution.
