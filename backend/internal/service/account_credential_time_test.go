package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetCredentialAsTimeParsesFractionalRFC3339(t *testing.T) {
	t.Parallel()

	account := &Account{Credentials: map[string]any{
		"expires_at": "2026-07-16T21:49:08.000Z",
	}}
	got := account.GetCredentialAsTime("expires_at")
	require.NotNil(t, got)
	require.True(t, got.Equal(time.Date(2026, 7, 16, 21, 49, 8, 0, time.UTC)))
}

func TestGetCredentialAsTimeParsesPlainRFC3339(t *testing.T) {
	t.Parallel()

	account := &Account{Credentials: map[string]any{
		"expires_at": "2026-07-16T21:49:08Z",
	}}
	got := account.GetCredentialAsTime("expires_at")
	require.NotNil(t, got)
	require.True(t, got.Equal(time.Date(2026, 7, 16, 21, 49, 8, 0, time.UTC)))
}

func TestNormalizeGrokOAuthCredentialsNormalizesFractionalExpiresAt(t *testing.T) {
	t.Parallel()

	out := NormalizeGrokOAuthCredentials(map[string]any{
		"access_token":  "tok",
		"refresh_token": "rt",
		"expires_at":    "2026-07-16T21:49:08.000Z",
		"base_url":      "https://api.x.ai/v1",
	})
	require.Equal(t, "2026-07-16T21:49:08Z", out["expires_at"])
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1", out["base_url"])
}
