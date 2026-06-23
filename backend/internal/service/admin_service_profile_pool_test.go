//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type createAccountProfilePoolRepo struct {
	accountRepoStub
	created *Account
}

func (r *createAccountProfilePoolRepo) Create(_ context.Context, account *Account) error {
	r.created = account
	account.ID = 1001
	return nil
}

func TestAdminServiceCreateAccountDefaultEnvironmentProfilePool(t *testing.T) {
	t.Run("anthropic oauth gets empty claude pool", func(t *testing.T) {
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "claude-oauth",
			Platform:             PlatformAnthropic,
			Type:                 AccountTypeOAuth,
			Credentials:          map[string]any{"access_token": "token"},
			Concurrency:          5,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		require.Same(t, repo.created, account)
		pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
		require.NoError(t, err)
		require.NotNil(t, pool)
		require.Equal(t, 5, pool.Capacity)
		require.Len(t, pool.Slots, 5)
		for _, slot := range pool.Slots {
			require.Equal(t, EnvironmentProfileSlotEmpty, slot.State)
			require.Nil(t, slot.Profile)
		}
		require.NotContains(t, account.Extra, claudeSingleEnvironmentKey)
		require.NotContains(t, account.Extra, claudeEnvironmentProfileLockedKey)
	})

	t.Run("openai oauth gets empty codex pool", func(t *testing.T) {
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "codex-oauth",
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeOAuth,
			Credentials:          map[string]any{"access_token": "token"},
			Concurrency:          3,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		pool, err := DecodeCodexEnvironmentProfilePool(account.Extra[codexEnvironmentProfilePoolKey])
		require.NoError(t, err)
		require.NotNil(t, pool)
		require.Equal(t, 3, pool.Capacity)
		require.Len(t, pool.Slots, 3)
		for _, slot := range pool.Slots {
			require.Equal(t, EnvironmentProfileSlotEmpty, slot.State)
			require.Nil(t, slot.Profile)
		}
		require.NotContains(t, account.Extra, codexSingleEnvironmentKey)
		require.NotContains(t, account.Extra, codexEnvironmentProfileLockedKey)
	})

	t.Run("preserves explicit disabled single environment", func(t *testing.T) {
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "disabled",
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeOAuth,
			Credentials:          map[string]any{"access_token": "token"},
			Extra:                map[string]any{codexSingleEnvironmentKey: false},
			Concurrency:          3,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		require.NotContains(t, account.Extra, codexEnvironmentProfilePoolKey)
		require.Equal(t, false, account.Extra[codexSingleEnvironmentKey])
	})

	t.Run("does not overwrite existing pool", func(t *testing.T) {
		existing := newCodexEnvironmentProfilePool(2)
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "existing",
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeOAuth,
			Credentials:          map[string]any{"access_token": "token"},
			Extra:                map[string]any{codexEnvironmentProfilePoolKey: existing},
			Concurrency:          8,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		require.Same(t, existing, account.Extra[codexEnvironmentProfilePoolKey])
	})

	t.Run("non oauth target does not get pool", func(t *testing.T) {
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "apikey",
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeAPIKey,
			Credentials:          map[string]any{"api_key": "key"},
			Concurrency:          3,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		require.Nil(t, account.Extra)
	})
}
