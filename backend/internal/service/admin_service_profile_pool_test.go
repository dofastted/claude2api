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
	t.Run("anthropic oauth gets frozen v2 claude pool", func(t *testing.T) {
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
		// v2: 固定 3 OS 槽位冻结，容量与并发解耦。
		require.True(t, pool.IsV2())
		require.Equal(t, 3, pool.Capacity)
		require.Len(t, pool.Slots, 3)
		for _, slot := range pool.Slots {
			require.Equal(t, EnvironmentProfileSlotBound, slot.State)
			require.NotNil(t, slot.Profile)
			require.NotEmpty(t, slot.Profile.DeviceID)
			require.Equal(t, claudeEnvironmentProfileSourceSimulated, slot.Profile.Source)
		}
		require.NotContains(t, account.Extra, claudeSingleEnvironmentKey)
		require.NotContains(t, account.Extra, claudeEnvironmentProfileLockedKey)
	})

	t.Run("openai oauth gets frozen v2 codex pool", func(t *testing.T) {
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
		require.True(t, pool.IsV2())
		require.Equal(t, 3, pool.Capacity)
		require.Len(t, pool.Slots, 3)
		for _, slot := range pool.Slots {
			require.Equal(t, EnvironmentProfileSlotBound, slot.State)
			require.NotNil(t, slot.Profile)
			require.NotEmpty(t, slot.Profile.SessionSeed)
			require.Equal(t, codexEnvironmentProfileSourceSimulated, slot.Profile.Source)
		}
		require.NotContains(t, account.Extra, codexSingleEnvironmentKey)
		require.NotContains(t, account.Extra, codexEnvironmentProfileLockedKey)
	})

	t.Run("openai oauth codex tier still gets fixed 3 v2 slots", func(t *testing.T) {
		repo := &createAccountProfilePoolRepo{}
		svc := &adminServiceImpl{accountRepo: repo}

		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name:                 "codex-pro20",
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeOAuth,
			Credentials:          map[string]any{"access_token": "token", "plan_type": "pro20"},
			Concurrency:          3,
			SkipDefaultGroupBind: true,
		})

		require.NoError(t, err)
		pool, err := DecodeCodexEnvironmentProfilePool(account.Extra[codexEnvironmentProfilePoolKey])
		require.NoError(t, err)
		require.NotNil(t, pool)
		// v2: 即使高 tier 也固定 3 槽（容量与 tier 解耦）。
		require.True(t, pool.IsV2())
		require.Equal(t, 3, pool.Capacity)
		require.Len(t, pool.Slots, 3)
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
		existing := newFrozenCodexEnvironmentProfilePool()
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
