package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
)

const codexIdentityConfuseContextKey = "codex_identity_confuse_state"

// CodexIdentityConfuseState records request IDs rewritten for upstream and restored for the client.
type CodexIdentityConfuseState struct {
	OriginalPromptCacheKey string
	UpstreamPromptCacheKey string
}

func (s CodexIdentityConfuseState) enabled() bool {
	return strings.TrimSpace(s.OriginalPromptCacheKey) != "" && strings.TrimSpace(s.UpstreamPromptCacheKey) != "" && s.OriginalPromptCacheKey != s.UpstreamPromptCacheKey
}

func applyCodexIdentityConfuseRequest(reqBody map[string]any, meta codexProfileRequestMetadata) (CodexIdentityConfuseState, bool) {
	if reqBody == nil {
		return CodexIdentityConfuseState{}, false
	}
	original := strings.TrimSpace(firstNonEmptyString(reqBody["prompt_cache_key"]))
	upstream := strings.TrimSpace(meta.SessionID)
	state := CodexIdentityConfuseState{
		OriginalPromptCacheKey: original,
		UpstreamPromptCacheKey: upstream,
	}
	if !state.enabled() {
		return state, false
	}

	changed := false
	if existing := strings.TrimSpace(firstNonEmptyString(reqBody["prompt_cache_key"])); existing != upstream {
		reqBody["prompt_cache_key"] = upstream
		changed = true
	}
	if clientMetadata, ok := reqBody["client_metadata"].(map[string]any); ok {
		if changedMetadata := applyCodexIdentityConfuseClientMetadata(clientMetadata, state, meta); changedMetadata {
			changed = true
		}
	}
	return state, changed
}

func applyCodexIdentityConfuseClientMetadata(clientMetadata map[string]any, state CodexIdentityConfuseState, meta codexProfileRequestMetadata) bool {
	if clientMetadata == nil || !state.enabled() {
		return false
	}
	changed := false
	if raw, ok := clientMetadata[openAIWSTurnMetadataHeader].(string); ok && strings.TrimSpace(raw) != "" {
		if next, ok := codexIdentityConfuseTurnMetadata(raw, state, meta); ok && next != raw {
			clientMetadata[openAIWSTurnMetadataHeader] = next
			changed = true
		}
	}
	if meta.WindowID != "" {
		if existing, ok := clientMetadata[codexHeaderWindowID].(string); ok && strings.TrimSpace(existing) != "" && existing != meta.WindowID {
			clientMetadata[codexHeaderWindowID] = meta.WindowID
			changed = true
		}
	}
	return changed
}

func codexIdentityConfuseTurnMetadata(raw string, state CodexIdentityConfuseState, meta codexProfileRequestMetadata) (string, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw, false
	}
	changed := false
	if _, exists := payload["prompt_cache_key"]; exists {
		payload["prompt_cache_key"] = state.UpstreamPromptCacheKey
		changed = true
	}
	if meta.WindowID != "" {
		if existing, ok := payload["window_id"].(string); ok && strings.TrimSpace(existing) != "" && existing != meta.WindowID {
			payload["window_id"] = meta.WindowID
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func applyCodexIdentityExposeResponsePayload(payload []byte, state CodexIdentityConfuseState) []byte {
	if len(payload) == 0 || !state.enabled() || !bytes.Contains(payload, []byte(state.UpstreamPromptCacheKey)) {
		return payload
	}
	return bytes.ReplaceAll(payload, []byte(state.UpstreamPromptCacheKey), []byte(state.OriginalPromptCacheKey))
}

func codexIdentityConfuseStateFromContext(c *gin.Context) (CodexIdentityConfuseState, bool) {
	if c == nil {
		return CodexIdentityConfuseState{}, false
	}
	value, ok := c.Get(codexIdentityConfuseContextKey)
	if !ok {
		return CodexIdentityConfuseState{}, false
	}
	state, ok := value.(CodexIdentityConfuseState)
	if !ok || !state.enabled() {
		return CodexIdentityConfuseState{}, false
	}
	return state, true
}
