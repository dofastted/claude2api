//go:build unit

package xai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionProfileFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		raw          string
		wantTier     string
		wantContext  int64
		wantRefresh  int
		wantUsageAPI bool
	}{
		{name: "missing defaults to free", raw: "", wantTier: SubscriptionTierFree, wantContext: FreeContextLimitTokens, wantRefresh: FreeUsageRefreshSeconds},
		{name: "free alias", raw: "free-tier", wantTier: SubscriptionTierFree, wantContext: FreeContextLimitTokens, wantRefresh: FreeUsageRefreshSeconds},
		{name: "super", raw: "super", wantTier: SubscriptionTierSuper, wantUsageAPI: true},
		{name: "supergrok alias", raw: "SuperGrok", wantTier: SubscriptionTierSuper, wantUsageAPI: true},
		{name: "heavy", raw: "heavy", wantTier: SubscriptionTierHeavy, wantUsageAPI: true},
		{name: "supergrok heavy alias", raw: "supergrok-heavy", wantTier: SubscriptionTierHeavy, wantUsageAPI: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			profile, ok := SubscriptionProfileFor(tt.raw)
			require.True(t, ok)
			require.Equal(t, tt.wantTier, profile.Tier)
			require.Equal(t, tt.wantContext, profile.ContextLimitTokens)
			require.Equal(t, tt.wantRefresh, profile.UsageRefreshSeconds)
			require.Equal(t, tt.wantUsageAPI, profile.UsageEndpointAvailable)
		})
	}
}

func TestSubscriptionProfileForRejectsUnknownTier(t *testing.T) {
	t.Parallel()

	_, ok := SubscriptionProfileFor("enterprise-custom")
	require.False(t, ok)
}

func TestDefaultCLICredentialHeadersMatchPagerTemplate(t *testing.T) {
	t.Parallel()

	headers := DefaultCLICredentialHeaders()
	require.Equal(t, DefaultCLIUserAgent, headers["User-Agent"])
	require.Equal(t, DefaultCLITokenAuth, headers["X-XAI-Token-Auth"])
	require.Equal(t, DefaultCLIClientIdentifier, headers["x-grok-client-identifier"])
	require.Equal(t, DefaultCLIClientVersion, headers["x-grok-client-version"])
	require.NotContains(t, headers, "x-authenticateresponse")
}
