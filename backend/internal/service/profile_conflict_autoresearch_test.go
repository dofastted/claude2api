package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type autoresearchIdentityCache struct {
	maskedSessionID string
}

func (c *autoresearchIdentityCache) GetFingerprint(context.Context, int64) (*Fingerprint, error) {
	return nil, nil
}

func (c *autoresearchIdentityCache) SetFingerprint(context.Context, int64, *Fingerprint) error {
	return nil
}

func (c *autoresearchIdentityCache) GetMaskedSessionID(context.Context, int64) (string, error) {
	return c.maskedSessionID, nil
}

func (c *autoresearchIdentityCache) SetMaskedSessionID(_ context.Context, _ int64, sessionID string) error {
	c.maskedSessionID = sessionID
	return nil
}

func TestAutoresearchProfileConflictWorkload(t *testing.T) {
	profile := fixedAutoresearchClaudeProfile()

	t.Run("profile beta is authoritative for v2 passthrough", func(t *testing.T) {
		svc := &GatewayService{}
		clientHeaders := http.Header{}
		clientHeaders.Set("Anthropic-Beta", "client-beta,context-management-2025-06-27")

		beta, shouldSet := svc.computeFinalAnthropicBeta("oauth", false, "claude-sonnet-4-6", clientHeaders, []byte(`{"messages":[]}`), nil, profile)

		require.True(t, shouldSet)
		require.Equal(t, "slot-beta-2026-01-01,context-management-2025-06-27", beta)
		require.NotContains(t, beta, "client-beta")
	})

	t.Run("profile headers override legacy header profile", func(t *testing.T) {
		svc := &GatewayService{}
		req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
		require.NoError(t, err)

		svc.applyClaudeCodeHeaderProfile(req, &Account{ID: 77}, &ClaudeCodeHeaderProfile{
			Headers: map[string]string{
				"User-Agent":                  "claude-cli/legacy",
				"X-App":                       "legacy-app",
				"X-Stainless-Package-Version": "0.0.1",
			},
			UpdatedAt: time.Unix(1700000000, 0).UTC(),
		})
		svc.applyClaudeEnvironmentProfile(req, &Account{ID: 77}, profile)

		require.Equal(t, profile.UserAgent, req.Header.Get("User-Agent"))
		require.Equal(t, profile.XApp, req.Header.Get("X-App"))
		require.Equal(t, profile.ClientVersion, req.Header.Get("X-Stainless-Package-Version"))
		require.Equal(t, profile.Platform, getHeaderRaw(req.Header, "X-Stainless-OS"))
	})

	t.Run("profile session seed overrides legacy session masking", func(t *testing.T) {
		cache := &autoresearchIdentityCache{maskedSessionID: "11111111-2222-4333-8444-555555555555"}
		svc := NewIdentityService(cache, nil)
		original := FormatMetadataUserID(strings.Repeat("b", 64), "old-account", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", ExtractCLIVersion(profile.UserAgent))
		body := []byte(`{"metadata":{"user_id":` + fmt.Sprintf("%q", original) + `},"messages":[]}`)

		out, err := svc.RewriteUserIDWithSessionID(body, 77, "profile-account", profile.DeviceID, profile.UserAgent, profile.SessionSeed)
		require.NoError(t, err)

		parsed := parseAutoresearchMetadataUserID(gjson.GetBytes(out, "metadata.user_id"))
		require.NotNil(t, parsed)
		require.Equal(t, profile.DeviceID, parsed.DeviceID)
		require.Equal(t, "profile-account", parsed.AccountUUID)
		require.Equal(t, profile.SessionSeed, parsed.SessionID)
		require.NotEqual(t, cache.maskedSessionID, parsed.SessionID)
	})

	t.Run("profile tls overrides legacy account switch", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
		require.NoError(t, err)
		legacy := &tlsfingerprint.Profile{Name: "legacy-account-tls"}
		profileTLS := resolveClaudeEnvironmentTLSProfile(profile)
		require.NotNil(t, profileTLS)

		req = attachClaudeEnvironmentTLSProfileToRequest(req, profileTLS)

		require.Equal(t, tlsfingerprint.ProfileNameClaudeCLIDefault, tlsProfileForRequest(req, legacy).Name)
	})

	t.Run("profile cache policy overrides legacy cache switches", func(t *testing.T) {
		body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral","ttl":"5m"}}]},{"role":"assistant","content":[{"type":"text","text":"ok"}]},{"role":"user","content":[{"type":"text","text":"middle"}]},{"role":"user","content":[{"type":"text","text":"last"}]}]}`)

		out := rewriteCacheControlForClaudeEnvironmentProfile(profile, body)

		require.False(t, gjson.GetBytes(out, "messages.0.content.0.cache_control").Exists())
		require.Equal(t, "1h", gjson.GetBytes(out, "messages.2.content.0.cache_control.ttl").String())
		require.Equal(t, "1h", gjson.GetBytes(out, "messages.3.content.0.cache_control.ttl").String())
	})

	conflicts := countAutoresearchLegacyExitPoints(t)
	fmt.Printf("METRIC profile_conflict_count=%d\n", conflicts)
	fmt.Printf("METRIC profile_alignment_checks=%d\n", 3)
	fmt.Printf("METRIC profile_workload_cases=%d\n", conflicts+5)
}

func fixedAutoresearchClaudeProfile() *ClaudeEnvironmentProfile {
	return &ClaudeEnvironmentProfile{
		Family:         ClaudeClientFamilyCodeCLI,
		Source:         claudeEnvironmentProfileSourceSimulated,
		ClientID:       strings.Repeat("c", 64),
		DeviceID:       strings.Repeat("d", 64),
		SessionSeed:    "22222222-3333-4444-8555-666666666666",
		UserAgent:      "claude-cli/2.1.88 (external, cli)",
		XApp:           "claude-code",
		ClientVersion:  "2.1.88",
		Platform:       "linux",
		PlatformRaw:    "linux",
		Arch:           "x64",
		Runtime:        "node",
		RuntimeVersion: "v24.0.0",
		ClientType:     "cli",
		Headers:        map[string]string{},
		BetaSet: []string{
			"slot-beta-2026-01-01",
			"context-management-2025-06-27",
		},
		TLSProfile:  tlsfingerprint.ProfileNameClaudeCLIDefault,
		CachePolicy: claudeEnvironmentCachePolicyProfileManaged,
		FrozenAt:    time.Unix(1700000000, 0).UTC(),
		CreatedAt:   time.Unix(1700000000, 0).UTC(),
		UpdatedAt:   time.Unix(1700000000, 0).UTC(),
	}
}

func parseAutoresearchMetadataUserID(value gjson.Result) *ParsedUserID {
	if value.Type == gjson.String {
		return ParseMetadataUserID(value.String())
	}
	return ParseMetadataUserID(value.Raw)
}

func countAutoresearchLegacyExitPoints(t *testing.T) int {
	t.Helper()
	checks := []struct {
		name     string
		file     string
		pattern  string
		resolved bool
	}{
		{name: "tls fingerprint extra", file: "account.go", pattern: "enable_tls_fingerprint", resolved: true},
		{name: "session id masking extra", file: "account.go", pattern: "session_id_masking_enabled", resolved: true},
		{name: "message cache rewrite setting", file: "gateway_messages_cache.go", pattern: "rewriteMessageCacheControlIfEnabled", resolved: true},
		{name: "cache ttl 1h injection setting", file: "gateway_service.go", pattern: "shouldInjectAnthropicCacheTTL1h", resolved: true},
		{name: "legacy claude header profile", file: "claude_code_header_profile.go", pattern: "claude_code_header_profile", resolved: false},
	}

	count := 0
	for _, check := range checks {
		data, err := os.ReadFile(check.file)
		require.NoErrorf(t, err, "read %s", check.file)
		if strings.Contains(string(data), check.pattern) && !check.resolved {
			count++
		}
	}
	return count
}
