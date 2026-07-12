package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateClaudeOAuthBoundTransport(t *testing.T) {
	pool := &OAuthPool{
		ID:             11,
		EgressRouteID:  9,
		AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
	}
	policy, err := NewClaudeOAuthBoundTransportPolicy(pool, "http://127.0.0.1:8080")
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(WithClaudeOAuthBoundTransport(t.Context(), policy), http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", nil)
	require.NoError(t, err)
	resolved, bound, err := ValidateClaudeOAuthBoundTransport(req, "http://127.0.0.1:8080")
	require.NoError(t, err)
	require.True(t, bound)
	require.Equal(t, policy, resolved)

	_, _, err = ValidateClaudeOAuthBoundTransport(req, "")
	require.ErrorIs(t, err, ErrClaudeOAuthBoundTransport)
	_, _, err = ValidateClaudeOAuthBoundTransport(req, "http://127.0.0.1:9090")
	require.ErrorIs(t, err, ErrClaudeOAuthBoundTransport)

	disallowed, err := http.NewRequestWithContext(WithClaudeOAuthBoundTransport(t.Context(), policy), http.MethodPost, "https://example.com/v1/messages", nil)
	require.NoError(t, err)
	_, _, err = ValidateClaudeOAuthBoundTransport(disallowed, "http://127.0.0.1:8080")
	require.ErrorIs(t, err, ErrClaudeOAuthBoundTransport)
}

func TestClaudeOAuthBoundTransportRejectsUnexpectedQueryAndRedirect(t *testing.T) {
	policy, err := NewClaudeOAuthBoundTransportPolicy(&OAuthPool{
		ID: 11, EgressRouteID: 9, AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
	}, "socks5://127.0.0.1:1080")
	require.NoError(t, err)

	unexpectedQuery, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?redirect=https://example.com", nil)
	require.NoError(t, err)
	require.False(t, policy.AllowsURL(unexpectedQuery.URL))

	redirect, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/other", nil)
	require.NoError(t, err)
	require.False(t, policy.AllowsURL(redirect.URL))
}
