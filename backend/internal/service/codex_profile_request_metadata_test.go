package service

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexProfileRequestMetadataMatchesLatestRequestShape(t *testing.T) {
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_device_id": "install-stable-123",
		},
	}
	profile := &CodexEnvironmentProfile{
		Family:           CodexClientFamilyCLI,
		Source:           "simulated",
		UserAgent:        "codex_cli_rs/0.142.2 (Ubuntu 22.4.0; x86_64) xterm-256color",
		Originator:       "codex_cli_rs",
		Version:          "0.142.2",
		SessionSeed:      "session-seed",
		ConversationSeed: "thread-seed",
		TLSProfile:       "codex-cli-linux",
	}
	require.NoError(t, profile.Validate())
	opts := CodexProfileApplyOptions{APIKeyID: 7, PromptCacheKey: "prompt-key"}

	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	applyCodexEnvironmentProfile(req, account, profile, opts)

	meta := codexProfileRequestMetadataFor(account, profile, opts)
	require.Equal(t, "install-stable-123", meta.InstallationID)
	require.NotEmpty(t, meta.SessionID)
	require.NotEmpty(t, meta.ThreadID)
	require.NotEmpty(t, meta.WindowID)
	require.NotEmpty(t, meta.TurnMetadata)

	require.Equal(t, meta.InstallationID, req.Header.Get("x-codex-installation-id"))
	require.Equal(t, meta.SessionID, req.Header.Get("session-id"))
	require.Equal(t, meta.SessionID, req.Header.Get("session_id"))
	require.Equal(t, meta.ThreadID, req.Header.Get("thread-id"))
	require.Equal(t, meta.ThreadID, req.Header.Get("conversation_id"))
	require.Equal(t, meta.ThreadID, req.Header.Get("x-client-request-id"))
	require.Equal(t, meta.WindowID, req.Header.Get("x-codex-window-id"))
	require.Equal(t, meta.TurnMetadata, req.Header.Get("x-codex-turn-metadata"))

	var turn map[string]string
	require.NoError(t, json.Unmarshal([]byte(meta.TurnMetadata), &turn))
	require.Equal(t, meta.InstallationID, turn["installation_id"])
	require.Equal(t, meta.SessionID, turn["session_id"])
	require.Equal(t, meta.ThreadID, turn["thread_id"])
	require.Equal(t, meta.WindowID, turn["window_id"])
	require.Equal(t, "turn", turn["request_kind"])

	body := map[string]any{
		"client_metadata": map[string]any{
			"session_id":              "client-session",
			"thread_id":               "client-thread",
			"x-codex-window-id":       "client-window",
			"x-codex-turn-metadata":   "client-turn",
			"x-codex-installation-id": "client-installation",
		},
	}
	require.True(t, applyCodexClientMetadata(body, account, meta))
	clientMetadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "client-installation", clientMetadata["x-codex-installation-id"])
	require.Equal(t, meta.SessionID, clientMetadata["session_id"])
	require.Equal(t, meta.ThreadID, clientMetadata["thread_id"])
	require.Equal(t, meta.WindowID, clientMetadata["x-codex-window-id"])
	require.Equal(t, meta.TurnMetadata, clientMetadata["x-codex-turn-metadata"])
}

func TestCodexProfileRequestMetadataUsesProfileInstallationFallback(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	profile := &CodexEnvironmentProfile{
		Family:           CodexClientFamilyCLI,
		Source:           "simulated",
		UserAgent:        "codex_cli_rs/0.142.2 (Ubuntu 22.4.0; x86_64) xterm-256color",
		Originator:       "codex_cli_rs",
		Version:          "0.142.2",
		SessionSeed:      "session-seed",
		ConversationSeed: "thread-seed",
		TLSProfile:       "codex-cli-linux",
	}
	require.NoError(t, profile.Validate())

	meta := codexProfileRequestMetadataFor(account, profile, CodexProfileApplyOptions{APIKeyID: 7})
	require.NotEmpty(t, meta.InstallationID)
	require.Equal(t, profile.InstallationID, meta.InstallationID)

	body := map[string]any{}
	require.True(t, applyCodexClientMetadata(body, account, meta))
	clientMetadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, meta.InstallationID, clientMetadata["x-codex-installation-id"])
}
