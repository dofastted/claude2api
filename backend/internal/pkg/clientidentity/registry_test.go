package clientidentity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistryAtomicSwap(t *testing.T) {
	registry := NewRegistry()
	initial := registry.Get()

	newSnapshots := &Snapshots{
		Claude: ClaudeSnapshot{VersionFields: VersionFields{CLIVersion: "1.0.0"}},
	}
	registry.Swap(newSnapshots)

	assert.Equal(t, "1.0.0", registry.Get().Claude.VersionFields.CLIVersion)
	assert.NotEqual(t, initial, registry.Get())
}

func TestRegistryDefaultCodexIdentityUsesMinimumSupportedVersion(t *testing.T) {
	registry := NewRegistry()

	assert.Equal(t, MinimumCodexCLIVersion, registry.Get().Codex.VersionFields.CLIVersion)
	assert.Contains(t, registry.Get().Codex.Headers["User-Agent"], "codex_cli_rs/"+MinimumCodexCLIVersion)
}
