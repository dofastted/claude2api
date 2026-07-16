package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokTestHTTPUpstream struct {
	lastReq *http.Request
	status  int
	body    string
}

func (u *grokTestHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, concurrency, nil)
}

func (u *grokTestHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, tlsProfile *tlsfingerprint.Profile) (*http.Response, error) {
	u.lastReq = req
	status := u.status
	if status == 0 {
		status = http.StatusOK
	}
	body := u.body
	if body == "" {
		body = "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestAccountTestService_GrokRoutesToCLIChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &grokTestHTTPUpstream{}
	svc := &AccountTestService{
		httpUpstream:        upstream,
		tlsFPProfileService: &TLSFingerprintProfileService{},
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
	}
	account := &Account{
		ID:       2831,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "tok-123",
			"base_url":     "https://cli-chat-proxy.grok.com/v1",
			"auth_kind":    "oauth",
		},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)

	err := svc.testGrokAccountConnection(c, account, "grok-4.5", "hi")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer tok-123", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "xai-grok-cli", upstream.lastReq.Header.Get("X-XAI-Token-Auth"))
	require.Contains(t, strings.ToLower(upstream.lastReq.Header.Get("User-Agent")), "grok")
	require.Contains(t, rec.Body.String(), "test_start")
	require.NotContains(t, rec.Body.String(), "api.anthropic.com")
}
