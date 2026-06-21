package clientidentity

type ClientFamily string

const (
	FamilyClaudeCLI     ClientFamily = "claude-cli"
	FamilyClaudeDesktop ClientFamily = "claude-desktop"
	FamilyCodexCLI      ClientFamily = "codex-cli"
	FamilyCodexDesktop  ClientFamily = "codex-desktop"
)
