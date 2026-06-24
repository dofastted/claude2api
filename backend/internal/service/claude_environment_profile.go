package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/claude"
	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
)

const (
	claudeSingleEnvironmentKey                   = "claude_single_environment"
	claudeEnvironmentProfileKey                  = "claude_environment_profile"
	claudeEnvironmentProfileLockedKey            = "claude_environment_profile_locked"
	claudeEnvironmentAllowDesktopLearnKey        = "claude_environment_allow_desktop_learn"
	claudeEnvironmentProfileFamilyPreferenceKey  = "claude_environment_profile_family_preference"
	claudeEnvironmentTelemetryPolicyLocalAck     = "local_ack"
	claudeEnvironmentProfileSourceAutoDefault    = "auto_default"
	claudeEnvironmentProfileSourceLearnedDesktop = "learned_verified_desktop"
	claudeEnvironmentProfileSourceAdmin          = "admin"
	claudeEnvironmentProfileSourceSimulated      = "simulated"
	claudeEnvironmentCachePolicyPreserveClient   = "preserve_client"
	claudeEnvironmentCachePolicyProfileManaged   = "profile_managed"
)

type ClaudeClientFamily string

const (
	ClaudeClientFamilyCodeCLI ClaudeClientFamily = "code_cli"
	ClaudeClientFamilyDesktop ClaudeClientFamily = "desktop"
)

type ClaudeEnvironmentProfile struct {
	Family          ClaudeClientFamily `json:"family"`
	Source          string             `json:"source"`
	ClientID        string             `json:"client_id"`
	DeviceID        string             `json:"device_id"`
	SessionSeed     string             `json:"session_seed"`
	UserAgent       string             `json:"user_agent"`
	XApp            string             `json:"x_app"`
	ClientVersion   string             `json:"client_version"`
	Platform        string             `json:"platform"`
	PlatformRaw     string             `json:"platform_raw"`
	Arch            string             `json:"arch"`
	Runtime         string             `json:"runtime"`
	RuntimeVersion  string             `json:"runtime_version"`
	ClientType      string             `json:"client_type"`
	Headers         map[string]string  `json:"headers"`
	BetaSet         []string           `json:"beta_set,omitempty"`
	TLSProfile      string             `json:"tls_profile,omitempty"`
	CachePolicy     string             `json:"cache_policy,omitempty"`
	FrozenAt        time.Time          `json:"frozen_at,omitempty"`
	TelemetryPolicy string             `json:"telemetry_policy"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

func DecodeClaudeEnvironmentProfile(raw any) (*ClaudeEnvironmentProfile, error) {
	if raw == nil {
		return nil, nil
	}
	if profile, ok := raw.(ClaudeEnvironmentProfile); ok {
		return &profile, nil
	}
	if profile, ok := raw.(*ClaudeEnvironmentProfile); ok {
		return profile, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var profile ClaudeEnvironmentProfile
	if err := json.Unmarshal(encoded, &profile); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(profile.Family)) == "" && strings.TrimSpace(profile.UserAgent) == "" {
		return nil, nil
	}
	return &profile, nil
}

func ValidateClaudeEnvironmentProfile(profile *ClaudeEnvironmentProfile) error {
	if profile == nil {
		return fmt.Errorf("claude environment profile is required")
	}
	switch profile.Family {
	case ClaudeClientFamilyCodeCLI, ClaudeClientFamilyDesktop:
	default:
		return fmt.Errorf("unsupported claude environment profile family: %s", profile.Family)
	}
	if strings.TrimSpace(profile.UserAgent) == "" {
		return fmt.Errorf("claude environment profile user_agent is required")
	}
	if strings.TrimSpace(profile.ClientID) == "" {
		return fmt.Errorf("claude environment profile client_id is required")
	}
	if strings.TrimSpace(profile.DeviceID) == "" {
		return fmt.Errorf("claude environment profile device_id is required")
	}
	if strings.TrimSpace(profile.SessionSeed) == "" {
		return fmt.Errorf("claude environment profile session_seed is required")
	}
	if strings.TrimSpace(profile.TLSProfile) == "" {
		profile.TLSProfile = defaultClaudeEnvironmentTLSProfileForFamily(profile.Family)
	}
	if strings.TrimSpace(profile.CachePolicy) == "" {
		profile.CachePolicy = claudeEnvironmentCachePolicyPreserveClient
	}
	return nil
}

func defaultClaudeCodeEnvironmentProfile(identityRegistry *clientidentity.Registry) *ClaudeEnvironmentProfile {
	headers := claude.GetHeaders(identityRegistry)
	now := time.Now().UTC()
	ua := strings.TrimSpace(headers["User-Agent"])
	return &ClaudeEnvironmentProfile{
		Family:          ClaudeClientFamilyCodeCLI,
		Source:          claudeEnvironmentProfileSourceAutoDefault,
		ClientID:        generateClientID(),
		DeviceID:        generateClientID(),
		SessionSeed:     uuid.NewString(),
		UserAgent:       ua,
		XApp:            "claude-code",
		ClientVersion:   ExtractCLIVersion(ua),
		Platform:        "darwin",
		PlatformRaw:     "darwin",
		Arch:            "arm64",
		Runtime:         "node",
		RuntimeVersion:  strings.TrimPrefix(headers["X-Stainless-Runtime-Version"], "v"),
		ClientType:      "cli",
		Headers:         map[string]string{},
		TLSProfile:      tlsfingerprint.ProfileNameClaudeCLIDefault,
		CachePolicy:     claudeEnvironmentCachePolicyPreserveClient,
		TelemetryPolicy: claudeEnvironmentTelemetryPolicyLocalAck,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func defaultClaudeEnvironmentTLSProfileForFamily(family ClaudeClientFamily) string {
	switch family {
	case ClaudeClientFamilyDesktop:
		return tlsfingerprint.ProfileNameClaudeDesktopDefault
	default:
		return tlsfingerprint.ProfileNameClaudeCLIDefault
	}
}

func resolveClaudeEnvironmentTLSProfile(profile *ClaudeEnvironmentProfile) *tlsfingerprint.Profile {
	if profile == nil {
		return nil
	}
	return tlsfingerprint.BuiltInProfileByName(strings.TrimSpace(profile.TLSProfile))
}

type claudeEnvironmentTLSProfileContextKey struct{}

func attachClaudeEnvironmentTLSProfileToRequest(req *http.Request, profile *tlsfingerprint.Profile) *http.Request {
	if req == nil || profile == nil {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), claudeEnvironmentTLSProfileContextKey{}, profile))
}

func tlsProfileForRequest(req *http.Request, fallback *tlsfingerprint.Profile) *tlsfingerprint.Profile {
	if req != nil {
		if profile, ok := req.Context().Value(claudeEnvironmentTLSProfileContextKey{}).(*tlsfingerprint.Profile); ok && profile != nil {
			return profile
		}
	}
	return fallback
}

func claudeEnvironmentProfileManagesCache(profile *ClaudeEnvironmentProfile) bool {
	return isV2ClaudeEnvironmentProfile(profile) && strings.TrimSpace(profile.CachePolicy) == claudeEnvironmentCachePolicyProfileManaged
}

func classifyClaudeClientFamily(headers http.Header, _ []byte) ClaudeClientFamily {
	uaRaw := strings.TrimSpace(headers.Get("User-Agent"))
	if IsGenericProbeUserAgent(uaRaw) {
		return ""
	}
	ua := strings.ToLower(uaRaw)
	xApp := strings.ToLower(strings.TrimSpace(headers.Get("X-App")))
	clientType := strings.ToLower(strings.TrimSpace(headers.Get("Anthropic-Client-Type")))
	if strings.Contains(ua, "claude desktop") || strings.Contains(ua, "electron") || xApp == "claude-desktop" || clientType == "desktop" {
		return ClaudeClientFamilyDesktop
	}
	if strings.Contains(ua, "claude-cli/") || xApp == "claude-code" || xApp == "claude-cli" || clientType == "cli" {
		return ClaudeClientFamilyCodeCLI
	}
	return ""
}

func learnDesktopClaudeEnvironmentProfile(headers http.Header) *ClaudeEnvironmentProfile {
	now := time.Now().UTC()
	ua := strings.TrimSpace(headers.Get("User-Agent"))
	if IsGenericProbeUserAgent(ua) {
		return nil
	}
	deviceID := strings.TrimSpace(headers.Get("Anthropic-Client-Device-Id"))
	if deviceID == "" {
		deviceID = generateClientID()
	}
	clientID := strings.TrimSpace(headers.Get("Anthropic-Client-Id"))
	if clientID == "" {
		clientID = generateClientID()
	}
	platform := strings.TrimSpace(headers.Get("X-Stainless-OS"))
	arch := strings.TrimSpace(headers.Get("X-Stainless-Arch"))
	runtime := strings.TrimSpace(headers.Get("X-Stainless-Runtime"))
	runtimeVersion := strings.TrimSpace(headers.Get("X-Stainless-Runtime-Version"))
	clientVersion := strings.TrimSpace(headers.Get("Anthropic-Client-Version"))
	if clientVersion == "" {
		clientVersion = ExtractCLIVersion(ua)
	}
	return &ClaudeEnvironmentProfile{
		Family:          ClaudeClientFamilyDesktop,
		Source:          claudeEnvironmentProfileSourceLearnedDesktop,
		ClientID:        clientID,
		DeviceID:        deviceID,
		SessionSeed:     uuid.NewString(),
		UserAgent:       ua,
		XApp:            strings.TrimSpace(headers.Get("X-App")),
		ClientVersion:   clientVersion,
		Platform:        strings.ToLower(platform),
		PlatformRaw:     platform,
		Arch:            strings.ToLower(arch),
		Runtime:         strings.ToLower(runtime),
		RuntimeVersion:  runtimeVersion,
		ClientType:      strings.TrimSpace(headers.Get("Anthropic-Client-Type")),
		Headers:         filterClaudeEnvironmentProfileHeaders(headers),
		TelemetryPolicy: claudeEnvironmentTelemetryPolicyLocalAck,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func filterClaudeEnvironmentProfileHeaders(headers http.Header) map[string]string {
	filtered := make(map[string]string)
	for key, values := range headers {
		canonicalKey := strings.ToLower(strings.TrimSpace(key))
		if canonicalKey == "" || isSensitiveClaudeCodeHeader(canonicalKey) || !isClaudeEnvironmentHeaderAllowed(canonicalKey) {
			continue
		}
		value := strings.TrimSpace(firstNonEmptyHeaderValue(values))
		if value == "" || looksSensitiveClaudeEnvironmentValue(value) {
			continue
		}
		filtered[canonicalKey] = value
	}
	return filtered
}

func isClaudeEnvironmentHeaderAllowed(key string) bool {
	if key == "user-agent" || key == "x-app" || key == "anthropic-version" || key == "anthropic-beta" {
		return true
	}
	return strings.HasPrefix(key, "anthropic-client-") || strings.HasPrefix(key, "x-stainless-")
}

func looksSensitiveClaudeEnvironmentValue(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "@") || strings.Contains(lower, "file://") || strings.Contains(lower, "github.com/") || strings.Contains(lower, "\\")
}

func (s *GatewayService) getOrCreateClaudeEnvironmentProfile(ctx context.Context, account *Account, headers http.Header, body []byte) (*ClaudeEnvironmentProfile, error) {
	if s == nil || account == nil || !account.IsClaudeSingleEnvironmentEnabled() {
		return nil, nil
	}
	if profile, ok := account.GetClaudeEnvironmentProfile(); ok {
		s.logClaudeEnvironmentFamilyMismatch(account, profile, classifyClaudeClientFamily(headers, body))
		return profile, nil
	}
	key := fmt.Sprintf("claude_environment_profile:%d", account.ID)
	value, err, _ := s.claudeEnvironmentProfileSF.Do(key, func() (any, error) {
		if s.accountRepo != nil {
			fresh, freshErr := s.accountRepo.GetByID(ctx, account.ID)
			if freshErr == nil && fresh != nil {
				if profile, ok := fresh.GetClaudeEnvironmentProfile(); ok {
					s.logClaudeEnvironmentFamilyMismatch(account, profile, classifyClaudeClientFamily(headers, body))
					return profile, nil
				}
			}
		}
		profile := s.buildInitialClaudeEnvironmentProfile(account, headers, body)
		if err := ValidateClaudeEnvironmentProfile(profile); err != nil {
			return nil, err
		}
		updates := map[string]any{
			claudeEnvironmentProfileKey:       profile,
			claudeEnvironmentProfileLockedKey: true,
		}
		if s.accountRepo == nil {
			return nil, fmt.Errorf("account repository is not configured")
		}
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
			slog.Warn("claude_environment_profile_update_failed", "account_id", account.ID, "error", err)
			return nil, err
		}
		slog.Info("claude_environment_profile_created", "account_id", account.ID, "family", profile.Family, "source", profile.Source)
		return profile, nil
	})
	if err != nil {
		return nil, err
	}
	profile, _ := value.(*ClaudeEnvironmentProfile)
	return profile, nil
}

func (s *GatewayService) buildInitialClaudeEnvironmentProfile(account *Account, headers http.Header, body []byte) *ClaudeEnvironmentProfile {
	preference := account.ClaudeEnvironmentFamilyPreference()
	family := classifyClaudeClientFamily(headers, body)
	if preference == string(ClaudeClientFamilyDesktop) {
		family = ClaudeClientFamilyDesktop
	}
	if family == ClaudeClientFamilyDesktop && account.AllowClaudeDesktopEnvironmentLearn() {
		profile := learnDesktopClaudeEnvironmentProfile(headers)
		if profile != nil && strings.TrimSpace(profile.UserAgent) != "" {
			slog.Info("claude_environment_profile_learned_desktop", "account_id", account.ID)
			return profile
		}
	}
	return defaultClaudeCodeEnvironmentProfile(s.identityRegistry)
}

func (s *GatewayService) logClaudeEnvironmentFamilyMismatch(account *Account, profile *ClaudeEnvironmentProfile, incoming ClaudeClientFamily) {
	if account == nil || profile == nil || incoming == "" || incoming == profile.Family {
		return
	}
	slog.Info("claude_environment_profile_family_mismatch", "account_id", account.ID, "profile_family", profile.Family, "incoming_family", incoming)
}

func (s *GatewayService) applyClaudeEnvironmentProfile(req *http.Request, account *Account, profile *ClaudeEnvironmentProfile) {
	if req == nil || profile == nil {
		return
	}
	for key, value := range profile.Headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isSensitiveClaudeCodeHeader(key) || !isClaudeEnvironmentHeaderAllowed(key) {
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
	deleteHeaderAllForms(req.Header, "traceparent")
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	slog.Debug("claude_environment_profile_applied", "account_id", accountID, "family", profile.Family)
}

func ClaudeEnvironmentProfileExtraKeys() map[string]string {
	return map[string]string{
		"single_environment":        claudeSingleEnvironmentKey,
		"profile_locked":            claudeEnvironmentProfileLockedKey,
		"allow_desktop_learn":       claudeEnvironmentAllowDesktopLearnKey,
		"profile_family_preference": claudeEnvironmentProfileFamilyPreferenceKey,
	}
}
