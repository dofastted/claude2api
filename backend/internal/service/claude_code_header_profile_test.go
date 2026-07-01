package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
