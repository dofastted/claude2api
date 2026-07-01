package tlsfingerprint

import "testing"

func TestBuiltInProfilesAreExplicitAndIsolated(t *testing.T) {
	profiles := []string{
		ProfileNameClaudeCLIDefault,
		ProfileNameClaudeCLILinux,
		ProfileNameClaudeCLIMacOS,
		ProfileNameClaudeCLIWindows,
		ProfileNameClaudeDesktopDefault,
		ProfileNameCodexCLIDefault,
		ProfileNameCodexCLILinux,
		ProfileNameCodexCLIMacOS,
		ProfileNameCodexCLIWindows,
		ProfileNameCodexDesktopDefault,
	}
	for _, name := range profiles {
		profile := BuiltInProfileByName(name)
		if profile == nil {
			t.Fatalf("BuiltInProfileByName(%q) returned nil", name)
		}
		if profile.Name != name {
			t.Fatalf("profile.Name = %q, want %q", profile.Name, name)
		}
		if len(profile.CipherSuites) == 0 || len(profile.Extensions) == 0 {
			t.Fatalf("profile %q must carry explicit cipher suites and extensions", name)
		}
	}

	first := BuiltInProfileByName(ProfileNameCodexCLILinux)
	first.CipherSuites[0] = 0
	second := BuiltInProfileByName(ProfileNameCodexCLILinux)
	if second.CipherSuites[0] == 0 {
		t.Fatal("built-in profile clone leaked mutation")
	}
}

func TestBuiltInProfilesVaryByEnvironment(t *testing.T) {
	linux := BuiltInProfileByName(ProfileNameCodexCLILinux)
	macos := BuiltInProfileByName(ProfileNameCodexCLIMacOS)
	windows := BuiltInProfileByName(ProfileNameCodexCLIWindows)
	if linux == nil || macos == nil || windows == nil {
		t.Fatal("expected codex environment profiles")
	}
	if macos.EnableGREASE == linux.EnableGREASE {
		t.Fatal("macOS profile should differ from Linux by GREASE")
	}
	if len(windows.Extensions) == len(linux.Extensions) {
		t.Fatal("Windows profile should use a distinct extension order")
	}
}
