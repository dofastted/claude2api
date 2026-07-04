//go:build unit

package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fableRateLimitCall struct {
	accountID int64
	scope     string
	resetAt   time.Time
	reason    string
}

type fableRateLimitRepo struct {
	mockAccountRepoForGemini
	setRateLimitedCalls      int
	tempUnschedulableCalls   int
	updateSessionWindowCalls int
	modelRateLimitCalls      []fableRateLimitCall
}

func (r *fableRateLimitRepo) SetRateLimited(_ context.Context, _ int64, _ time.Time) error {
	r.setRateLimitedCalls++
	return nil
}

func (r *fableRateLimitRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedulableCalls++
	return nil
}

func (r *fableRateLimitRepo) UpdateSessionWindow(_ context.Context, _ int64, _, _ *time.Time, _ string) error {
	r.updateSessionWindowCalls++
	return nil
}

func (r *fableRateLimitRepo) SetModelRateLimit(_ context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	call := fableRateLimitCall{accountID: id, scope: scope, resetAt: resetAt}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, call)

	if r.accountsByID != nil {
		if account := r.accountsByID[id]; account != nil {
			if account.Extra == nil {
				account.Extra = map[string]any{}
			}
			rawLimits, _ := account.Extra[modelRateLimitsKey].(map[string]any)
			if rawLimits == nil {
				rawLimits = map[string]any{}
			}
			rawLimits[scope] = map[string]any{
				"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
				"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
			}
			account.Extra[modelRateLimitsKey] = rawLimits
		}
	}
	return nil
}

func TestRateLimitService_Handle429_FableUsesModelRateLimitBucket(t *testing.T) {
	resetAt := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
	headers.Set("anthropic-ratelimit-unified-5h-reset", time.Now().Add(5*time.Hour).Format("150405"))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.01")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(resetAt))

	account := &Account{
		ID:          42,
		Name:        "fable-oauth",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusTooManyRequests),
					"keywords":         []any{"models per week"},
					"duration_minutes": float64(30),
				},
			},
		},
		Extra: map[string]any{},
	}
	repo := &fableRateLimitRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}}
	svc := &RateLimitService{accountRepo: repo}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		headers,
		[]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"You have exceeded the claude-fable-5 models per week limit"}}`),
		"claude-fable-5",
	)

	require.False(t, shouldDisable)
	require.Equal(t, 0, repo.setRateLimitedCalls, "Fable 429 must not write account-level rate limit")
	require.Equal(t, 0, repo.tempUnschedulableCalls, "Fable 429 must not temp-unschedule the account")
	require.Equal(t, 0, repo.updateSessionWindowCalls, "Fable weekly limit must not rewrite the shared 5h session window")
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, fableRateLimitCall{accountID: account.ID, scope: claudeFableRateLimitModelKey, resetAt: repo.modelRateLimitCalls[0].resetAt, reason: repo.modelRateLimitCalls[0].reason}, repo.modelRateLimitCalls[0])
	require.WithinDuration(t, resetAt, repo.modelRateLimitCalls[0].resetAt, time.Second)
	require.Contains(t, repo.modelRateLimitCalls[0].reason, string(ClaudeRateLimitTypeFableWeekly))

	require.True(t, account.Schedulable)
	require.Nil(t, account.RateLimitResetAt)
	require.True(t, account.IsSchedulable())
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
	require.True(t, account.IsSchedulableForModel("claude-opus-4-8"))
}

func TestRateLimitService_Handle429_FableMappingUsesModelRateLimitBucket(t *testing.T) {
	resetAt := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", formatUnix(resetAt))

	account := &Account{
		ID:          43,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"claude-latest": "claude-fable-5"},
		},
		Extra: map[string]any{},
	}
	repo := &fableRateLimitRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}}
	svc := &RateLimitService{accountRepo: repo}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, []byte(`{"error":{"message":"rate limited"}}`), "claude-latest")

	require.Equal(t, 0, repo.setRateLimitedCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, claudeFableRateLimitModelKey, repo.modelRateLimitCalls[0].scope)
	require.False(t, account.IsSchedulableForModel("claude-latest"))
	require.True(t, account.IsSchedulableForModel("claude-opus-4-8"))
}

func TestAccountFableRateLimit_ExpiredDoesNotBlockFableOrAccount(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	account := &Account{
		Platform:    PlatformAnthropic,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				claudeFableRateLimitModelKey: map[string]any{
					"rate_limit_reset_at": past,
				},
			},
		},
	}

	require.True(t, account.IsSchedulable())
	require.True(t, account.IsSchedulableForModel("claude-fable-5"))
	require.True(t, account.IsSchedulableForModel("claude-opus-4-8"))
	require.Zero(t, account.GetRateLimitRemainingTime("claude-fable-5"))
}

func TestClassifyClaudeRateLimit_FableWeekly(t *testing.T) {
	svc := &RateLimitService{}
	rateLimitType, cooldown := svc.classifyClaudeRateLimit(
		http.StatusTooManyRequests,
		http.Header{},
		[]byte(`{"error":{"message":"You have exceeded the claude-fable-5 models per week limit"}}`),
	)

	require.Equal(t, ClaudeRateLimitTypeFableWeekly, rateLimitType)
	require.Equal(t, 168*time.Hour, cooldown)
}

func formatUnix(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}
