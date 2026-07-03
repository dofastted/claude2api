package service

import (
	"context"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/stretchr/testify/require"
)

// fakeProbeProber is a hand-written fake of the ProxyExitInfoProber boundary:
// deterministic, records the proxy URL it was asked to use, and returns a
// canned ProxyExitInfo. No real network is touched.
type fakeProbeProber struct {
	calledProxyURLs []string
	info            *ProxyExitInfo
	err             error
}

var errFakeProbeUnreachable = errFake("probe unreachable: all sources blocked")

type errFake string

func (e errFake) Error() string { return string(e) }

func (f *fakeProbeProber) ProbeProxy(_ context.Context, proxyURL string) (*ProxyExitInfo, int64, error) {
	f.calledProxyURLs = append(f.calledProxyURLs, proxyURL)
	if f.err != nil {
		return nil, 0, f.err
	}
	if f.info == nil {
		return nil, 0, nil
	}
	out := *f.info
	return &out, 0, nil
}

type fakeEnvironmentProfileProxyLookup struct {
	ids   []int64
	proxy *Proxy
	err   error
}

func (f *fakeEnvironmentProfileProxyLookup) GetByID(_ context.Context, id int64) (*Proxy, error) {
	f.ids = append(f.ids, id)
	if f.err != nil {
		return nil, f.err
	}
	if f.proxy == nil {
		return nil, nil
	}
	out := *f.proxy
	return &out, nil
}

func newResolverWithFake(prober *fakeProbeProber, cfg *config.Config) *EnvironmentProfileTimezoneResolver {
	return &EnvironmentProfileTimezoneResolver{
		prober: prober,
		cfg:    cfg,
		cache:  map[string]environmentProfileTimezoneCacheEntry{},
	}
}

func newResolverWithFakeAndProxyLookup(prober *fakeProbeProber, cfg *config.Config, lookup environmentProfileProxyLookup) *EnvironmentProfileTimezoneResolver {
	return &EnvironmentProfileTimezoneResolver{
		prober:      prober,
		cfg:         cfg,
		proxyLookup: lookup,
		cache:       map[string]environmentProfileTimezoneCacheEntry{},
	}
}

// --- NormalizeEnvironmentProfileTimezone / IsChinaEnvironmentProfileTimezone ---

func TestNormalizeEnvironmentProfileTimezone_PreservesValidNonCN(t *testing.T) {
	require.Equal(t, "America/Los_Angeles", NormalizeEnvironmentProfileTimezone("America/Los_Angeles"))
	require.Equal(t, "Europe/London", NormalizeEnvironmentProfileTimezone("Europe/London"))
	require.Equal(t, "Asia/Tokyo", NormalizeEnvironmentProfileTimezone("Asia/Tokyo"))
}

func TestNormalizeEnvironmentProfileTimezone_Empty_FallsBack(t *testing.T) {
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezone(""))
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezone("   "))
}

func TestNormalizeEnvironmentProfileTimezone_Invalid_FallsBack(t *testing.T) {
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezone("Not/A/Zone"))
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezone("Local"))
}

func TestNormalizeEnvironmentProfileTimezone_CN_Zones_FallBack(t *testing.T) {
	for _, tz := range []string{"Asia/Shanghai", "Asia/Chongqing", "Asia/Chungking", "Asia/Harbin", "Asia/Urumqi", "Asia/Kashgar", "PRC", "China", "asia/shanghai"} {
		require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezone(tz), "tz=%s", tz)
	}
}

func TestNormalizeEnvironmentProfileTimezoneForCountry_CN_Code_FallsBack(t *testing.T) {
	// Even a valid US timezone must fall back when the country code is CN.
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezoneForCountry("America/New_York", "CN"))
	require.Equal(t, EnvironmentProfileFallbackTimezone, NormalizeEnvironmentProfileTimezoneForCountry("Asia/Shanghai", "cn"))
	// Non-CN country code preserves a valid timezone.
	require.Equal(t, "Europe/Berlin", NormalizeEnvironmentProfileTimezoneForCountry("Europe/Berlin", "DE"))
}

func TestIsChinaEnvironmentProfileTimezone(t *testing.T) {
	require.True(t, IsChinaEnvironmentProfileTimezone("Asia/Shanghai", ""))
	require.True(t, IsChinaEnvironmentProfileTimezone("", "CN"))
	require.True(t, IsChinaEnvironmentProfileTimezone("", "cn"))
	require.False(t, IsChinaEnvironmentProfileTimezone("America/Los_Angeles", "US"))
	require.False(t, IsChinaEnvironmentProfileTimezone("Asia/Tokyo", "JP"))
}

// --- EnvironmentProfileTimezoneResolver.Resolve: proxy precedence + fallback ---

// TestResolve_GlobalProxyOverridesAccountProxy pins the precedence contract:
// gateway.global_proxy_url beats the account's own proxy URL, and the prober
// is asked to probe through the global proxy, not the account proxy.
func TestResolve_GlobalProxyOverridesAccountProxy(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "Europe/Berlin", CountryCode: "DE", Source: "ip-api"}}
	cfg := &config.Config{}
	cfg.Gateway.GlobalProxyURL = "http://global.example:8080"
	r := newResolverWithFake(prober, cfg)

	accountProxyID := int64(42)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		ProxyID:  &accountProxyID,
		Proxy:    &Proxy{Protocol: "http", Host: "account.example", Port: 3128},
	}

	got := r.Resolve(context.Background(), account)
	require.Equal(t, "Europe/Berlin", got)
	require.Len(t, prober.calledProxyURLs, 1, "resolver must probe exactly once")
	require.Equal(t, "http://global.example:8080", prober.calledProxyURLs[0], "global proxy must override account proxy")
}

// TestResolve_AccountProxyUsedWhenNoGlobal pins that without a global proxy
// the resolver probes through the account's own proxy URL.
func TestResolve_AccountProxyUsedWhenNoGlobal(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "Asia/Singapore", CountryCode: "SG", Source: "ip-api"}}
	r := newResolverWithFake(prober, &config.Config{})

	accountProxyID := int64(7)
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		ProxyID:  &accountProxyID,
		Proxy:    &Proxy{Protocol: "socks5", Host: "acct.example", Port: 1080},
	}

	got := r.Resolve(context.Background(), account)
	require.Equal(t, "Asia/Singapore", got)
	require.Len(t, prober.calledProxyURLs, 1)
	require.Contains(t, prober.calledProxyURLs[0], "socks5://acct.example:1080", "account proxy must be used when no global proxy is set")
}

func TestResolve_LoadsAccountProxyByIDWhenNotHydrated(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "Asia/Tokyo", CountryCode: "JP", Source: "ip-api"}}
	lookup := &fakeEnvironmentProfileProxyLookup{proxy: &Proxy{Protocol: "http", Host: "lookup.example", Port: 8080}}
	r := newResolverWithFakeAndProxyLookup(prober, &config.Config{}, lookup)

	accountProxyID := int64(9)
	got := r.Resolve(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &accountProxyID})

	require.Equal(t, "Asia/Tokyo", got)
	require.Equal(t, []int64{accountProxyID}, lookup.ids)
	require.Len(t, prober.calledProxyURLs, 1)
	require.Equal(t, "http://lookup.example:8080", prober.calledProxyURLs[0])
}

func TestResolve_ProxyIDWithoutLookup_FallsBackWithoutDirectProbe(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "America/New_York", CountryCode: "US", Source: "ip-api"}}
	r := newResolverWithFake(prober, &config.Config{})

	accountProxyID := int64(10)
	got := r.Resolve(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &accountProxyID})

	require.Equal(t, EnvironmentProfileFallbackTimezone, got)
	require.Empty(t, prober.calledProxyURLs, "configured proxy without lookup must not silently probe direct egress")
}

// TestResolve_DirectWhenNoProxy pins that with no global and no account proxy
// the resolver probes with an empty proxy URL (direct local egress).
func TestResolve_DirectWhenNoProxy(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "America/New_York", CountryCode: "US", Source: "ip-api"}}
	r := newResolverWithFake(prober, &config.Config{})

	got := r.Resolve(context.Background(), &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth})
	require.Equal(t, "America/New_York", got)
	require.Len(t, prober.calledProxyURLs, 1)
	require.Empty(t, prober.calledProxyURLs[0], "no proxy means direct (empty) proxy URL")
}

// TestResolve_ProbeError_FallsBackToUS pins that when every timezone-capable
// source is unreachable, the resolver writes America/Los_Angeles rather than
// failing or falling back to a China/CN timezone.
func TestResolve_ProbeError_FallsBackToUS(t *testing.T) {
	prober := &fakeProbeProber{err: errFakeProbeUnreachable}
	r := newResolverWithFake(prober, &config.Config{})

	got := r.Resolve(context.Background(), nil)
	require.Equal(t, EnvironmentProfileFallbackTimezone, got)
	require.Len(t, prober.calledProxyURLs, 1)
}

// TestResolve_IPOnlySource_FallsBackToUS pins the contract that an IP-only
// source (no timezone field) is treated as a resolver failure: the profile
// timezone becomes America/Los_Angeles, never empty and never CN.
func TestResolve_IPOnlySource_FallsBackToUS(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{IP: "9.8.7.6", Source: "httpbin", CountryCode: ""}}
	r := newResolverWithFake(prober, &config.Config{})

	got := r.Resolve(context.Background(), nil)
	require.Equal(t, EnvironmentProfileFallbackTimezone, got)
}

// TestResolve_CNTimezone_FallsBackToUS pins that a probe returning a CN
// timezone (by country code or by zone name) is normalized to the US fallback.
func TestResolve_CNTimezone_FallsBackToUS(t *testing.T) {
	cases := []struct {
		name string
		info *ProxyExitInfo
	}{
		{"cn country code", &ProxyExitInfo{Timezone: "Asia/Shanghai", CountryCode: "CN", Source: "ip-api"}},
		{"cn zone name only", &ProxyExitInfo{Timezone: "Asia/Chongqing", CountryCode: "", Source: "ip-api"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prober := &fakeProbeProber{info: c.info}
			r := newResolverWithFake(prober, &config.Config{})
			require.Equal(t, EnvironmentProfileFallbackTimezone, r.Resolve(context.Background(), nil))
		})
	}
}

// TestResolve_CachesPerProxyKey pins that repeated resolves for the same
// effective proxy URL hit the cache and probe the boundary at most once.
func TestResolve_CachesPerProxyKey(t *testing.T) {
	prober := &fakeProbeProber{info: &ProxyExitInfo{Timezone: "Europe/Paris", CountryCode: "FR", Source: "ip-api"}}
	r := newResolverWithFake(prober, &config.Config{})

	first := r.Resolve(context.Background(), nil)
	second := r.Resolve(context.Background(), nil)
	require.Equal(t, "Europe/Paris", first)
	require.Equal(t, first, second)
	require.Len(t, prober.calledProxyURLs, 1, "second resolve must hit cache, not re-probe")
}

// --- Frozen Claude/Codex profile pool timezone behavior ---

// TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_PropagatesTimezone
// pins that the supplied service-decided timezone is written into every
// frozen windows/macos/linux slot.
func TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_PropagatesTimezone(t *testing.T) {
	pool := newFrozenClaudeEnvironmentProfilePoolWithTimezone("2.1.161", "Europe/Berlin")
	require.True(t, pool.IsV2())
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, "Europe/Berlin", slot.Profile.Timezone, "slot %d timezone", i)
	}
}

// TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_CN_NormalizesToFallback
// pins that a CN/China timezone supplied to pool generation is normalized to
// the US fallback in every slot.
func TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_CN_NormalizesToFallback(t *testing.T) {
	pool := newFrozenClaudeEnvironmentProfilePoolWithTimezone("2.1.161", "Asia/Shanghai")
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d must be fallback", i)
	}
}

// TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_Missing_NormalizesToFallback
// pins that an empty/missing supplied timezone is normalized to the US
// fallback in every slot.
func TestNewFrozenClaudeEnvironmentProfilePoolWithTimezone_Missing_NormalizesToFallback(t *testing.T) {
	pool := newFrozenClaudeEnvironmentProfilePoolWithTimezone("2.1.161", "")
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d must be fallback", i)
	}
}

// TestNewFrozenClaudeEnvironmentProfilePool_Default_Fallback pins that the
// default pool constructor (no timezone arg) writes the US fallback.
func TestNewFrozenClaudeEnvironmentProfilePool_Default_Fallback(t *testing.T) {
	pool := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d default must be fallback", i)
	}
}

// TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_PropagatesTimezone
// pins that the supplied service-decided timezone is written into every
// frozen Codex windows/macos/linux slot.
func TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_PropagatesTimezone(t *testing.T) {
	pool := newFrozenCodexEnvironmentProfilePoolWithTimezone("Asia/Singapore")
	require.True(t, pool.IsV2())
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, "Asia/Singapore", slot.Profile.Timezone, "slot %d timezone", i)
	}
}

// TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_CN_NormalizesToFallback
// pins that a CN/China timezone supplied to Codex pool generation is
// normalized to the US fallback in every slot.
func TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_CN_NormalizesToFallback(t *testing.T) {
	pool := newFrozenCodexEnvironmentProfilePoolWithTimezone("Asia/Shanghai")
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d must be fallback", i)
	}
}

// TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_Missing_NormalizesToFallback
// pins that an empty/missing supplied timezone is normalized to the US
// fallback in every Codex slot.
func TestNewFrozenCodexEnvironmentProfilePoolWithTimezone_Missing_NormalizesToFallback(t *testing.T) {
	pool := newFrozenCodexEnvironmentProfilePoolWithTimezone("")
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d must be fallback", i)
	}
}

// TestNewFrozenCodexEnvironmentProfilePool_Default_Fallback pins that the
// default Codex pool constructor writes the US fallback.
func TestNewFrozenCodexEnvironmentProfilePool_Default_Fallback(t *testing.T) {
	pool := newFrozenCodexEnvironmentProfilePool()
	require.Len(t, pool.Slots, 3)
	for i, slot := range pool.Slots {
		require.Equal(t, EnvironmentProfileFallbackTimezone, slot.Profile.Timezone, "slot %d default must be fallback", i)
	}
}

// --- OAuth credential / extra / metadata sanitization ---

func TestSanitizeOAuthCredentialsForStorage_StripsBlockedKeys_OAuth(t *testing.T) {
	in := map[string]any{
		"access_token":            "tok-keep",
		"refresh_token":           "rt-keep",
		"email":                   "u@example.com",
		"chatgpt_account_id":      "acct-keep",
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
	}
	out := SanitizeOAuthCredentialsForStorage(PlatformOpenAI, AccountTypeOAuth, in)
	require.Contains(t, out, "access_token")
	require.Contains(t, out, "refresh_token")
	require.Contains(t, out, "email")
	require.Contains(t, out, "chatgpt_account_id")
	require.Equal(t, "tok-keep", out["access_token"])
	for _, blocked := range []string{"base_url", "custom_base_url", "custom_base_url_enabled", "endpoint", "hostname", "host", "api_key", "x-api-key", "key", "authorization", "timezone", "time_zone", "tz"} {
		require.NotContains(t, out, blocked, "blocked key %q must be stripped", blocked)
	}
}

func TestSanitizeOAuthCredentialsForStorage_PreservesAPIKeyBaseURL(t *testing.T) {
	// APIKey accounts must keep base_url + api_key: the OAuth-only sanitizer
	// must not touch non-OAuth account types.
	in := map[string]any{
		"base_url": "https://api.anthropic.example",
		"api_key":  "sk-real",
	}
	out := SanitizeOAuthCredentialsForStorage(PlatformAnthropic, AccountTypeAPIKey, in)
	require.Equal(t, "https://api.anthropic.example", out["base_url"], "APIKey base_url must be preserved")
	require.Equal(t, "sk-real", out["api_key"], "APIKey api_key must be preserved")
}

func TestSanitizeOAuthCredentialsForStorage_AnthropicOAuth_StripsBaseURL(t *testing.T) {
	in := map[string]any{
		"access_token":            "tok",
		"base_url":                "https://relay.example",
		"custom_base_url":         "https://relay2.example",
		"custom_base_url_enabled": true,
		"endpoint":                "https://ep.example",
	}
	out := SanitizeOAuthCredentialsForStorage(PlatformAnthropic, AccountTypeOAuth, in)
	require.Equal(t, "tok", out["access_token"])
	require.NotContains(t, out, "base_url")
	require.NotContains(t, out, "custom_base_url")
	require.NotContains(t, out, "custom_base_url_enabled")
	require.NotContains(t, out, "endpoint")
}

func TestSanitizeOAuthCredentialsForStorage_AnthropicSetupToken_StripsBaseURL(t *testing.T) {
	in := map[string]any{
		"access_token": "tok",
		"base_url":     "https://relay.example",
		"api_key":      "sk-leak",
	}
	out := SanitizeOAuthCredentialsForStorage(PlatformAnthropic, AccountTypeSetupToken, in)
	require.Equal(t, "tok", out["access_token"])
	require.NotContains(t, out, "base_url")
	require.NotContains(t, out, "api_key")
}

func TestSanitizeOAuthExtraForStorage_StripsBlockedKeys(t *testing.T) {
	in := map[string]any{
		"custom_base_url_enabled": true,
		"custom_base_url":         "https://relay.example",
		"base_url":                "https://relay2.example",
		"endpoint":                "https://ep.example",
		"hostname":                "relay.example",
		"api_key":                 "sk-leak",
		"timezone":                "Asia/Shanghai",
		"kept":                    "ok",
	}
	out := SanitizeOAuthExtraForStorage(PlatformAnthropic, AccountTypeOAuth, in)
	require.Equal(t, "ok", out["kept"])
	for _, blocked := range []string{"base_url", "custom_base_url", "custom_base_url_enabled", "endpoint", "hostname", "api_key", "timezone"} {
		require.NotContains(t, out, blocked, "extra blocked key %q must be stripped", blocked)
	}
}

func TestSanitizeOAuthExtraForStorage_NilOrNonOAuth_ReturnsInput(t *testing.T) {
	require.Nil(t, SanitizeOAuthExtraForStorage(PlatformAnthropic, AccountTypeOAuth, nil))
	in := map[string]any{"base_url": "keep"}
	out := SanitizeOAuthExtraForStorage(PlatformAnthropic, AccountTypeAPIKey, in)
	require.Equal(t, "keep", out["base_url"], "non-OAuth account type must not be sanitized")
}

func TestSanitizeOAuthCredentialsForStorage_NilOrNonOAuth_ReturnsInput(t *testing.T) {
	require.Nil(t, SanitizeOAuthCredentialsForStorage(PlatformOpenAI, AccountTypeOAuth, nil))
	in := map[string]any{"base_url": "keep", "api_key": "keep"}
	out := SanitizeOAuthCredentialsForStorage(PlatformOpenAI, AccountTypeAPIKey, in)
	require.Equal(t, "keep", out["base_url"])
	require.Equal(t, "keep", out["api_key"])
}

// --- sanitizeOAuthJSONBody / sanitizeOAuthMetadataMap ---

func TestSanitizeOAuthJSONBody_StripsBlockedKeysAndNestedMetadata(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-7-sonnet",
		"base_url": "https://relay.example",
		"endpoint": "https://ep.example",
		"metadata": {
			"user_id": "u-keep",
			"base_url": "https://meta-relay.example",
			"hostname": "meta-relay.example",
			"api_key": "sk-leak",
			"timezone": "Asia/Shanghai"
		},
		"client_metadata": {
			"session_id": "s-keep",
			"endpoint": "https://cm-ep.example",
			"authorization": "Bearer leak"
		}
	}`)
	got := sanitizeOAuthJSONBody(body)
	require.NotContains(t, string(got), "base_url")
	require.NotContains(t, string(got), "endpoint")
	require.NotContains(t, string(got), "hostname")
	require.NotContains(t, string(got), "api_key")
	require.NotContains(t, string(got), "authorization")
	require.NotContains(t, string(got), "Asia/Shanghai")
	require.Contains(t, string(got), "claude-3-7-sonnet")
	require.Contains(t, string(got), "u-keep")
	require.Contains(t, string(got), "s-keep")
}

func TestSanitizeOAuthJSONBody_InvalidJSON_ReturnedUnchanged(t *testing.T) {
	body := []byte("not-json{")
	require.Equal(t, body, sanitizeOAuthJSONBody(body))
}

func TestSanitizeOAuthJSONBody_Empty_ReturnedUnchanged(t *testing.T) {
	require.Empty(t, sanitizeOAuthJSONBody(nil))
	require.Empty(t, sanitizeOAuthJSONBody([]byte{}))
}

func TestSanitizeOAuthMetadataMap_StripsBlockedKeys(t *testing.T) {
	meta := map[string]any{
		"session_id":    "s-keep",
		"thread_id":     "t-keep",
		"base_url":      "https://relay.example",
		"endpoint":      "https://ep.example",
		"hostname":      "relay.example",
		"api_key":       "sk-leak",
		"authorization": "Bearer leak",
		"timezone":      "Asia/Shanghai",
	}
	require.True(t, sanitizeOAuthMetadataMap(meta))
	require.Equal(t, "s-keep", meta["session_id"])
	require.Equal(t, "t-keep", meta["thread_id"])
	for _, blocked := range []string{"base_url", "endpoint", "hostname", "api_key", "authorization", "timezone"} {
		require.NotContains(t, meta, blocked)
	}
}

func TestSanitizeOAuthMetadataMap_NoChange_ReturnsFalse(t *testing.T) {
	meta := map[string]any{"session_id": "s", "thread_id": "t"}
	require.False(t, sanitizeOAuthMetadataMap(meta))
}

// --- sanitizeCodexTurnMetadataString ---

func TestSanitizeCodexTurnMetadataString_StripsBlockedKeys(t *testing.T) {
	raw := `{"installation_id":"inst-keep","session_id":"s-keep","base_url":"https://relay.example","endpoint":"https://ep.example","timezone":"Asia/Shanghai","api_key":"sk-leak"}`
	got := sanitizeCodexTurnMetadataString(raw)
	require.NotContains(t, got, "base_url")
	require.NotContains(t, got, "endpoint")
	require.NotContains(t, got, "api_key")
	require.NotContains(t, got, "Asia/Shanghai")
	require.Contains(t, got, "inst-keep")
	require.Contains(t, got, "s-keep")
}

func TestSanitizeCodexTurnMetadataString_NoChange_ReturnsOriginal(t *testing.T) {
	raw := `{"installation_id":"inst","session_id":"s","request_kind":"turn"}`
	require.Equal(t, raw, sanitizeCodexTurnMetadataString(raw))
}

func TestSanitizeCodexTurnMetadataString_Empty(t *testing.T) {
	require.Empty(t, sanitizeCodexTurnMetadataString(""))
	require.Empty(t, sanitizeCodexTurnMetadataString("   "))
}

// TestSanitizeCodexTurnMetadataString_InvalidJSON_ContainsBlockedText_ReturnsEmpty
// pins that an unparseable turn metadata string that contains blocked key
// text is replaced with empty rather than forwarded upstream.
func TestSanitizeCodexTurnMetadataString_InvalidJSON_ContainsBlockedText_ReturnsEmpty(t *testing.T) {
	raw := `not-json-but-contains-base_url-and-endpoint`
	require.Empty(t, sanitizeCodexTurnMetadataString(raw))
}

// TestSanitizeCodexTurnMetadataString_InvalidJSON_NoBlockedText_ReturnedAsIs
// pins that an unparseable string with no blocked text is preserved.
func TestSanitizeCodexTurnMetadataString_InvalidJSON_NoBlockedText_ReturnedAsIs(t *testing.T) {
	raw := `not-json-but-harmless`
	require.Equal(t, raw, sanitizeCodexTurnMetadataString(raw))
}
