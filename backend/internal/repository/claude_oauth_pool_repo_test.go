package repository

import (
	"context"
	"testing"

	"github.com/dofastted/claude2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestClaudeOAuthPoolRepositoryMembershipAndCapsuleActivation(t *testing.T) {
	client := newSecuritySecretTestClient(t)
	ctx := context.Background()
	proxyEntity, err := client.Proxy.Create().
		SetName("strict-egress").
		SetProtocol("http").
		SetHost("127.0.0.1").
		SetPort(8080).
		Save(ctx)
	require.NoError(t, err)
	accountOne, err := client.Account.Create().
		SetName("oauth-one").
		SetPlatform(service.PlatformAnthropic).
		SetType(service.AccountTypeOAuth).
		Save(ctx)
	require.NoError(t, err)
	accountTwo, err := client.Account.Create().
		SetName("oauth-two").
		SetPlatform(service.PlatformAnthropic).
		SetType(service.AccountTypeOAuth).
		Save(ctx)
	require.NoError(t, err)

	repo := NewClaudeOAuthPoolRepository(client)
	pool := &service.OAuthPool{
		Name:           "strict",
		EgressRouteID:  proxyEntity.ID,
		AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
		AllowedModels:  []string{"claude-sonnet-4-6"},
	}
	require.NoError(t, repo.Create(ctx, pool))
	require.Positive(t, pool.ID)

	first := &service.OAuthPoolCredential{PoolID: pool.ID, AccountID: accountOne.ID}
	require.NoError(t, repo.AddCredential(ctx, first))
	second := &service.OAuthPoolCredential{PoolID: pool.ID, AccountID: accountTwo.ID}
	require.NoError(t, repo.AddCredential(ctx, second))

	otherPool := &service.OAuthPool{
		Name:           "other",
		EgressRouteID:  proxyEntity.ID,
		AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
		AllowedModels:  []string{"claude-sonnet-4-6"},
	}
	require.NoError(t, repo.Create(ctx, otherPool))
	require.ErrorIs(t, repo.AddCredential(ctx, &service.OAuthPoolCredential{
		PoolID: otherPool.ID, AccountID: accountOne.ID,
	}), service.ErrOAuthPoolCredentialConflict)

	setOne := &service.OAuthCapsuleSet{
		PoolID:              pool.ID,
		Version:             1,
		CompatibilityDigest: "digest-v1",
		Payload:             map[string]any{"credentials": map[string]any{}},
	}
	require.NoError(t, repo.CreateCapsuleSet(ctx, setOne))
	activated, err := repo.ActivateCapsuleSet(ctx, pool.ID, 1, "digest-v1")
	require.NoError(t, err)
	require.Equal(t, int64(1), activated.ActiveCapsuleSetVersion)
	require.Nil(t, activated.PreviousCapsuleSetVersion)

	setTwo := &service.OAuthCapsuleSet{
		PoolID:              pool.ID,
		Version:             2,
		CompatibilityDigest: "digest-v2",
		Payload:             map[string]any{"credentials": map[string]any{}},
	}
	require.NoError(t, repo.CreateCapsuleSet(ctx, setTwo))
	activated, err = repo.ActivateCapsuleSet(ctx, pool.ID, 2, "digest-v2")
	require.NoError(t, err)
	require.Equal(t, int64(2), activated.ActiveCapsuleSetVersion)
	require.NotNil(t, activated.PreviousCapsuleSetVersion)
	require.Equal(t, int64(1), *activated.PreviousCapsuleSetVersion)
}
