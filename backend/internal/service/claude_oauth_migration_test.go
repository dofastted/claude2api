package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyClaudeOAuthCredentialFailureStrictWhitelist(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   ClaudeOAuthMigrationReason
		ok     bool
	}{
		{name: "revoked structured code", status: 401, body: `{"error":{"type":"token_revoked"}}`, want: ClaudeOAuthMigrationCredentialRevoked, ok: true},
		{name: "quota exhausted structured code", status: 429, body: `{"error":{"code":"quota_exhausted"}}`, want: ClaudeOAuthMigrationCredentialExhausted, ok: true},
		{name: "generic rate limit", status: 429, body: `{"error":{"type":"rate_limit_error","message":"quota exhausted maybe"}}`},
		{name: "unknown unauthorized", status: 401, body: `{"error":{"type":"authentication_error"}}`},
		{name: "server error", status: 500, body: `{"error":{"code":"quota_exhausted"}}`},
		{name: "network failure has no body", status: 0, body: ``},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classification, ok := ClassifyClaudeOAuthCredentialFailure(&UpstreamFailoverError{
				StatusCode: test.status, ResponseBody: []byte(test.body), ResponseHeaders: make(http.Header),
			})
			require.Equal(t, test.ok, ok)
			if test.ok {
				require.Equal(t, test.want, classification.Reason)
			}
		})
	}
}

func TestClaudeOAuthMigrationManagerUsesStableRankingAndCAS(t *testing.T) {
	proxyID := int64(9)
	pool := &OAuthPool{ID: 11, Status: OAuthPoolStatusActive, Mode: OAuthPoolModeEnforce, EgressRouteID: proxyID}
	poolRepo := &fakeClaudeOAuthPoolRepository{
		pool: pool,
		credentials: []OAuthPoolCredential{
			{ID: 1, PoolID: 11, AccountID: 101, State: OAuthPoolCredentialAvailable},
			{ID: 2, PoolID: 11, AccountID: 102, State: OAuthPoolCredentialAvailable},
		},
	}
	accountRepo := &fakeClaudeOAuthAccountReader{accounts: []Account{
		{ID: 101, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &proxyID},
		{ID: 102, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &proxyID},
	}}
	binding := &ClaudeOAuthBinding{
		PoolID: 11, BindingHash: "stable-binding", AccountID: 101, CapsuleSetVersion: 7, CapsuleSlot: 2, Epoch: 0,
	}
	bindingStore := &fakeClaudeOAuthBindingStore{bindings: map[string]*ClaudeOAuthBinding{"stable-binding": binding}}
	selector, err := NewClaudeOAuthPoolSelector(poolRepo, accountRepo, bindingStore, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	manager, err := NewClaudeOAuthMigrationManager(selector, poolRepo, bindingStore)
	require.NoError(t, err)

	result, err := manager.MigrateOnFailure(context.Background(), &ClaudeOAuthSelection{
		Pool: pool, Binding: binding, Account: &accountRepo.accounts[0],
	}, &UpstreamFailoverError{StatusCode: 429, ResponseBody: []byte(`{"error":{"code":"quota_exhausted"}}`)})
	require.NoError(t, err)
	require.True(t, result.Migrated)
	require.Equal(t, int64(102), result.Binding.AccountID)
	require.Equal(t, int64(1), result.Binding.Epoch)
	require.Equal(t, int64(7), result.Binding.CapsuleSetVersion)
	require.Equal(t, 2, result.Binding.CapsuleSlot)
	require.NotNil(t, poolRepo.updatedCredential)
	require.Equal(t, OAuthPoolCredentialExhausted, poolRepo.updatedCredential.State)
}

func TestClaudeOAuthMigrationManagerDoesNotMoveUnknownFailure(t *testing.T) {
	manager := &ClaudeOAuthMigrationManager{}
	result, err := manager.MigrateOnFailure(context.Background(), nil, &UpstreamFailoverError{
		StatusCode: 429, ResponseBody: []byte(`{"error":{"type":"rate_limit_error"}}`),
	})
	require.NoError(t, err)
	require.False(t, result.Migrated)
}
