//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokAccountCredentialsUsesCLITemplateAndFreeDefaults(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(6 * time.Hour).Unix()
	credentials := (&GrokOAuthService{}).BuildAccountCredentials(&GrokTokenInfo{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    21600,
		ExpiresAt:    expiresAt,
		ClientID:     xai.DefaultClientID,
		Scope:        xai.DefaultScope,
		Email:        "grok@example.com",
	})

	require.Equal(t, xai.DefaultCLIBaseURL, credentials["base_url"])
	require.Equal(t, xai.DefaultTokenURL, credentials["token_endpoint"])
	require.Equal(t, "oauth", credentials["auth_kind"])
	require.Equal(t, xai.SubscriptionTierFree, credentials["subscription_tier"])
	require.Equal(t, xai.FreeContextLimitTokens, credentials["context_limit_tokens"])
	require.Equal(t, xai.FreeUsageRefreshSeconds, credentials["usage_refresh_seconds"])
	require.Equal(t, false, credentials["usage_endpoint_available"])

	headers, ok := credentials["headers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, xai.DefaultCLIUserAgent, headers["User-Agent"])
	require.Equal(t, xai.DefaultCLITokenAuth, headers["X-XAI-Token-Auth"])
	require.Equal(t, xai.DefaultCLIClientIdentifier, headers["x-grok-client-identifier"])
}

func TestBuildGrokAccountCredentialsMapsHeavyUsageEndpoint(t *testing.T) {
	t.Parallel()

	credentials := (&GrokOAuthService{}).BuildAccountCredentials(&GrokTokenInfo{
		AccessToken:      "access-token",
		ExpiresIn:        21600,
		ExpiresAt:        time.Now().Add(6 * time.Hour).Unix(),
		SubscriptionTier: "supergrok-heavy",
	})

	require.Equal(t, xai.SubscriptionTierHeavy, credentials["subscription_tier"])
	require.Equal(t, true, credentials["usage_endpoint_available"])
	require.Equal(t, int64(0), credentials["context_limit_tokens"])
	require.Equal(t, 0, credentials["usage_refresh_seconds"])
}
