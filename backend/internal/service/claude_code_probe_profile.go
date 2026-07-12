package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dofastted/claude2api/internal/pkg/claude"
	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func cloneClaudeEnvironmentProfile(profile *ClaudeEnvironmentProfile) *ClaudeEnvironmentProfile {
	if profile == nil {
		return nil
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return nil
	}
	var cloned ClaudeEnvironmentProfile
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil
	}
	return &cloned
}

func claudeCodeProbeBaseHeaders(identityRegistry *clientidentity.Registry) http.Header {
	headers := http.Header{}
	for key, value := range claude.GetHeaders(identityRegistry) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		headers.Set(resolveWireCasing(key), value)
	}
	return headers
}

func resolveClaudeCodeProbeProfile(account *Account, identityRegistry *clientidentity.Registry, body []byte) *ClaudeEnvironmentProfile {
	env := routeToSlot(DetectClaudeEnvironmentClass(claudeCodeProbeBaseHeaders(identityRegistry), body))
	if profile := claudeCodeProbeProfileFromAccount(account, env); profile != nil {
		return profile
	}
	profile := buildFrozenClaudeEnvironmentProfileForSlot(env, ExtractCLIVersion(claude.GetHeaders(identityRegistry)["User-Agent"]))
	if account != nil {
		applyStableClaudeTelemetryIdentity(profile, account.ID, string(env))
		applyClaudeTelemetryContext(profile, account)
	}
	return profile
}

func claudeCodeProbeProfileFromAccount(account *Account, env EnvironmentClass) *ClaudeEnvironmentProfile {
	if account == nil || account.Extra == nil {
		return nil
	}
	if pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey]); err == nil && pool != nil {
		if profile := claudeCodeProbeProfileFromPool(pool, env); profile != nil {
			profile = cloneClaudeEnvironmentProfile(profile)
			applyClaudeTelemetryContext(profile, account)
			return profile
		}
	}
	if profile, ok := account.GetClaudeEnvironmentProfile(); ok {
		profile = cloneClaudeEnvironmentProfile(profile)
		applyClaudeTelemetryContext(profile, account)
		return profile
	}
	return nil
}

func claudeCodeProbeProfileFromPool(pool *ClaudeEnvironmentProfilePool, env EnvironmentClass) *ClaudeEnvironmentProfile {
	if pool == nil {
		return nil
	}
	env = routeToSlot(env)
	for i := range pool.Slots {
		if routeToSlot(pool.Slots[i].Environment) == env && pool.Slots[i].Profile != nil {
			return pool.Slots[i].Profile
		}
	}
	return nil
}

func applyClaudeCodeProbeProfileToBody(body []byte, account *Account, profile *ClaudeEnvironmentProfile) []byte {
	if len(body) == 0 || profile == nil {
		return body
	}
	ensureClaudeTelemetryIdentity(profile)
	accountUUID := ""
	if account != nil {
		accountUUID = strings.TrimSpace(account.GetExtraString("account_uuid"))
	}
	version := strings.TrimSpace(profile.ClientVersion)
	if version == "" {
		version = ExtractCLIVersion(profile.UserAgent)
	}
	metadataUserID := FormatMetadataUserID(profile.TelemetryUserID, accountUUID, profile.TelemetrySessionID, version)
	if next, err := sjson.SetBytes(body, "metadata.user_id", metadataUserID); err == nil {
		body = next
	}
	return rewriteCacheControlForClaudeEnvironmentProfile(profile, body)
}

func applyClaudeEnvironmentProfileHeaders(req *http.Request, account *Account, profile *ClaudeEnvironmentProfile) {
	if req == nil || profile == nil {
		return
	}
	for key, value := range profile.Headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isSensitiveClaudeCodeHeader(key) || isBlockedOAuthHeaderField(key) || !isClaudeEnvironmentHeaderAllowed(key) {
			continue
		}
		setHeaderRaw(req.Header, resolveWireCasing(key), value)
	}
	if profile.UserAgent != "" {
		setHeaderRaw(req.Header, "User-Agent", profile.UserAgent)
	}
	if profile.XApp != "" {
		setHeaderRaw(req.Header, "X-App", profile.XApp)
	}
	if profile.ClientType != "" {
		setHeaderRaw(req.Header, "Anthropic-Client-Type", profile.ClientType)
	}
	if profile.ClientVersion != "" {
		setHeaderRaw(req.Header, "X-Stainless-Package-Version", profile.ClientVersion)
	}
	if profile.Platform != "" {
		setHeaderRaw(req.Header, "X-Stainless-OS", profile.Platform)
	}
	if profile.Arch != "" {
		setHeaderRaw(req.Header, "X-Stainless-Arch", profile.Arch)
	}
	if profile.Runtime != "" {
		setHeaderRaw(req.Header, "X-Stainless-Runtime", profile.Runtime)
	}
	if profile.RuntimeVersion != "" {
		setHeaderRaw(req.Header, "X-Stainless-Runtime-Version", profile.RuntimeVersion)
	}
	if profile.TelemetrySessionID != "" {
		setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", profile.TelemetrySessionID)
	}
	deleteHeaderAllForms(req.Header, "traceparent")
}

func applyClaudeEnvironmentProfileHeaderMap(headers map[string]string, profile *ClaudeEnvironmentProfile) {
	if headers == nil || profile == nil {
		return
	}
	for key, value := range profile.Headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isSensitiveClaudeCodeHeader(key) || isBlockedOAuthHeaderField(key) || !isClaudeEnvironmentHeaderAllowed(key) {
			continue
		}
		headers[resolveWireCasing(key)] = value
	}
	if profile.UserAgent != "" {
		headers["User-Agent"] = profile.UserAgent
	}
	if profile.ClientType != "" {
		headers["Anthropic-Client-Type"] = profile.ClientType
	}
	if profile.Platform != "" {
		headers["X-Stainless-OS"] = profile.Platform
	}
	if profile.Arch != "" {
		headers["X-Stainless-Arch"] = profile.Arch
	}
	if profile.Runtime != "" {
		headers["X-Stainless-Runtime"] = profile.Runtime
	}
	if profile.RuntimeVersion != "" {
		headers["X-Stainless-Runtime-Version"] = profile.RuntimeVersion
	}
	if profile.TelemetrySessionID != "" {
		headers["X-Claude-Code-Session-Id"] = profile.TelemetrySessionID
	}
	applyClaudeCodeCLIProbeHeaderMapDefaults(headers, nil)
}

func applyClaudeCodeCLIProbeHeaderDefaults(headers http.Header, identityRegistry *clientidentity.Registry) {
	if headers == nil {
		return
	}
	defaults := claude.GetHeaders(identityRegistry)
	if xapp := strings.TrimSpace(defaults["X-App"]); xapp != "" {
		setHeaderRaw(headers, resolveWireCasing("x-app"), xapp)
	}
	if pkg := strings.TrimSpace(defaults["X-Stainless-Package-Version"]); pkg != "" {
		setHeaderRaw(headers, "X-Stainless-Package-Version", pkg)
	}
}

func applyClaudeCodeCLIProbeHeaderMapDefaults(headers map[string]string, identityRegistry *clientidentity.Registry) {
	if headers == nil {
		return
	}
	defaults := claude.GetHeaders(identityRegistry)
	if xapp := strings.TrimSpace(defaults["X-App"]); xapp != "" {
		headers[resolveWireCasing("x-app")] = xapp
	}
	if pkg := strings.TrimSpace(defaults["X-Stainless-Package-Version"]); pkg != "" {
		headers["X-Stainless-Package-Version"] = pkg
	}
}

func applyClaudeCodeProbeProfileHeaders(req *http.Request, account *Account, profile *ClaudeEnvironmentProfile, body []byte) {
	if req == nil || profile == nil {
		return
	}
	applyClaudeEnvironmentProfileHeaders(req, account, profile)
	applyClaudeCodeCLIProbeHeaderDefaults(req.Header, nil)
	setClaudeCodeSessionHeaderFromBody(req.Header, body)
}

func setClaudeCodeSessionHeaderFromBody(headers http.Header, body []byte) {
	if headers == nil || len(body) == 0 {
		return
	}
	parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String())
	if parsed == nil || strings.TrimSpace(parsed.SessionID) == "" {
		return
	}
	setHeaderRaw(headers, "X-Claude-Code-Session-Id", parsed.SessionID)
}
