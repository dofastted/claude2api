package xai

import "strings"

const (
	SubscriptionTierFree  = "free"
	SubscriptionTierSuper = "super"
	SubscriptionTierHeavy = "heavy"

	FreeContextLimitTokens  int64 = 1_000_000
	FreeUsageRefreshSeconds       = 24 * 60 * 60

	DefaultCLIUserAgent        = "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)"
	DefaultCLIClientIdentifier = "grok-pager"
	DefaultCLIClientVersion    = "0.2.93"
	DefaultCLITokenAuth        = "xai-grok-cli"
)

var subscriptionTierReplacer = strings.NewReplacer("_", "-", " ", "-")

type SubscriptionProfile struct {
	Tier                   string `json:"tier"`
	ContextLimitTokens     int64  `json:"context_limit_tokens,omitempty"`
	UsageRefreshSeconds    int    `json:"usage_refresh_seconds,omitempty"`
	UsageEndpointAvailable bool   `json:"usage_endpoint_available"`
}

func NormalizeSubscriptionTier(raw string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = subscriptionTierReplacer.Replace(normalized)
	switch normalized {
	case "", "free", "free-tier":
		return SubscriptionTierFree, true
	case "super", "supergrok", "super-grok":
		return SubscriptionTierSuper, true
	case "heavy", "supergrok-heavy", "super-grok-heavy":
		return SubscriptionTierHeavy, true
	default:
		return "", false
	}
}

func SubscriptionProfileFor(raw string) (SubscriptionProfile, bool) {
	tier, ok := NormalizeSubscriptionTier(raw)
	if !ok {
		return SubscriptionProfile{}, false
	}
	profile := SubscriptionProfile{Tier: tier}
	switch tier {
	case SubscriptionTierFree:
		profile.ContextLimitTokens = FreeContextLimitTokens
		profile.UsageRefreshSeconds = FreeUsageRefreshSeconds
	case SubscriptionTierSuper, SubscriptionTierHeavy:
		profile.UsageEndpointAvailable = true
	}
	return profile, true
}

func DefaultCLICredentialHeaders() map[string]any {
	return map[string]any{
		"User-Agent":               DefaultCLIUserAgent,
		"X-XAI-Token-Auth":         DefaultCLITokenAuth,
		"x-grok-client-identifier": DefaultCLIClientIdentifier,
		"x-grok-client-version":    DefaultCLIClientVersion,
	}
}
