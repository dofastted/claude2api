package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOAuthPoolNormalizesStrictPolicy(t *testing.T) {
	pool := &OAuthPool{
		Name:          " strict ",
		EgressRouteID: 7,
		AllowedOrigins: []string{
			"https://api.anthropic.com/v1/messages/count_tokens",
			"https://api.anthropic.com/v1/messages",
			"https://api.anthropic.com/v1/messages",
		},
		AllowedModels: []string{"claude-sonnet-*", " claude-opus-4-6 ", "claude-sonnet-*"},
	}

	require.NoError(t, ValidateOAuthPool(pool))
	require.Equal(t, "strict", pool.Name)
	require.Equal(t, OAuthPoolProviderClaude, pool.Provider)
	require.Equal(t, OAuthPoolModeShadow, pool.Mode)
	require.Equal(t, ClaudeOAuthSessionTTLSeconds, pool.SessionTTLSeconds)
	require.Equal(t, []string{
		"https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/v1/messages/count_tokens",
	}, pool.AllowedOrigins)
	require.Equal(t, []string{"claude-opus-4-6", "claude-sonnet-*"}, pool.AllowedModels)
	require.True(t, pool.SupportsModel("claude-sonnet-4-6"))
	require.False(t, pool.SupportsModel("claude-haiku-4-5"))
}

func TestValidateOAuthPoolRejectsUnapprovedOriginAndMutableTTL(t *testing.T) {
	base := OAuthPool{
		Name:              "strict",
		EgressRouteID:     7,
		AllowedOrigins:    []string{"https://api.anthropic.com/v1/messages"},
		AllowedModels:     []string{"claude-sonnet-4-6"},
		SessionTTLSeconds: ClaudeOAuthSessionTTLSeconds,
	}

	withOrigin := base
	withOrigin.AllowedOrigins = []string{"https://attacker.example/v1/messages"}
	require.ErrorIs(t, ValidateOAuthPool(&withOrigin), ErrOAuthPoolInvalid)

	withTTL := base
	withTTL.SessionTTLSeconds = 7200
	require.ErrorIs(t, ValidateOAuthPool(&withTTL), ErrOAuthPoolInvalid)

	withEnforce := base
	withEnforce.Mode = OAuthPoolModeEnforce
	require.ErrorIs(t, ValidateOAuthPool(&withEnforce), ErrOAuthPoolInvalid)
}

func TestValidateOAuthPoolAccountRestrictsCredentialAndEgress(t *testing.T) {
	pool := &OAuthPool{EgressRouteID: 7}
	matchingProxy := int64(7)
	mismatchedProxy := int64(8)

	require.NoError(t, ValidateOAuthPoolAccount(pool, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		ProxyID:  &matchingProxy,
	}))
	require.NoError(t, ValidateOAuthPoolAccount(pool, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}))
	require.ErrorIs(t, ValidateOAuthPoolAccount(pool, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
	}), ErrOAuthPoolCredentialInvalid)
	require.ErrorIs(t, ValidateOAuthPoolAccount(pool, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		ProxyID:  &mismatchedProxy,
	}), ErrOAuthPoolCredentialInvalid)
}
