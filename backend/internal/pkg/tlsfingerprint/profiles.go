package tlsfingerprint

const (
	ProfileNameClaudeCLIDefault     = "claude-cli-default"
	ProfileNameClaudeDesktopDefault = "claude-desktop-default"
	ProfileNameCodexCLIDefault      = "codex-cli-default"
	ProfileNameCodexDesktopDefault  = "codex-desktop-default"
)

// TODO: Capture real TLS fingerprints (JA3, cipher/curve/extension order) for
// each client family and replace these framework placeholders.
// Source: packet capture from real official client requests.
// Capture date: TBD.
var builtInProfiles = map[string]*Profile{
	ProfileNameClaudeCLIDefault: {
		Name: ProfileNameClaudeCLIDefault,
	},
	ProfileNameClaudeDesktopDefault: {
		Name: ProfileNameClaudeDesktopDefault,
	},
	ProfileNameCodexCLIDefault: {
		Name: ProfileNameCodexCLIDefault,
	},
	ProfileNameCodexDesktopDefault: {
		Name: ProfileNameCodexDesktopDefault,
	},
}

// BuiltInProfileByName returns a copy of a built-in family profile placeholder.
// Unknown names return nil so callers can fall back to existing behavior.
func BuiltInProfileByName(name string) *Profile {
	profile, ok := builtInProfiles[name]
	if !ok || profile == nil {
		return nil
	}
	return cloneProfile(profile)
}

func cloneProfile(profile *Profile) *Profile {
	if profile == nil {
		return nil
	}
	return &Profile{
		Name:                profile.Name,
		CipherSuites:        append([]uint16(nil), profile.CipherSuites...),
		Curves:              append([]uint16(nil), profile.Curves...),
		PointFormats:        append([]uint16(nil), profile.PointFormats...),
		EnableGREASE:        profile.EnableGREASE,
		SignatureAlgorithms: append([]uint16(nil), profile.SignatureAlgorithms...),
		ALPNProtocols:       append([]string(nil), profile.ALPNProtocols...),
		SupportedVersions:   append([]uint16(nil), profile.SupportedVersions...),
		KeyShareGroups:      append([]uint16(nil), profile.KeyShareGroups...),
		PSKModes:            append([]uint16(nil), profile.PSKModes...),
		Extensions:          append([]uint16(nil), profile.Extensions...),
	}
}
