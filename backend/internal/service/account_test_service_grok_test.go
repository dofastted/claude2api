//go:build unit

package service

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestService_GrokUsesCLIChatCompletionsProbe(t *testing.T) {
	ctx, recorder := newTestContext()
	account := &Account{
		ID:          2815,
		Name:        "grok-heavy",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"base_url":     xai.DefaultCLIBaseURL,
		},
	}
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"pong"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		httpUpstream:      upstream,
		cfg:               &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	err := svc.TestAccountConnection(ctx, account.ID, "grok", "hello", AccountTestModeDefault)

	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, xai.DefaultCLIBaseURL+"/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, xai.DefaultCLIUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, xai.DefaultCLITokenAuth, upstream.lastReq.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, xai.DefaultCLIClientIdentifier, upstream.lastReq.Header.Get("x-grok-client-identifier"))
	require.Equal(t, xai.DefaultCLIClientVersion, upstream.lastReq.Header.Get("x-grok-client-version"))
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Contains(t, recorder.Body.String(), "pong")
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestService_GrokExpiredTokenFailsBeforeUpstreamProbe(t *testing.T) {
	ctx, recorder := newTestContext()
	account := &Account{
		ID:       2816,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "revoked-refresh-token",
			"expires_at":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"base_url":      xai.DefaultCLIBaseURL,
		},
	}
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	provider := NewGrokTokenProvider(repo, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &tokenRefresherStub{
		err: errors.New("invalid_grant: refresh token has been revoked"),
	})
	upstream := &httpUpstreamRecorder{}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: provider,
		httpUpstream:      upstream,
		cfg:               &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	err := svc.TestAccountConnection(ctx, account.ID, "grok", "hello", AccountTestModeDefault)

	require.Error(t, err)
	require.Nil(t, upstream.lastReq)
	require.Contains(t, err.Error(), "Failed to acquire Grok access token")
	require.Contains(t, recorder.Body.String(), "Failed to acquire Grok access token")
	require.NotContains(t, recorder.Body.String(), "api.anthropic.com")
}
