package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIWSAccountRepoStub struct {
	AccountRepository
	accounts       map[int64]*Account
	updateExtraIDs []int64
	deleteKeysIDs  []int64
}

func newOpenAIWSAdminService(repo *openAIWSAccountRepoStub, globalEnabled bool) *adminServiceImpl {
	return &adminServiceImpl{
		accountRepo: repo,
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIWS: config.GatewayOpenAIWSConfig{
					ResponsesWebsocketsV2: globalEnabled,
				},
			},
		},
	}
}

func (r *openAIWSAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	copied := *account
	if account.Extra != nil {
		copied.Extra = make(map[string]any, len(account.Extra))
		for key, value := range account.Extra {
			copied.Extra[key] = value
		}
	}
	return &copied, nil
}

func (r *openAIWSAccountRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	account, ok := r.accounts[id]
	if !ok {
		return ErrAccountNotFound
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	for key, value := range updates {
		account.Extra[key] = value
	}
	r.updateExtraIDs = append(r.updateExtraIDs, id)
	return nil
}

func (r *openAIWSAccountRepoStub) DeleteExtraKeys(_ context.Context, id int64, keys []string) error {
	account, ok := r.accounts[id]
	if !ok {
		return ErrAccountNotFound
	}
	for _, key := range keys {
		delete(account.Extra, key)
	}
	r.deleteKeysIDs = append(r.deleteKeysIDs, id)
	return nil
}

func TestEnableAllOpenAIWS(t *testing.T) {
	ctx := context.Background()

	t.Run("oauth account writes target state and corrects false overrides", func(t *testing.T) {
		account := &Account{
			ID:       1,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				"keep": "value",
				"openai_oauth_responses_websockets_v2_enabled": false,
				"openai_oauth_responses_websockets_v2_mode":    OpenAIWSIngressModeOff,
				"responses_websockets_v2_enabled":              false,
				"openai_ws_force_http":                         true,
			},
		}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{1: account}}
		svc := newOpenAIWSAdminService(repo, true)

		require.NoError(t, svc.EnableAllOpenAIWS(ctx, 1))
		require.True(t, account.IsOpenAIResponsesWebSocketV2Enabled())
		require.Equal(t, OpenAIWSIngressModeCtxPool, account.ResolveOpenAIResponsesWebSocketV2Mode(OpenAIWSIngressModeOff))
		require.True(t, account.IsOpenAIWSAllowStoreRecoveryEnabled())
		require.False(t, account.IsOpenAIWSForceHTTPEnabled())
		require.Equal(t, "value", account.Extra["keep"])
		require.Equal(t, []int64{1}, repo.updateExtraIDs)
	})

	t.Run("apikey account writes type-specific target state", func(t *testing.T) {
		account := &Account{
			ID:       2,
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": false,
				"openai_apikey_responses_websockets_v2_mode":    OpenAIWSIngressModeOff,
			},
		}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{2: account}}
		svc := newOpenAIWSAdminService(repo, true)

		require.NoError(t, svc.EnableAllOpenAIWS(ctx, 2))
		require.True(t, account.IsOpenAIResponsesWebSocketV2Enabled())
		require.Equal(t, OpenAIWSIngressModeCtxPool, account.ResolveOpenAIResponsesWebSocketV2Mode(OpenAIWSIngressModeOff))
	})

	t.Run("global disabled rejects account override", func(t *testing.T) {
		account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{3: account}}
		svc := newOpenAIWSAdminService(repo, false)

		err := svc.EnableAllOpenAIWS(ctx, 3)
		require.Error(t, err)
		require.ErrorContains(t, err, "global gateway.openai_ws.responses_websockets_v2 is disabled")
		require.Empty(t, repo.updateExtraIDs)
	})

	t.Run("non openai account rejects", func(t *testing.T) {
		account := &Account{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{4: account}}
		svc := newOpenAIWSAdminService(repo, true)

		err := svc.EnableAllOpenAIWS(ctx, 4)
		require.Error(t, err)
		require.ErrorContains(t, err, "account is not OpenAI platform")
	})

	t.Run("reset deletes ws fields and keeps unrelated extra", func(t *testing.T) {
		account := &Account{
			ID:       5,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				"keep": "value",
			},
		}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{5: account}}
		svc := newOpenAIWSAdminService(repo, true)

		require.NoError(t, svc.EnableAllOpenAIWS(ctx, 5))
		require.NoError(t, svc.ResetOpenAIWS(ctx, 5))
		require.False(t, account.IsOpenAIResponsesWebSocketV2Enabled())
		require.Equal(t, OpenAIWSIngressModeOff, account.ResolveOpenAIResponsesWebSocketV2Mode(OpenAIWSIngressModeOff))
		require.False(t, account.IsOpenAIWSAllowStoreRecoveryEnabled())
		require.False(t, account.IsOpenAIWSForceHTTPEnabled())
		require.Equal(t, map[string]any{"keep": "value"}, account.Extra)
		require.Equal(t, []int64{5}, repo.deleteKeysIDs)
	})

	t.Run("enable and reset are idempotent", func(t *testing.T) {
		account := &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		repo := &openAIWSAccountRepoStub{accounts: map[int64]*Account{6: account}}
		svc := newOpenAIWSAdminService(repo, true)

		require.NoError(t, svc.EnableAllOpenAIWS(ctx, 6))
		firstExtra := copyOpenAIWSTestMap(account.Extra)
		require.NoError(t, svc.EnableAllOpenAIWS(ctx, 6))
		require.Equal(t, firstExtra, account.Extra)

		require.NoError(t, svc.ResetOpenAIWS(ctx, 6))
		require.NoError(t, svc.ResetOpenAIWS(ctx, 6))
		require.Empty(t, account.Extra)
	})
}

func copyOpenAIWSTestMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
