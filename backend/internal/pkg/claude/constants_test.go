package claude

import (
	"testing"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/require"
)

func TestGetHeadersUsesRegistryAndClones(t *testing.T) {
	registry := clientidentity.NewRegistry()
	registry.Swap(&clientidentity.Snapshots{
		Claude: clientidentity.ClaudeSnapshot{
			Headers: map[string]string{
				"User-Agent":                  "claude-cli/9.8.7 (external, cli)",
				"X-Stainless-Package-Version": "9.8.7",
			},
		},
	})

	headers := GetHeaders(registry)
	require.Equal(t, "claude-cli/9.8.7 (external, cli)", headers["User-Agent"])

	headers["User-Agent"] = "mutated"
	require.Equal(t, "claude-cli/9.8.7 (external, cli)", GetHeaders(registry)["User-Agent"])
}

func TestGetHeadersFallsBackToDefaultHeaders(t *testing.T) {
	headers := GetHeaders(nil)
	require.Equal(t, DefaultHeaders["User-Agent"], headers["User-Agent"])

	headers["User-Agent"] = "mutated"
	require.Equal(t, DefaultHeaders["User-Agent"], GetHeaders(nil)["User-Agent"])

	registry := clientidentity.NewRegistry()
	registry.Swap(&clientidentity.Snapshots{})
	require.Equal(t, DefaultHeaders["User-Agent"], GetHeaders(registry)["User-Agent"])
}

func TestDefaultModelsContainsClaudeSonnet5(t *testing.T) {
	for _, model := range DefaultModels {
		if model.ID == "claude-sonnet-5" {
			require.Equal(t, "Claude Sonnet 5", model.DisplayName)
			return
		}
	}

	t.Fatal("expected claude-sonnet-5 in default models")
}
