package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIPassthroughOAuthBody_RemovesUnsupportedFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"prompt_cache_retention":"24h","safety_identifier":"sid","stream_options":{"include_usage":true},"max_output_tokens":100,"max_completion_tokens":100,"temperature":0.5,"top_p":0.9,"frequency_penalty":0.2,"presence_penalty":0.3,"truncation":"auto","context_management":[{"type":"compaction"}],"namespace":"tools","service_tier":"flex","prompt_cache_key":"cache-key","parallel_tool_calls":true}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	for _, field := range openAICodexOAuthUnsupportedFields {
		require.False(t, gjson.GetBytes(normalized, field).Exists(), "%s should be stripped", field)
	}
	require.False(t, gjson.GetBytes(normalized, "service_tier").Exists())
	require.Equal(t, "cache-key", gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.True(t, gjson.GetBytes(normalized, "parallel_tool_calls").Bool())
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
}

func TestNormalizeOpenAIPassthroughOAuthBody_PreservesPriorityServiceTier(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","service_tier":" PRIORITY "}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "priority", gjson.GetBytes(normalized, "service_tier").String())
}

func TestSanitizeOpenAIResponsesCompatibilityFields_RemovesNamespace(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","namespace":"request-tools","input":[{"type":"message","role":"user","namespace":"message-tools","content":[{"type":"input_text","text":"hi"}]},{"type":"function_call","name":"lookup","namespace":"function-tools","arguments":"{}"}]}`)

	normalized, changed, err := sanitizeOpenAIResponsesCompatibilityFields(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "namespace").Exists())
	require.False(t, gjson.GetBytes(normalized, "input.0.namespace").Exists())
	require.False(t, gjson.GetBytes(normalized, "input.1.namespace").Exists())
	require.Equal(t, "lookup", gjson.GetBytes(normalized, "input.1.name").String())
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
