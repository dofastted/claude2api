package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/require"
)

func TestIdentityServiceCreatesFingerprintFromRegistryDefaults(t *testing.T) {
	cache := &identityCacheStub{}
	registry := clientidentity.NewRegistry()
	registry.Swap(&clientidentity.Snapshots{
		Claude: clientidentity.ClaudeSnapshot{
			Headers: map[string]string{
				"User-Agent":                  "claude-cli/9.8.7 (external, cli)",
				"X-Stainless-Lang":            "js",
				"X-Stainless-Package-Version": "9.8.7",
				"X-Stainless-OS":              "Linux",
				"X-Stainless-Arch":            "x64",
				"X-Stainless-Runtime":         "node",
				"X-Stainless-Runtime-Version": "v99.0.0",
			},
		},
	})
	svc := NewIdentityService(cache, registry)

	fp, err := svc.GetOrCreateFingerprint(context.Background(), 123, http.Header{})
	require.NoError(t, err)
	require.Equal(t, "claude-cli/9.8.7 (external, cli)", fp.UserAgent)
	require.Equal(t, "9.8.7", fp.StainlessPackageVersion)
	require.Equal(t, "x64", fp.StainlessArch)
	require.Equal(t, "v99.0.0", fp.StainlessRuntimeVersion)
}
