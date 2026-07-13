# OAuth Environment Capsules

## Contract

- Only these OAuth credentials auto-bind environment capsules:
  - Anthropic OAuth (Claude Code)
  - OpenAI OAuth (Codex CLI)
  - Grok OAuth (Grok CLI)
- Each credential gets exactly 3 system capsules: windows, macos, linux.
- Capsules are credential-owned (`account.extra`), not pool-configured.
- Admin must not hand-edit frozen capsule identity for those OAuth types.
- OAuth pools are migration/egress/shadow policy only.

## Forbidden

- Pool-level manual capsule create/activate as environment source
- Generating capsules for SetupToken, APIKey, Gemini, Antigravity, etc.
- Using inbound UA/body to re-select environment for capsule OAuth accounts
