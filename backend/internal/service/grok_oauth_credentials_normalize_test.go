package service

import (
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGrokOAuthCredentialsRewritesPublicAPIBaseURL(t *testing.T) {
	out := NormalizeGrokOAuthCredentials(map[string]any{
		"access_token":  "at",
		"refresh_token": "rt",
		"base_url":      "https://api.x.ai/v1",
		"expires_at":    float64(1784233642),
	})
	require.Equal(t, xai.DefaultCLIBaseURL, out["base_url"])
	require.Equal(t, "oauth", out["auth_kind"])
	require.Equal(t, xai.DefaultTokenURL, out["token_endpoint"])
	require.Equal(t, "Bearer", out["token_type"])
	require.Equal(t, time.Unix(1784233642, 0).UTC().Format(time.RFC3339), out["expires_at"])
	headers, ok := out["headers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, xai.DefaultCLITokenAuth, headers["X-XAI-Token-Auth"])
}
