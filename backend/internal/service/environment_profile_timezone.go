package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dofastted/claude2api/internal/config"
)

const EnvironmentProfileFallbackTimezone = "America/Los_Angeles"

const environmentProfileTimezoneCacheTTL = time.Hour

var defaultEnvironmentProfileTimezoneResolver struct {
	mu       sync.RWMutex
	resolver *EnvironmentProfileTimezoneResolver
}

func DefaultEnvironmentProfileTimezoneResolver() *EnvironmentProfileTimezoneResolver {
	defaultEnvironmentProfileTimezoneResolver.mu.RLock()
	defer defaultEnvironmentProfileTimezoneResolver.mu.RUnlock()
	return defaultEnvironmentProfileTimezoneResolver.resolver
}

func setDefaultEnvironmentProfileTimezoneResolver(resolver *EnvironmentProfileTimezoneResolver) {
	if resolver == nil || resolver.prober == nil {
		return
	}
	defaultEnvironmentProfileTimezoneResolver.mu.Lock()
	defer defaultEnvironmentProfileTimezoneResolver.mu.Unlock()
	defaultEnvironmentProfileTimezoneResolver.resolver = resolver
}

type environmentProfileTimezoneCacheEntry struct {
	timezone  string
	expiresAt time.Time
}

type environmentProfileProxyLookup interface {
	GetByID(ctx context.Context, id int64) (*Proxy, error)
}

type EnvironmentProfileTimezoneResolver struct {
	prober      ProxyExitInfoProber
	cfg         *config.Config
	proxyLookup environmentProfileProxyLookup

	mu    sync.Mutex
	cache map[string]environmentProfileTimezoneCacheEntry
}

func NewEnvironmentProfileTimezoneResolver(prober ProxyExitInfoProber, cfg *config.Config, proxyLookups ...environmentProfileProxyLookup) *EnvironmentProfileTimezoneResolver {
	var proxyLookup environmentProfileProxyLookup
	if len(proxyLookups) > 0 {
		proxyLookup = proxyLookups[0]
	}
	resolver := &EnvironmentProfileTimezoneResolver{
		prober:      prober,
		cfg:         cfg,
		proxyLookup: proxyLookup,
		cache:       map[string]environmentProfileTimezoneCacheEntry{},
	}
	setDefaultEnvironmentProfileTimezoneResolver(resolver)
	if prober != nil {
		go resolver.WarmDirect(context.Background())
	}
	return resolver
}

func (r *EnvironmentProfileTimezoneResolver) WarmDirect(ctx context.Context) {
	if r == nil {
		return
	}
	_ = r.Resolve(ctx, nil)
}

func (r *EnvironmentProfileTimezoneResolver) Resolve(ctx context.Context, account *Account) string {
	if r == nil || r.prober == nil {
		return EnvironmentProfileFallbackTimezone
	}
	proxyURL, cacheKey, ok := r.effectiveProxy(ctx, account)
	if cached, ok := r.cached(cacheKey); ok {
		return cached
	}
	if !ok {
		slog.Warn("environment_profile_timezone_proxy_unavailable", "proxy_key", cacheKey)
		return r.store(cacheKey, EnvironmentProfileFallbackTimezone)
	}
	info, _, err := r.prober.ProbeProxy(ctx, proxyURL)
	if err != nil {
		slog.Warn("environment_profile_timezone_probe_failed", "proxy_key", cacheKey, "error", err)
		return r.store(cacheKey, EnvironmentProfileFallbackTimezone)
	}
	timezone := EnvironmentProfileFallbackTimezone
	if info != nil {
		timezone = NormalizeEnvironmentProfileTimezoneForCountry(info.Timezone, info.CountryCode)
		if timezone == EnvironmentProfileFallbackTimezone && strings.TrimSpace(info.Timezone) != "" {
			slog.Warn("environment_profile_timezone_normalized_to_fallback", "proxy_key", cacheKey, "source", info.Source, "country_code", info.CountryCode, "timezone", info.Timezone)
		}
	}
	return r.store(cacheKey, timezone)
}

func (r *EnvironmentProfileTimezoneResolver) effectiveProxy(ctx context.Context, account *Account) (proxyURL, cacheKey string, ok bool) {
	if r != nil && r.cfg != nil {
		if global := strings.TrimSpace(r.cfg.Gateway.GlobalProxyURL); global != "" {
			return global, global, true
		}
	}
	if account == nil || account.ProxyID == nil || *account.ProxyID <= 0 {
		return "", "direct", true
	}
	cacheKey = formatEnvironmentProfileProxyCacheKey(*account.ProxyID)
	if account.Proxy != nil {
		if proxyURL := strings.TrimSpace(account.Proxy.URL()); proxyURL != "" {
			return proxyURL, proxyURL, true
		}
		return "", cacheKey, false
	}
	if r == nil || r.proxyLookup == nil {
		return "", cacheKey, false
	}
	proxy, err := r.proxyLookup.GetByID(ctx, *account.ProxyID)
	if err != nil || proxy == nil {
		if err != nil {
			slog.Warn("environment_profile_timezone_proxy_lookup_failed", "proxy_id", *account.ProxyID, "error", err)
		}
		return "", cacheKey, false
	}
	proxyURL = strings.TrimSpace(proxy.URL())
	if proxyURL == "" {
		return "", cacheKey, false
	}
	return proxyURL, proxyURL, true
}

func formatEnvironmentProfileProxyCacheKey(proxyID int64) string {
	return "proxy:" + strconv.FormatInt(proxyID, 10)
}

func (r *EnvironmentProfileTimezoneResolver) cached(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key]
	if !ok || time.Now().UTC().After(entry.expiresAt) {
		return "", false
	}
	return entry.timezone, true
}

func (r *EnvironmentProfileTimezoneResolver) store(key, timezone string) string {
	timezone = NormalizeEnvironmentProfileTimezone(timezone)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = environmentProfileTimezoneCacheEntry{
		timezone:  timezone,
		expiresAt: time.Now().UTC().Add(environmentProfileTimezoneCacheTTL),
	}
	return timezone
}

func NormalizeEnvironmentProfileTimezone(timezone string) string {
	return NormalizeEnvironmentProfileTimezoneForCountry(timezone, "")
}

func NormalizeEnvironmentProfileTimezoneForCountry(timezone, countryCode string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" || IsChinaEnvironmentProfileTimezone(timezone, countryCode) {
		return EnvironmentProfileFallbackTimezone
	}
	if timezone == "Local" {
		return EnvironmentProfileFallbackTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return EnvironmentProfileFallbackTimezone
	}
	return timezone
}

func IsChinaEnvironmentProfileTimezone(timezone, countryCode string) bool {
	if strings.EqualFold(strings.TrimSpace(countryCode), "CN") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(timezone)) {
	case "asia/shanghai", "asia/chongqing", "asia/chungking", "asia/harbin", "asia/urumqi", "asia/kashgar", "prc", "china":
		return true
	default:
		return false
	}
}

func sanitizeOAuthJSONBody(body []byte) []byte {
	if len(body) == 0 || !json.Valid(body) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if !sanitizeOAuthRequestMap(payload) {
		return body
	}
	next, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return next
}

func sanitizeOAuthRequestMap(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	changed := sanitizeBlockedOAuthKeys(payload)
	if sanitizeCodexTurnMetadataField(payload) {
		changed = true
	}
	for _, key := range []string{"metadata", "client_metadata", "credential_extras", "credentials"} {
		if child, ok := mapAny(payload[key]); ok {
			if sanitizeBlockedOAuthKeysRecursive(child) {
				payload[key] = child
				changed = true
			}
		}
	}
	return changed
}

func sanitizeOAuthMetadataMap(metadata map[string]any) bool {
	return sanitizeBlockedOAuthKeysRecursive(metadata)
}

func sanitizeCodexTurnMetadataField(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	changed := false
	for key, value := range payload {
		normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
		if normalized != "x_codex_turn_metadata" {
			continue
		}
		raw, ok := value.(string)
		if !ok {
			continue
		}
		next := sanitizeCodexTurnMetadataString(raw)
		if strings.TrimSpace(next) == "" {
			delete(payload, key)
			changed = true
			continue
		}
		if next != raw {
			payload[key] = next
			changed = true
		}
	}
	return changed
}

func sanitizeCodexTurnMetadataString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		if containsBlockedOAuthKeyText(trimmed) {
			return ""
		}
		return trimmed
	}
	if !sanitizeBlockedOAuthKeysRecursive(payload) {
		return trimmed
	}
	next, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(next)
}
func sanitizeCodexTurnMetadataStringStrict(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || containsBlockedOAuthKeyText(trimmed) {
		return ""
	}
	return sanitizeCodexTurnMetadataString(trimmed)
}

func sanitizeBlockedOAuthKeys(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	changed := false
	for key := range payload {
		if isBlockedOAuthClientField(key) {
			delete(payload, key)
			changed = true
		}
	}
	return changed
}

func sanitizeBlockedOAuthKeysRecursive(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	changed := sanitizeBlockedOAuthKeys(payload)
	for key, value := range payload {
		if child, ok := mapAny(value); ok {
			if sanitizeBlockedOAuthKeysRecursive(child) {
				payload[key] = child
				changed = true
			}
			continue
		}
		if values, ok := value.([]any); ok {
			if sanitizeBlockedOAuthValueList(values) {
				payload[key] = values
				changed = true
			}
		}
	}
	if sanitizeCodexTurnMetadataField(payload) {
		changed = true
	}
	return changed
}

func sanitizeBlockedOAuthValueList(values []any) bool {
	changed := false
	for i, value := range values {
		if child, ok := mapAny(value); ok {
			if sanitizeBlockedOAuthKeysRecursive(child) {
				values[i] = child
				changed = true
			}
			continue
		}
		if nested, ok := value.([]any); ok {
			if sanitizeBlockedOAuthValueList(nested) {
				values[i] = nested
				changed = true
			}
		}
	}
	return changed
}

func containsBlockedOAuthKeyText(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{
		"base_url", "custom_base_url", "endpoint", "hostname",
		"api_key", "x-api-key", "authorization",
		"time_zone", "timezone", "tz",
		"country_code", "countrycode", "country", "region_code", "regioncode", "region", "locale", "language", "accept_language", "accept-language",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func isBlockedOAuthClientField(key string) bool {
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "base_url", "custom_base_url", "custom_base_url_enabled", "endpoint", "hostname", "host", "api_key", "x_api_key", "key", "authorization", "timezone", "time_zone", "tz", "country", "country_code", "countrycode", "region", "region_code", "regioncode", "locale", "language", "accept_language":
		return true
	default:
		return false
	}
}

func isBlockedOAuthHeaderField(key string) bool {
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	if isBlockedOAuthClientField(normalized) {
		return true
	}
	if strings.Contains(normalized, "base_url") || strings.Contains(normalized, "custom_base_url") || strings.Contains(normalized, "endpoint") || strings.Contains(normalized, "hostname") {
		return true
	}
	if normalized == "host" || strings.HasSuffix(normalized, "_host") || strings.Contains(normalized, "host_") {
		return true
	}
	if strings.Contains(normalized, "api_key") || strings.Contains(normalized, "x_api_key") || strings.Contains(normalized, "authorization") {
		return true
	}
	return normalized == "timezone" || normalized == "time_zone" || normalized == "tz" || strings.HasSuffix(normalized, "_timezone") || normalized == "country" || normalized == "country_code" || normalized == "countrycode" || strings.HasSuffix(normalized, "_country") || strings.HasSuffix(normalized, "_country_code") || normalized == "region" || normalized == "region_code" || normalized == "regioncode" || strings.HasSuffix(normalized, "_region") || normalized == "locale" || normalized == "language" || normalized == "accept_language"
}

func mapAny(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func SanitizeOAuthCredentialsForStorage(platform, accountType string, credentials map[string]any) map[string]any {
	if !isOAuthProfileAccount(platform, accountType) || credentials == nil {
		return credentials
	}
	out := make(map[string]any, len(credentials))
	for key, value := range credentials {
		if isBlockedOAuthCredentialField(key) {
			continue
		}
		out[key] = value
	}
	return out
}

func SanitizeOAuthExtraForStorage(platform, accountType string, extra map[string]any) map[string]any {
	if !isOAuthProfileAccount(platform, accountType) || extra == nil {
		return extra
	}
	out := make(map[string]any, len(extra))
	for key, value := range extra {
		if isBlockedOAuthClientField(key) {
			continue
		}
		out[key] = value
	}
	return out
}

func isOAuthProfileAccount(platform, accountType string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformAnthropic:
		return accountType == AccountTypeOAuth || accountType == AccountTypeSetupToken
	case PlatformOpenAI:
		return accountType == AccountTypeOAuth
	default:
		return false
	}
}

func isBlockedOAuthCredentialField(key string) bool {
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "base_url", "custom_base_url", "custom_base_url_enabled", "endpoint", "hostname", "host", "api_key", "x_api_key", "key", "authorization", "timezone", "time_zone", "tz", "country", "country_code", "countrycode", "region", "region_code", "regioncode", "locale", "language", "accept_language":
		return true
	default:
		return false
	}
}
