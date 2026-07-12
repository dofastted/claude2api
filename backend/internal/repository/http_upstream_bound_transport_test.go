package repository

import (
	"net/http"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamBoundTransportRejectsGlobalProxyOverride(t *testing.T) {
	policy, err := service.NewClaudeOAuthBoundTransportPolicy(&service.OAuthPool{
		ID: 11, EgressRouteID: 9, AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
	}, "http://pool.proxy:8080")
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(service.WithClaudeOAuthBoundTransport(t.Context(), policy), http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)

	upstream := &httpUpstreamService{cfg: &config.Config{Gateway: config.GatewayConfig{GlobalProxyURL: "http://global.proxy:8888"}}}
	_, err = upstream.enforceClaudeOAuthBoundTransport(req, "http://pool.proxy:8080")
	require.ErrorIs(t, err, service.ErrClaudeOAuthBoundTransport)
}

func TestHTTPUpstreamBoundTransportUsesOnlyPoolProxy(t *testing.T) {
	policy, err := service.NewClaudeOAuthBoundTransportPolicy(&service.OAuthPool{
		ID: 11, EgressRouteID: 9, AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
	}, "http://pool.proxy:8080")
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(service.WithClaudeOAuthBoundTransport(t.Context(), policy), http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)

	upstream := &httpUpstreamService{cfg: &config.Config{}}
	proxyURL, err := upstream.enforceClaudeOAuthBoundTransport(req, "http://pool.proxy:8080")
	require.NoError(t, err)
	require.Equal(t, "http://pool.proxy:8080", proxyURL)

	redirect, err := http.NewRequestWithContext(req.Context(), http.MethodGet, "https://example.com/v1/messages", nil)
	require.NoError(t, err)
	err = upstream.boundAwareRedirectChecker(redirect, []*http.Request{req})
	require.ErrorIs(t, err, service.ErrClaudeOAuthBoundTransport)
}
