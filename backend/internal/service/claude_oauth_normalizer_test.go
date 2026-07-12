package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeClaudeOAuthRequestBodyMovesSystemToBoundedFirstUserMessage(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","system":"follow the user format","messages":[{"role":"user","content":"hello"}]}`)
	normalized, changed, err := NormalizeClaudeOAuthRequestBody(body)
	require.NoError(t, err)
	require.True(t, changed)

	var envelope struct {
		System   json.RawMessage   `json:"system"`
		Messages []json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(normalized, &envelope))
	require.Empty(t, envelope.System)
	require.Len(t, envelope.Messages, 2)
	var firstMessage struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(envelope.Messages[0], &firstMessage))
	require.Equal(t, "user", firstMessage.Role)
	require.Len(t, firstMessage.Content, 3)
	require.Equal(t, claudeOAuthSystemBoundaryStart, firstMessage.Content[0].Text)
	require.Equal(t, "follow the user format", firstMessage.Content[1].Text)
	require.Equal(t, claudeOAuthSystemBoundaryEnd, firstMessage.Content[2].Text)
	require.NotContains(t, string(normalized), `"role":"assistant"`)
}

func TestNormalizeClaudeOAuthRequestBodyPreservesSystemContentBlocks(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"one","cache_control":{"type":"ephemeral"}},{"type":"text","text":"two"}],"messages":[]}`)
	normalized, changed, err := NormalizeClaudeOAuthRequestBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(normalized), `"cache_control":{"type":"ephemeral"}`)
	require.NotContains(t, string(normalized), `"system"`)
	require.True(t, strings.Index(string(normalized), claudeOAuthSystemBoundaryStart) < strings.Index(string(normalized), "one"))
}

func TestNormalizeClaudeOAuthRequestBodyLeavesRequestsWithoutSystemUntouched(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	normalized, changed, err := NormalizeClaudeOAuthRequestBody(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, normalized)
}

func TestFinalizeClaudeOAuthCapsuleRequestRebuildsHeaders(t *testing.T) {
	set, err := BuildClaudeOAuthCapsuleSet(11, 1, "2.1.5", "UTC")
	require.NoError(t, err)
	profile, err := ClaudeOAuthCapsuleProfile(set, 1)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-CLIProxy-Session-ID", "must-not-leak")
	req.Header.Set("Traceparent", "must-not-leak")
	req.Header.Set("X-Untrusted", "must-not-leak")
	req.Header.Set("User-Agent", "attacker")

	svc := &GatewayService{identityRegistry: clientidentity.NewRegistry()}
	err = svc.finalizeClaudeOAuthCapsuleRequest(req, &Account{ID: 101}, profile, true, "oauth-2025-04-20", true)
	require.NoError(t, err)
	require.Equal(t, "Bearer secret", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "content-type"))
	require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
	require.Equal(t, "oauth-2025-04-20", getHeaderRaw(req.Header, "anthropic-beta"))
	require.Equal(t, profile.UserAgent, getHeaderRaw(req.Header, "user-agent"))
	require.Equal(t, profile.TelemetrySessionID, getHeaderRaw(req.Header, "x-claude-code-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cliproxy-session-id"))
	require.Empty(t, getHeaderRaw(req.Header, "traceparent"))
	require.Empty(t, getHeaderRaw(req.Header, "x-untrusted"))
}

func TestBuildUpstreamRequestEnforcesClaudeOAuthCapsuleContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	set, err := BuildClaudeOAuthCapsuleSet(11, 1, "2.1.5", "UTC")
	require.NoError(t, err)
	profile, err := ClaudeOAuthCapsuleProfile(set, 2)
	require.NoError(t, err)
	proxyID := int64(9)
	proxy := &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8080}
	selection := &ClaudeOAuthSelection{
		Pool: &OAuthPool{
			ID: 11, Mode: OAuthPoolModeEnforce, EgressRouteID: proxyID,
			AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
		},
		CapsuleSet: set,
		Profile:    profile,
	}
	ctx := WithClaudeOAuthSelection(t.Context(), selection)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "attacker")
	c.Request.Header.Set("X-Untrusted", "must-not-leak")
	c.Request.Header.Set(ClaudeOAuthSignedSessionHeader, "must-not-leak")
	body := []byte(`{"model":"claude-sonnet-4-6","system":"user preference","messages":[{"role":"user","content":"hello"}]}`)

	svc := &GatewayService{identityRegistry: clientidentity.NewRegistry()}
	req, wireBody, err := svc.buildUpstreamRequest(ctx, c, &Account{
		ID: 101, Platform: PlatformAnthropic, Type: AccountTypeOAuth, ProxyID: &proxyID, Proxy: proxy,
	}, body, "oauth-token", "oauth", "claude-sonnet-4-6", false, false)
	require.NoError(t, err)
	require.NotContains(t, string(wireBody), `"system"`)
	require.Contains(t, string(wireBody), claudeOAuthSystemBoundaryStart)
	require.Equal(t, profile.UserAgent, getHeaderRaw(req.Header, "user-agent"))
	require.Empty(t, getHeaderRaw(req.Header, "x-untrusted"))
	require.Empty(t, getHeaderRaw(req.Header, "x-cliproxy-session-id"))
	require.Equal(t, "Bearer oauth-token", getHeaderRaw(req.Header, "authorization"))
}
