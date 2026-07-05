package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIPassthroughOAuthBody_RemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"prompt_cache_retention":"24h","safety_identifier":"sid","stream_options":{"include_usage":true}}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	for _, field := range openAIChatGPTInternalUnsupportedFields {
		require.False(t, gjson.GetBytes(normalized, field).Exists(), "%s should be stripped", field)
	}
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
}

func TestNormalizeOpenAIPassthroughOAuthBody_StripsBlockedKeysFromCodexTurnMetadataString(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","client_metadata":{"session_id":"s-keep","x-codex-turn-metadata":"{\"session_id\":\"s-keep\",\"request_kind\":\"turn\",\"timezone\":\"Asia/Shanghai\",\"base_url\":\"https://relay.example\",\"api_key\":\"sk-leak\"}"}}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	turnMetadata := gjson.GetBytes(normalized, "client_metadata.x-codex-turn-metadata").String()
	require.NotEmpty(t, turnMetadata)
	require.NotContains(t, turnMetadata, "timezone")
	require.NotContains(t, turnMetadata, "Asia/Shanghai")
	require.NotContains(t, turnMetadata, "base_url")
	require.NotContains(t, turnMetadata, "api_key")
	require.Contains(t, turnMetadata, "s-keep")
	require.Contains(t, turnMetadata, "request_kind")
}

func TestNormalizeOpenAIPassthroughOAuthBody_CompactRemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"stream":true,"store":true}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "user").Exists())
	require.False(t, gjson.GetBytes(normalized, "metadata").Exists())
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}
