//go:build unit

package service

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/claude"
	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestService_CreateClaudeCodeTestPayloadBytesIncludesSignedBillingBlock(t *testing.T) {
	svc := &AccountTestService{identityRegistry: clientidentity.NewRegistry()}

	body, err := svc.createClaudeCodeTestPayloadBytes("claude-sonnet-4-6")
	require.NoError(t, err)

	billingText := gjson.GetBytes(body, "system.0.text").String()
	require.Contains(t, billingText, "x-anthropic-billing-header:")
	require.Contains(t, billingText, "cc_version="+claude.CLICurrentVersion+".")
	require.Contains(t, billingText, "cc_entrypoint=cli")
	require.NotContains(t, billingText, "cch=00000")
	require.Regexp(t, `cch=[0-9a-f]{5};`, billingText)
	require.Equal(t, claudeCodeSystemPrompt, gjson.GetBytes(body, "system.1.text").String())
	require.True(t, gjson.GetBytes(body, "system.2.cache_control").Exists())

	metadataUserID := gjson.GetBytes(body, "metadata.user_id").String()
	require.NotEmpty(t, metadataUserID)
	require.NotNil(t, ParseMetadataUserID(metadataUserID))
}

func TestAccountTestService_ClaudeConnectionUsesSignedClaudeCodePayloadAndHeaders(t *testing.T) {
	ctx, recorder := newTestContext()
	resp := newJSONResponse(http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "oauth-token",
		},
	}
	repo := &openAIAccountTestRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	svc := &AccountTestService{
		accountRepo:      repo,
		httpUpstream:     upstream,
		identityRegistry: clientidentity.NewRegistry(),
	}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.NoError(t, err)
	require.Contains(t, recorder.Body.String(), "test_complete")
	require.Len(t, upstream.requests, 1)

	req := upstream.requests[0]
	body := readRequestBodyForTest(t, req)
	billingText := gjson.GetBytes(body, "system.0.text").String()
	require.Contains(t, billingText, "x-anthropic-billing-header:")
	require.NotContains(t, billingText, "cch=00000")
	require.Regexp(t, `cch=[0-9a-f]{5};`, billingText)
	require.Equal(t, "Bearer oauth-token", getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "application/json", getHeaderRaw(req.Header, "Accept"))
	require.Equal(t, "cli", getHeaderRaw(req.Header, "x-app"))
	require.Equal(t, claude.DefaultHeaders["X-Stainless-Package-Version"], getHeaderRaw(req.Header, "X-Stainless-Package-Version"))
	require.Equal(t, "stream", getHeaderRaw(req.Header, "x-stainless-helper-method"))
	require.NotEmpty(t, getHeaderRaw(req.Header, "x-client-request-id"))
	require.Equal(t, claude.DefaultBetaHeader, getHeaderRaw(req.Header, "anthropic-beta"))
	parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String())
	require.NotNil(t, parsed)
	require.NotEmpty(t, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	require.Equal(t, parsed.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}

func TestAccountTestService_AnthropicAPIKeyRetriesBearerMessageProbe(t *testing.T) {
	ctx, recorder := newTestContext()
	guardResp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"Your request body appears to have been tampered with. Please use our official distribution platform."}}`)
	successResp := newJSONResponse(http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{guardResp, successResp}}
	account := &Account{
		ID:          45,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "packy-token",
			"base_url": "https://www.packyapi.com",
		},
	}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}, identityRegistry: clientidentity.NewRegistry()}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.NoError(t, err)
	require.Contains(t, recorder.Body.String(), "test_complete")
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[0].URL.String())
	require.Equal(t, claude.DefaultBetaHeader, getHeaderRaw(upstream.requests[1].Header, "anthropic-beta"))
	require.Equal(t, "packy-token", getHeaderRaw(upstream.requests[0].Header, "x-api-key"))
	require.Equal(t, claude.APIKeyBetaHeader, getHeaderRaw(upstream.requests[0].Header, "anthropic-beta"))
	require.Empty(t, getHeaderRaw(upstream.requests[0].Header, "authorization"))
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer packy-token", getHeaderRaw(upstream.requests[1].Header, "authorization"))
	require.Empty(t, getHeaderRaw(upstream.requests[1].Header, "x-api-key"))
}

func TestAccountTestService_AnthropicAPIKeyGuardFallsBackToModels(t *testing.T) {
	ctx, recorder := newTestContext()
	guardResp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"Your request body appears to have been tampered with. Please use our official distribution platform."}}`)
	bearerGuardResp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"We have detected an anomaly in your client. Please use the standard Claude Code client."}}`)
	healthGuardResp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"Your request body appears to have been tampered with. Please use our official distribution platform."}}`)
	healthBearerGuardResp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"We have detected an anomaly in your client. Please use the standard Claude Code client."}}`)
	xAPIKeyModelsResp := newJSONResponse(http.StatusUnauthorized, `{"error":{"message":"未提供令牌"}}`)
	bearerModelsResp := newJSONResponse(http.StatusOK, `{"data":[{"id":"claude-sonnet-4-6"}]}`)
	upstream := &queuedHTTPUpstream{responses: []*http.Response{guardResp, bearerGuardResp, healthGuardResp, healthBearerGuardResp, xAPIKeyModelsResp, bearerModelsResp}}
	account := &Account{
		ID:          43,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "packy-token",
			"base_url": "https://www.packyapi.com",
		},
	}
	repo := &openAIAccountTestRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	svc := &AccountTestService{
		accountRepo:      repo,
		httpUpstream:     upstream,
		cfg:              &config.Config{},
		identityRegistry: clientidentity.NewRegistry(),
	}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.NoError(t, err)
	require.Zero(t, repo.setErrorID)
	require.Len(t, upstream.requests, 6)
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[0].URL.String())
	require.Equal(t, "packy-token", getHeaderRaw(upstream.requests[0].Header, "x-api-key"))
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer packy-token", getHeaderRaw(upstream.requests[1].Header, "authorization"))
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[2].URL.String())
	require.Equal(t, claude.APIKeyHaikuBetaHeader, getHeaderRaw(upstream.requests[2].Header, "anthropic-beta"))
	require.Equal(t, "https://www.packyapi.com/v1/messages?beta=true", upstream.requests[3].URL.String())
	require.Equal(t, claude.HaikuBetaHeader, getHeaderRaw(upstream.requests[3].Header, "anthropic-beta"))
	require.Equal(t, "https://www.packyapi.com/v1/models", upstream.requests[4].URL.String())
	require.Equal(t, "packy-token", getHeaderRaw(upstream.requests[4].Header, "x-api-key"))
	require.Equal(t, "https://www.packyapi.com/v1/models", upstream.requests[5].URL.String())
	require.Equal(t, "Bearer packy-token", getHeaderRaw(upstream.requests[5].Header, "authorization"))
	require.Equal(t, []bool{false, false, false, false, false, false}, upstream.tlsFlags)

	body := recorder.Body.String()
	require.Contains(t, body, strictClaudeCodeProbeExplanation)
	require.Contains(t, body, "Model claude-sonnet-4-6 is available")
	require.Contains(t, body, `"success":true`)
}

func TestAccountTestService_AnthropicPassthroughProbeUsesProfileTLS(t *testing.T) {
	ctx, _ := newTestContext()
	resp := newJSONResponse(http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	account := &Account{
		ID:          46,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "packy-token",
			"base_url": "https://www.packyapi.com",
		},
		Extra: map[string]any{"anthropic_passthrough": true},
	}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}, identityRegistry: clientidentity.NewRegistry()}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.NoError(t, err)
	require.Equal(t, []bool{true}, upstream.tlsFlags)
	body := readRequestBodyForTest(t, upstream.requests[0])
	parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String())
	require.NotNil(t, parsed)
	require.Equal(t, claudeCodeAgentSystemPrompt, gjson.GetBytes(body, "system.0.text").String())
	require.NotContains(t, string(body), "x-anthropic-billing-header")
	require.Equal(t, claudeCodeAttributionDisabledBetaHeader, getHeaderRaw(upstream.requests[0].Header, "anthropic-beta"))
	require.Equal(t, "Bearer packy-token", getHeaderRaw(upstream.requests[0].Header, "authorization"))
	require.Equal(t, "packy-token", getHeaderRaw(upstream.requests[0].Header, "x-api-key"))
	require.Equal(t, parsed.SessionID, getHeaderRaw(upstream.requests[0].Header, "X-Claude-Code-Session-Id"))
}

func TestAccountTestService_PackyAccountUsesClaudeCLIRuntimeProbe(t *testing.T) {
	ctx, recorder := newTestContext()
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env.txt")
	fakeClaude := filepath.Join(tmp, "claude")
	script := "#!/bin/sh\nenv | sort > " + envPath + "\nprintf 'Hi! How can I help you today?\\n'\n"
	require.NoError(t, os.WriteFile(fakeClaude, []byte(script), 0o755))
	upstream := &queuedHTTPUpstream{}
	account := &Account{
		ID:          47,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "packy-token",
			"base_url": "https://www.packyapi.com",
		},
		Extra: map[string]any{},
	}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{ClaudeCLIRuntimeProbe: config.ClaudeCLIRuntimeProbeConfig{
			Enabled:         true,
			BinaryPath:      fakeClaude,
			TimeoutSeconds:  5,
			MaxOutputBytes:  1024,
			AttributionMode: "disabled",
		}},
		identityRegistry: clientidentity.NewRegistry(),
	}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.NoError(t, err)
	require.Empty(t, upstream.requests)
	body := recorder.Body.String()
	require.Contains(t, body, "Running Claude Code CLI runtime probe")
	require.Contains(t, body, claudeCLIRuntimeProbeOKText)
	require.Contains(t, body, `"success":true`)
	envBytes, err := os.ReadFile(envPath)
	require.NoError(t, err)
	envText := string(envBytes)
	require.Contains(t, envText, "ANTHROPIC_BASE_URL=https://www.packyapi.com")
	require.Contains(t, envText, "ANTHROPIC_AUTH_TOKEN=packy-token")
	require.Contains(t, envText, "ANTHROPIC_API_KEY=packy-token")
	require.Contains(t, envText, "CLAUDE_CODE_ATTRIBUTION_HEADER=0")
	require.NotContains(t, envText, "HOME=/mnt/x/project/sub2api")
	require.False(t, strings.Contains(body, "packy-token"))
}

func TestClaudeCLIRuntimeProbeEnvIsolatesAmbientClaudeAndProxySettings(t *testing.T) {
	env := claudeCLIRuntimeProbeEnv([]string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=/real/config",
		"CLAUDE_CODE_OAUTH_TOKEN=ambient-token",
		"ANTHROPIC_MODEL=ambient-model",
		"NODE_OPTIONS=--require=/tmp/hook.js",
		"HTTPS_PROXY=http://ambient-proxy",
		"NO_PROXY=packyapi.com",
	}, "/tmp/probe-home", "https://www.packyapi.com", "packy-token", "disabled", "http://account-proxy")
	envText := strings.Join(env, "\n")

	require.Contains(t, envText, "PATH=/usr/bin")
	require.Contains(t, envText, "HOME=/tmp/probe-home")
	require.Contains(t, envText, "HTTPS_PROXY=http://account-proxy")
	require.Contains(t, envText, "HTTP_PROXY=http://account-proxy")
	require.Contains(t, envText, "ALL_PROXY=http://account-proxy")
	require.NotContains(t, envText, "/real/config")
	require.NotContains(t, envText, "ambient-token")
	require.NotContains(t, envText, "ambient-model")
	require.NotContains(t, envText, "/tmp/hook.js")
	require.NotContains(t, envText, "http://ambient-proxy")
	require.NotContains(t, envText, "NO_PROXY=")
}

func TestIsPackyAPIAnthropicAccountRequiresPackyHostname(t *testing.T) {
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://www.packyapi.com"}}
	require.True(t, isPackyAPIAnthropicAccount(account))

	account.Credentials["base_url"] = "https://packyapi.com.evil.example"
	require.False(t, isPackyAPIAnthropicAccount(account))
}
