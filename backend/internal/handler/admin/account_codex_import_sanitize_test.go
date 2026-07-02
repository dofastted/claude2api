package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSanitizeCodexImportCredentialExtras_StripsBlockedKeys pins the contract
// that the Codex session import path cannot smuggle client-supplied endpoint
// identity or secret fields (base_url/custom_base_url/endpoint/host/api_key/
// key/authorization/timezone) into OAuth credentials via credential_extras.
//
// Protected OAuth token/identity fields (access_token, refresh_token, etc.)
// are intentionally skipped from the extras output: they come from the parsed
// token, not from credential_extras, so extras must not override them. Other
// non-protected, non-blocked extras survive for legitimate supplemental use.
func TestSanitizeCodexImportCredentialExtras_StripsBlockedKeys(t *testing.T) {
	in := map[string]any{
		// protected OAuth fields — skipped from extras so they cannot override
		// the parsed token values; they are NOT part of the extras output.
		"access_token":       "tok-should-not-override",
		"refresh_token":      "rt-should-not-override",
		"id_token":           "idt-should-not-override",
		"expires_at":         float64(1700000000),
		"email":              "u@example.com",
		"chatgpt_account_id": "acct-should-not-override",
		"chatgpt_user_id":    "user-should-not-override",
		"organization_id":    "org-should-not-override",
		"plan_type":          "pro",
		"client_id":          "cid-should-not-override",
		// blocked endpoint/secret/timezone fields — must be stripped
		"base_url":                "https://relay.example",
		"custom_base_url":         "https://relay2.example",
		"custom_base_url_enabled": true,
		"endpoint":                "https://ep.example",
		"hostname":                "relay.example",
		"host":                    "relay.example",
		"api_key":                 "sk-leak",
		"x-api-key":               "sk-leak2",
		"key":                     "raw-key",
		"authorization":           "Bearer leak",
		"timezone":                "Asia/Shanghai",
		"time_zone":               "Asia/Shanghai",
		"tz":                      "Asia/Shanghai",
		// non-protected, non-blocked supplemental extra — survives.
		"note": "kept-note",
	}

	out := sanitizeCodexImportCredentialExtras(in)

	// Blocked endpoint/secret/timezone keys never reach OAuth credentials.
	for _, blocked := range []string{
		"base_url", "custom_base_url", "custom_base_url_enabled",
		"endpoint", "hostname", "host",
		"api_key", "x-api-key", "key", "authorization",
		"timezone", "time_zone", "tz",
	} {
		require.NotContains(t, out, blocked, "blocked key %q must be stripped from import extras", blocked)
	}

	// Protected OAuth fields must NOT be overridable via credential_extras:
	// they are skipped from the extras output so the parsed token values win.
	for _, prot := range []string{
		"access_token", "refresh_token", "id_token", "expires_at",
		"email", "chatgpt_account_id", "chatgpt_user_id",
		"organization_id", "plan_type", "client_id",
	} {
		require.NotContains(t, out, prot, "protected field %q must not be injectable via credential_extras", prot)
	}

	// Non-protected, non-blocked supplemental extras survive.
	require.Equal(t, "kept-note", out["note"])
}

// TestSanitizeCodexImportCredentialExtras_EmptyReturnsNil pins the contract
// that an empty input yields nil, so no empty map is stored into credentials.
func TestSanitizeCodexImportCredentialExtras_EmptyReturnsNil(t *testing.T) {
	require.Nil(t, sanitizeCodexImportCredentialExtras(nil))
	require.Nil(t, sanitizeCodexImportCredentialExtras(map[string]any{}))
}

// TestSanitizeCodexImportCredentialExtras_OnlyBlockedReturnsNil pins that an
// input containing only blocked keys yields nil (no empty credentials map is
// persisted), defending the invariant that OAuth credentials never carry an
// empty sanitized blob from a purely malicious import payload.
func TestSanitizeCodexImportCredentialExtras_OnlyBlockedReturnsNil(t *testing.T) {
	in := map[string]any{
		"base_url":      "https://relay.example",
		"endpoint":      "https://ep.example",
		"api_key":       "sk-leak",
		"authorization": "Bearer leak",
		"timezone":      "Asia/Shanghai",
	}
	require.Nil(t, sanitizeCodexImportCredentialExtras(in))
}
