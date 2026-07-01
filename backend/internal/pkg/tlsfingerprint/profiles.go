package tlsfingerprint

const (
	ProfileNameClaudeCLIDefault     = "claude-cli-default"
	ProfileNameClaudeCLILinux       = "claude-cli-linux-node24"
	ProfileNameClaudeCLIMacOS       = "claude-cli-macos-node24"
	ProfileNameClaudeCLIWindows     = "claude-cli-windows-node24"
	ProfileNameClaudeDesktopDefault = "claude-desktop-default"
	ProfileNameCodexCLIDefault      = "codex-cli-default"
	ProfileNameCodexCLILinux        = "codex-cli-linux-node24"
	ProfileNameCodexCLIMacOS        = "codex-cli-macos-node24"
	ProfileNameCodexCLIWindows      = "codex-cli-windows-node24"
	ProfileNameCodexDesktopDefault  = "codex-desktop-default"
)

var builtInProfiles = map[string]*Profile{
	ProfileNameClaudeCLIDefault:     node24DefaultProfile(ProfileNameClaudeCLIDefault),
	ProfileNameClaudeCLILinux:       node24DefaultProfile(ProfileNameClaudeCLILinux),
	ProfileNameClaudeCLIMacOS:       node24GreaseProfile(ProfileNameClaudeCLIMacOS),
	ProfileNameClaudeCLIWindows:     node24WindowsProfile(ProfileNameClaudeCLIWindows),
	ProfileNameClaudeDesktopDefault: node24GreaseProfile(ProfileNameClaudeDesktopDefault),
	ProfileNameCodexCLIDefault:      node24DefaultProfile(ProfileNameCodexCLIDefault),
	ProfileNameCodexCLILinux:        node24DefaultProfile(ProfileNameCodexCLILinux),
	ProfileNameCodexCLIMacOS:        node24GreaseProfile(ProfileNameCodexCLIMacOS),
	ProfileNameCodexCLIWindows:      node24WindowsProfile(ProfileNameCodexCLIWindows),
	ProfileNameCodexDesktopDefault:  node24GreaseProfile(ProfileNameCodexDesktopDefault),
}

func node24DefaultProfile(name string) *Profile {
	return &Profile{
		Name:                name,
		CipherSuites:        append([]uint16(nil), defaultCipherSuites...),
		Curves:              []uint16{0x001d, 0x0017, 0x0018},
		PointFormats:        []uint16{0},
		SignatureAlgorithms: []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601, 0x0201},
		ALPNProtocols:       []string{"http/1.1"},
		SupportedVersions:   []uint16{0x0304, 0x0303},
		KeyShareGroups:      []uint16{0x001d},
		PSKModes:            []uint16{1},
		Extensions:          append([]uint16(nil), defaultExtensionOrder...),
	}
}

func node24GreaseProfile(name string) *Profile {
	profile := node24DefaultProfile(name)
	profile.EnableGREASE = true
	profile.KeyShareGroups = []uint16{0x001d, 0x0017}
	return profile
}

func node24WindowsProfile(name string) *Profile {
	profile := node24DefaultProfile(name)
	profile.Extensions = []uint16{0, 23, 65281, 10, 11, 35, 16, 5, 13, 18, 51, 45, 43}
	return profile
}

// BuiltInProfileByName returns a copy of a built-in family profile.
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
