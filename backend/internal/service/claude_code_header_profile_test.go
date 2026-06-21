package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type claudeCodeHeaderProfileAccountRepo struct {
	AccountRepository
	updates map[string]any
}

func (r *claudeCodeHeaderProfileAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

func TestFilterClaudeCodeHeaderProfileWhitelistAndSensitive(t *testing.T) {
	headers := http.Header{}
	headers.Set("User-Agent", "claude-cli/2.1.22")
	headers.Set("X-App", "claude-cli")
	headers.Set("Anthropic-Client-Sha", "abc123")
	headers.Set("Authorization", "Bearer secret")
	headers.Set("Cookie", "session=secret")
	headers.Set("X-Api-Key", "secret")
	headers.Set("X-Other", "ignored")

	got := filterClaudeCodeHeaderProfile(headers)

	require.Equal(t, map[string]string{
		"user-agent":           "claude-cli/2.1.22",
		"x-app":                "claude-cli",
		"anthropic-client-sha": "abc123",
	}, got)
}

func TestLearnClaudeCodeHeaderProfilePersistsOAuthAccount(t *testing.T) {
	repo := &claudeCodeHeaderProfileAccountRepo{}
	svc := &GatewayService{accountRepo: repo}
	account := &Account{ID: 42, Name: "claude", Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	headers := http.Header{}
	headers.Set("User-Agent", "claude-cli/2.1.22")
	headers.Set("X-App", "claude-cli")
	headers.Set("Authorization", "Bearer secret")

	svc.learnClaudeCodeHeaderProfile(context.Background(), account, headers)

	raw, ok := repo.updates[claudeCodeHeaderProfileKey]
	require.True(t, ok)
	profile, ok := raw.(ClaudeCodeHeaderProfile)
	require.True(t, ok)
	require.Equal(t, "claude-cli/2.1.22", profile.Headers["user-agent"])
	require.NotContains(t, profile.Headers, "authorization")
	require.Equal(t, profile, account.Extra[claudeCodeHeaderProfileKey])
}

func TestGetClaudeCodeHeaderProfileRejectsExpiredProfile(t *testing.T) {
	svc := &GatewayService{}
	account := &Account{Extra: map[string]any{
		claudeCodeHeaderProfileKey: ClaudeCodeHeaderProfile{
			Headers:   map[string]string{"user-agent": "claude-cli/2.1.22"},
			UpdatedAt: time.Now().Add(-claudeCodeHeaderProfileMaxAge - time.Hour),
		},
	}}

	require.Nil(t, svc.getClaudeCodeHeaderProfile(account))
}

func TestApplyClaudeCodeHeaderProfileFiltersSensitiveHeaders(t *testing.T) {
	svc := &GatewayService{}
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)
	profile := &ClaudeCodeHeaderProfile{
		Headers: map[string]string{
			"user-agent":    "claude-cli/2.1.22",
			"authorization": "Bearer secret",
			"x-other":       "ignored",
		},
		UpdatedAt: time.Now(),
	}

	svc.applyClaudeCodeHeaderProfile(req, &Account{ID: 42}, profile)

	require.Equal(t, "claude-cli/2.1.22", req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("Authorization"))
	require.Empty(t, req.Header.Get("X-Other"))
}
