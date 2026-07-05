package service

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAutoresearchProfileContractCodexOAuthEnvironmentOverwrite(t *testing.T) {
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_device_id": "account-device-id",
		},
	}
	profile := &CodexEnvironmentProfile{
		UserAgent:        "codex-profile-ua/1.0",
		Version:          "2026-07-05",
		Originator:       "codex_cli_rs",
		InstallationID:   "profile-installation-id",
		SessionSeed:      "session-seed",
		ConversationSeed: "conversation-seed",
		Timezone:         "America/Los_Angeles",
	}
	req := httptest.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("User-Agent", "client-ua")
	req.Header.Set("originator", "client-originator")

	applyCodexEnvironmentProfile(req, account, profile, CodexProfileApplyOptions{
		APIKeyID:       7,
		PromptCacheKey: "prompt-cache-key",
		WSBetaValue:    "responses=v2",
		AcceptLanguage: "en-US",
	})

	require.Equal(t, "codex-profile-ua/1.0", req.Header.Get("User-Agent"))
	require.Equal(t, "2026-07-05", req.Header.Get("Version"))
	require.Equal(t, "codex_cli_rs", req.Header.Get("originator"))
	require.Equal(t, "responses=v2", req.Header.Get("OpenAI-Beta"))
	require.Equal(t, "en-US", req.Header.Get("accept-language"))
	require.Equal(t, "account-device-id", req.Header.Get("x-codex-installation-id"))
	require.NotEmpty(t, req.Header.Get("session-id"))
	require.NotEmpty(t, req.Header.Get("thread-id"))
	require.Equal(t, req.Header.Get("thread-id"), req.Header.Get("x-client-request-id"))
	require.NotEmpty(t, req.Header.Get("x-codex-window-id"))

	turnMetadata := req.Header.Get("x-codex-turn-metadata")
	require.Equal(t, "America/Los_Angeles", gjson.Get(turnMetadata, "timezone").String())
	require.Equal(t, "turn", gjson.Get(turnMetadata, "request_kind").String())
	require.Equal(t, req.Header.Get("session-id"), gjson.Get(turnMetadata, "session_id").String())
	require.Equal(t, req.Header.Get("thread-id"), gjson.Get(turnMetadata, "thread_id").String())
	require.Equal(t, req.Header.Get("x-codex-window-id"), gjson.Get(turnMetadata, "window_id").String())
}

func TestAutoresearchProfileContractClaudeOAuthEnvironmentOverwrite(t *testing.T) {
	account := &Account{
		ID:       84,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}
	profile := &ClaudeEnvironmentProfile{
		Family:             ClaudeClientFamilyCodeCLI,
		UserAgent:          "claude-profile-ua/1.0",
		XApp:               "cli",
		ClientType:         "claude-code",
		ClientVersion:      "1.2.3",
		Platform:           "darwin",
		Arch:               "arm64",
		Runtime:            "node",
		RuntimeVersion:     "22.0.0",
		Timezone:           "America/Los_Angeles",
		TelemetrySessionID: "telemetry-session",
		TelemetryUserID:    "telemetry-user",
	}
	req := httptest.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("User-Agent", "client-ua")
	req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")

	svc := &GatewayService{}
	svc.applyClaudeEnvironmentProfile(req, account, profile)
	applyClaudeTelemetryContext(profile, account)

	require.Equal(t, "claude-profile-ua/1.0", getHeaderRaw(req.Header, "User-Agent"))
	require.Equal(t, "cli", getHeaderRaw(req.Header, "X-App"))
	require.Equal(t, "claude-code", getHeaderRaw(req.Header, "Anthropic-Client-Type"))
	require.Equal(t, "1.2.3", getHeaderRaw(req.Header, "X-Stainless-Package-Version"))
	require.Equal(t, "darwin", getHeaderRaw(req.Header, "X-Stainless-OS"))
	require.Equal(t, "arm64", getHeaderRaw(req.Header, "X-Stainless-Arch"))
	require.Equal(t, "node", getHeaderRaw(req.Header, "X-Stainless-Runtime"))
	require.Equal(t, "22.0.0", getHeaderRaw(req.Header, "X-Stainless-Runtime-Version"))
	require.Equal(t, "telemetry-session", getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
	require.Empty(t, getHeaderRaw(req.Header, "traceparent"))
	require.Equal(t, "America/Los_Angeles", profile.TelemetryAttributes["timezone"])
}
