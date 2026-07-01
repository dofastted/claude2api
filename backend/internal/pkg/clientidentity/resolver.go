package clientidentity

import (
	"strings"

	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
)

const (
	TLSProfileClaudeCLIDefault     = tlsfingerprint.ProfileNameClaudeCLIDefault
	TLSProfileClaudeDesktopDefault = tlsfingerprint.ProfileNameClaudeDesktopDefault
	TLSProfileCodexCLIDefault      = tlsfingerprint.ProfileNameCodexCLIDefault
	TLSProfileCodexDesktopDefault  = tlsfingerprint.ProfileNameCodexDesktopDefault
)

type Account interface {
	GetExtraString(key string) string
}

type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

// Resolve maps account.extra.client_family to a complete identity snapshot.
// Empty or unknown families return nil so callers can preserve existing defaults.
func (r *Resolver) Resolve(account Account) *IdentitySnapshot {
	if account == nil {
		return nil
	}
	familyStr := strings.TrimSpace(account.GetExtraString("client_family"))
	if familyStr == "" {
		return nil
	}

	switch ClientFamily(familyStr) {
	case FamilyClaudeCLI:
		return r.claudeCLISnapshot()
	case FamilyClaudeDesktop:
		return r.claudeDesktopSnapshot()
	case FamilyCodexCLI:
		return r.codexCLISnapshot()
	case FamilyCodexDesktop:
		return r.codexDesktopSnapshot()
	default:
		return nil
	}
}

func (r *Resolver) claudeCLISnapshot() *IdentitySnapshot {
	// TODO(R1): read dynamic versions from IdentityRegistry atomic.Pointer.
	return &IdentitySnapshot{
		Headers:        cloneHeaders(defaultClaudeHeaders),
		VersionFields:  VersionFields{CLIVersion: defaultClaudeCLIVersion, SDKVersion: defaultClaudeSDKVersion},
		TLSProfileName: TLSProfileClaudeCLIDefault,
	}
}

func (r *Resolver) claudeDesktopSnapshot() *IdentitySnapshot {
	// TODO(R1): replace placeholder headers/version fields with captured Claude Desktop data.
	return &IdentitySnapshot{
		Headers: map[string]string{
			"User-Agent":       "claude-desktop/0.0.0",
			"X-App":            "desktop",
			"X-Stainless-Lang": "js",
		},
		VersionFields:  VersionFields{CLIVersion: "0.0.0"},
		TLSProfileName: TLSProfileClaudeDesktopDefault,
	}
}

func (r *Resolver) codexCLISnapshot() *IdentitySnapshot {
	return &IdentitySnapshot{
		Headers: map[string]string{
			"User-Agent": defaultCodexCLIUA,
			"originator": "codex_cli_rs",
		},
		VersionFields:  VersionFields{CLIVersion: defaultCodexCLIVersion},
		TLSProfileName: TLSProfileCodexCLIDefault,
	}
}

func (r *Resolver) codexDesktopSnapshot() *IdentitySnapshot {
	// TODO(R1): replace placeholder headers/version fields with captured Codex Desktop data.
	return &IdentitySnapshot{
		Headers: map[string]string{
			"User-Agent": "codex-desktop/0.0.0",
			"X-App":      "desktop",
		},
		VersionFields:  VersionFields{CLIVersion: "0.0.0"},
		TLSProfileName: TLSProfileCodexDesktopDefault,
	}
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}
