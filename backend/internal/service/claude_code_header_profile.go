package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	claudeCodeHeaderProfileKey     = "claude_code_header_profile"
	claudeCodeHeaderProfileMaxAge  = 30 * 24 * time.Hour
	claudeCodeHeaderProfileMaxSize = 2 * 1024
)

// ClaudeCodeHeaderProfile stores the stable, non-sensitive Claude Code request headers learned per account.
type ClaudeCodeHeaderProfile struct {
	Headers       map[string]string `json:"headers"`
	LearnedFrom   string            `json:"learned_from"`
	UpdatedAt     time.Time         `json:"updated_at"`
	ClientVersion string            `json:"client_version"`
	ClientFamily  string            `json:"client_family"`
}

var claudeCodeHeaderWhitelist = map[string]struct{}{
	"user-agent":               {},
	"x-app":                    {},
	"anthropic-beta":           {},
	"anthropic-version":        {},
	"anthropic-client-sha":     {},
	"anthropic-client-version": {},
}

var sensitiveHeaderKeywords = []string{
	"authorization",
	"cookie",
	"token",
	"x-api-key",
	"api-key",
}

func filterClaudeCodeHeaderProfile(headers http.Header) map[string]string {
	filtered := make(map[string]string)
	for key, values := range headers {
		canonicalKey := strings.ToLower(strings.TrimSpace(key))
		if canonicalKey == "" || isSensitiveClaudeCodeHeader(canonicalKey) || !isClaudeCodeHeaderAllowed(canonicalKey) {
			continue
		}
		value := strings.TrimSpace(firstNonEmptyHeaderValue(values))
		if value == "" {
			continue
		}
		filtered[canonicalKey] = value
	}
	return filtered
}

func isSensitiveClaudeCodeHeader(key string) bool {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	for _, keyword := range sensitiveHeaderKeywords {
		if strings.Contains(lowerKey, keyword) {
			return true
		}
	}
	return false
}

func isClaudeCodeHeaderAllowed(key string) bool {
	if _, ok := claudeCodeHeaderWhitelist[key]; ok {
		return true
	}
	return strings.HasPrefix(key, "anthropic-client-")
}

func firstNonEmptyHeaderValue(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *GatewayService) learnClaudeCodeHeaderProfile(ctx context.Context, account *Account, headers http.Header) {
	if s == nil || s.accountRepo == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return
	}
	filteredHeaders := filterClaudeCodeHeaderProfile(headers)
	if len(filteredHeaders) == 0 {
		return
	}
	profile := ClaudeCodeHeaderProfile{
		Headers:       filteredHeaders,
		LearnedFrom:   "real_claude_code_request",
		UpdatedAt:     time.Now().UTC(),
		ClientVersion: ExtractCLIVersion(headers.Get("User-Agent")),
		ClientFamily:  "claude-cli",
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		slog.Warn("claude_code_header_profile_marshal_failed", "account_id", account.ID, "error", err)
		return
	}
	if len(encoded) > claudeCodeHeaderProfileMaxSize {
		slog.Warn("claude_code_header_profile_too_large", "account_id", account.ID, "size", len(encoded))
		return
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{claudeCodeHeaderProfileKey: profile}); err != nil {
		slog.Warn("claude_code_header_profile_update_failed", "account_id", account.ID, "error", err)
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 1)
	}
	account.Extra[claudeCodeHeaderProfileKey] = profile
	slog.Info("claude_code_header_profile_learned",
		"account_id", account.ID,
		"account_name", account.Name,
		"client_version", profile.ClientVersion,
		"headers_count", len(profile.Headers),
	)
}

func (s *GatewayService) getClaudeCodeHeaderProfile(account *Account) *ClaudeCodeHeaderProfile {
	if account == nil || account.Extra == nil {
		return nil
	}
	rawProfile, ok := account.Extra[claudeCodeHeaderProfileKey]
	if !ok || rawProfile == nil {
		return nil
	}
	profile, err := decodeClaudeCodeHeaderProfile(rawProfile)
	if err != nil {
		slog.Warn("claude_code_header_profile_parse_failed", "account_id", account.ID, "error", err)
		return nil
	}
	if len(profile.Headers) == 0 {
		return nil
	}
	if time.Since(profile.UpdatedAt) > claudeCodeHeaderProfileMaxAge {
		slog.Debug("claude_code_header_profile_expired", "account_id", account.ID, "age_days", time.Since(profile.UpdatedAt).Hours()/24)
		return nil
	}
	return profile
}

func decodeClaudeCodeHeaderProfile(raw any) (*ClaudeCodeHeaderProfile, error) {
	if profile, ok := raw.(ClaudeCodeHeaderProfile); ok {
		return &profile, nil
	}
	if profile, ok := raw.(*ClaudeCodeHeaderProfile); ok {
		return profile, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var profile ClaudeCodeHeaderProfile
	if err := json.Unmarshal(encoded, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *GatewayService) applyClaudeCodeHeaderProfile(req *http.Request, account *Account, profile *ClaudeCodeHeaderProfile) {
	if req == nil || profile == nil {
		return
	}
	for key, value := range profile.Headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isSensitiveClaudeCodeHeader(key) || !isClaudeCodeHeaderAllowed(key) {
			continue
		}
		setHeaderRaw(req.Header, resolveWireCasing(key), value)
	}
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	slog.Debug("claude_code_header_profile_applied",
		"account_id", accountID,
		"profile_age_days", time.Since(profile.UpdatedAt).Hours()/24,
		"headers_count", len(profile.Headers),
	)
}
