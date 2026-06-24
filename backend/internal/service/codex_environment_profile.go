package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	openaiheaders "github.com/dofastted/claude2api/internal/pkg/openai"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	codexSingleEnvironmentKey                         = "codex_single_environment"
	codexEnvironmentProfileKey                        = "codex_environment_profile"
	codexEnvironmentProfileLockedKey                  = "codex_environment_profile_locked"
	codexEnvironmentAllowOfficialClientLearnKey       = "codex_environment_allow_official_client_learn"
	codexEnvironmentProfileFamilyPreferenceKey        = "codex_environment_profile_family_preference"
	codexEnvironmentAllowOfficialClientLearnLegacyKey = "codex_environment_allow_desktop_learn"
	codexEnvironmentProfileSourceSimulated            = "simulated"
)

type CodexClientFamily string

const (
	CodexClientFamilyCLI     CodexClientFamily = "cli"
	CodexClientFamilyDesktop CodexClientFamily = "desktop"
	CodexClientFamilyVSCode  CodexClientFamily = "vscode"
)

type CodexEnvironmentProfile struct {
	Family           CodexClientFamily `json:"family"`
	Source           string            `json:"source"`
	UserAgent        string            `json:"user_agent"`
	Originator       string            `json:"originator"`
	Version          string            `json:"version"`
	SessionSeed      string            `json:"session_seed"`
	ConversationSeed string            `json:"conversation_seed"`
	ClientType       string            `json:"client_type"`
	Platform         string            `json:"platform"`
	Arch             string            `json:"arch"`
	TLSProfile       string            `json:"tls_profile"`
	Headers          map[string]string `json:"headers"`
	FrozenAt         time.Time         `json:"frozen_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type CodexEnvironmentProfileSettings struct {
	SingleEnvironment        *bool   `json:"single_environment"`
	ProfileLocked            *bool   `json:"profile_locked"`
	AllowOfficialClientLearn *bool   `json:"allow_official_client_learn"`
	ProfileFamilyPreference  *string `json:"profile_family_preference"`
}

type CodexProfileApplyOptions struct {
	APIKeyID             int64
	PromptCacheKey       string
	CompatMessagesBridge bool
	Compact              bool
	WSBetaValue          string
	AcceptLanguage       string
}

func DecodeCodexEnvironmentProfile(raw any) (*CodexEnvironmentProfile, error) {
	if raw == nil {
		return nil, nil
	}
	var profile CodexEnvironmentProfile
	switch v := raw.(type) {
	case CodexEnvironmentProfile:
		profile = v
	case *CodexEnvironmentProfile:
		if v == nil {
			return nil, nil
		}
		profile = *v
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &profile); err != nil {
			return nil, err
		}
	case []byte:
		if len(v) == 0 {
			return nil, nil
		}
		if err := json.Unmarshal(v, &profile); err != nil {
			return nil, err
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(trimmed), &profile); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported codex environment profile type %T", raw)
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return &profile, nil
}

func EncodeCodexEnvironmentProfile(profile *CodexEnvironmentProfile) (map[string]any, error) {
	if profile == nil {
		return nil, fmt.Errorf("codex environment profile is required")
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *CodexEnvironmentProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("codex environment profile is required")
	}
	family := normalizeCodexClientFamily(p.Family)
	if family == "" {
		return fmt.Errorf("invalid codex environment profile family")
	}
	p.Family = family
	p.Source = strings.TrimSpace(p.Source)
	if p.Source == "" {
		p.Source = "admin"
	}
	p.UserAgent = strings.TrimSpace(p.UserAgent)
	if p.UserAgent == "" {
		return fmt.Errorf("codex environment profile user_agent is required")
	}
	p.Originator = strings.TrimSpace(p.Originator)
	p.Version = strings.TrimSpace(p.Version)
	p.SessionSeed = strings.TrimSpace(p.SessionSeed)
	if p.SessionSeed == "" {
		return fmt.Errorf("codex environment profile session_seed is required")
	}
	p.ConversationSeed = strings.TrimSpace(p.ConversationSeed)
	if p.ConversationSeed == "" {
		return fmt.Errorf("codex environment profile conversation_seed is required")
	}
	p.ClientType = strings.TrimSpace(p.ClientType)
	if p.ClientType == "" {
		p.ClientType = string(p.Family)
	}
	p.Platform = strings.TrimSpace(p.Platform)
	p.Arch = strings.TrimSpace(p.Arch)
	p.TLSProfile = strings.TrimSpace(p.TLSProfile)
	if p.TLSProfile == "" {
		p.TLSProfile = defaultCodexTLSProfileForFamily(p.Family)
	}
	p.Headers = sanitizeCodexEnvironmentProfileHeaders(p.Headers)
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = p.CreatedAt
	}
	return nil
}

func DefaultCodexCLIEnvironmentProfile(registry *clientidentity.Registry) (*CodexEnvironmentProfile, error) {
	snapshot := codexSnapshotFromRegistry(registry)
	ua := strings.TrimSpace(snapshot.Headers["User-Agent"])
	if ua == "" {
		ua = strings.TrimSpace(clientidentity.NewRegistry().Get().Codex.Headers["User-Agent"])
	}
	originator := strings.TrimSpace(snapshot.Headers["originator"])
	if originator == "" {
		originator = "codex_cli_rs"
	}
	version := strings.TrimSpace(snapshot.VersionFields.CLIVersion)
	profile, err := newCodexEnvironmentProfile(CodexClientFamilyCLI, "auto_default", ua, originator, version, defaultCodexTLSProfile(snapshot.TLSProfileName), nil)
	if err != nil {
		return nil, err
	}
	profile.Platform = "darwin"
	profile.Arch = "arm64"
	return profile, nil
}

func codexSnapshotFromRegistry(registry *clientidentity.Registry) clientidentity.CodexSnapshot {
	if registry != nil {
		if snapshots := registry.Get(); snapshots != nil {
			return snapshots.Codex
		}
	}
	return clientidentity.NewRegistry().Get().Codex
}

func newCodexEnvironmentProfile(family CodexClientFamily, source, ua, originator, version, tlsProfile string, headers map[string]string) (*CodexEnvironmentProfile, error) {
	now := time.Now().UTC().Truncate(time.Second)
	sessionSeed, err := randomHexSeed(32)
	if err != nil {
		return nil, err
	}
	conversationSeed, err := randomHexSeed(32)
	if err != nil {
		return nil, err
	}
	profile := &CodexEnvironmentProfile{
		Family:           family,
		Source:           strings.TrimSpace(source),
		UserAgent:        strings.TrimSpace(ua),
		Originator:       strings.TrimSpace(originator),
		Version:          strings.TrimSpace(version),
		SessionSeed:      sessionSeed,
		ConversationSeed: conversationSeed,
		ClientType:       string(family),
		Platform:         "darwin",
		Arch:             "arm64",
		TLSProfile:       strings.TrimSpace(tlsProfile),
		Headers:          sanitizeCodexEnvironmentProfileHeaders(headers),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return profile, profile.Validate()
}

func LearnCodexEnvironmentProfileFromHeaders(headers http.Header, registry *clientidentity.Registry) (*CodexEnvironmentProfile, bool, error) {
	if IsGenericProbeUserAgent(headers.Get("User-Agent")) {
		return nil, false, nil
	}
	family := detectCodexClientFamilyFromHeaders(headers)
	if family == "" {
		return nil, false, nil
	}
	snapshot := codexSnapshotFromRegistry(registry)
	ua := strings.TrimSpace(headerValueCaseInsensitive(headers, "User-Agent"))
	if ua == "" {
		ua = strings.TrimSpace(snapshot.Headers["User-Agent"])
	}
	originator := strings.TrimSpace(headerValueCaseInsensitive(headers, "originator"))
	if originator == "" {
		originator = strings.TrimSpace(snapshot.Headers["originator"])
	}
	if originator == "" {
		originator = "codex_cli_rs"
	}
	version := strings.TrimSpace(headerValueCaseInsensitive(headers, "version"))
	if version == "" {
		version = strings.TrimSpace(snapshot.VersionFields.CLIVersion)
	}
	profile, err := newCodexEnvironmentProfile(family, "learned_verified_"+string(family), ua, originator, version, defaultCodexTLSProfileForFamily(family), whitelistCodexLearnedHeaders(headers))
	return profile, true, err
}

func detectCodexClientFamilyFromHeaders(headers http.Header) CodexClientFamily {
	if headers == nil {
		return ""
	}
	if ua := strings.TrimSpace(headerValueCaseInsensitive(headers, "User-Agent")); ua != "" && IsGenericProbeUserAgent(ua) {
		return ""
	}
	ua := strings.ToLower(strings.TrimSpace(headerValueCaseInsensitive(headers, "User-Agent")))
	originator := strings.ToLower(strings.TrimSpace(headerValueCaseInsensitive(headers, "originator")))
	combined := ua + "\n" + originator
	if strings.Contains(combined, "codex_chatgpt_desktop") || strings.Contains(combined, "codex desktop") {
		return CodexClientFamilyDesktop
	}
	if strings.Contains(combined, "codex_vscode") {
		return CodexClientFamilyVSCode
	}
	if strings.Contains(combined, "codex_cli_rs") || strings.Contains(combined, "codex-tui") || openaiheaders.IsCodexCLIRequest(ua) || originator == "codex_cli_rs" || originator == "codex-tui" {
		return CodexClientFamilyCLI
	}
	return ""
}

func randomHexSeed(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func whitelistCodexLearnedHeaders(headers http.Header) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string)
	for _, key := range []string{"User-Agent", "originator", "version", "openai-beta", "accept-language"} {
		if v := strings.TrimSpace(headers.Get(key)); v != "" && !isSensitiveCodexProfileHeader(key, v) {
			out[http.CanonicalHeaderKey(key)] = v
		}
	}
	return out
}

func sanitizeCodexEnvironmentProfileHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isSensitiveCodexProfileHeader(key, value) {
			continue
		}
		switch strings.ToLower(key) {
		case "user-agent", "originator", "version", "openai-beta", "accept-language":
			out[http.CanonicalHeaderKey(key)] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveCodexProfileHeader(key, value string) bool {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	lowerValue := strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(lowerKey, "authorization") || strings.Contains(lowerKey, "cookie") || strings.Contains(lowerKey, "api-key") || strings.Contains(lowerKey, "token") {
		return true
	}
	if strings.Contains(lowerValue, "bearer ") || strings.Contains(lowerValue, "sk-") || strings.Contains(lowerValue, "@") || strings.Contains(lowerValue, "file://") || strings.Contains(lowerValue, "github.com/") {
		return true
	}
	return false
}

func normalizeCodexClientFamily(family CodexClientFamily) CodexClientFamily {
	switch CodexClientFamily(strings.ToLower(strings.TrimSpace(string(family)))) {
	case CodexClientFamilyCLI:
		return CodexClientFamilyCLI
	case CodexClientFamilyDesktop:
		return CodexClientFamilyDesktop
	case CodexClientFamilyVSCode:
		return CodexClientFamilyVSCode
	default:
		return ""
	}
}

func normalizeCodexProfileFamilyPreference(preference string) (string, error) {
	preference = strings.ToLower(strings.TrimSpace(preference))
	if preference == "" {
		return "auto", nil
	}
	switch preference {
	case "auto", string(CodexClientFamilyCLI), string(CodexClientFamilyDesktop), string(CodexClientFamilyVSCode):
		return preference, nil
	default:
		return "", fmt.Errorf("invalid codex profile family preference")
	}
}

func defaultCodexTLSProfile(name string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return tlsfingerprint.ProfileNameCodexCLIDefault
}

func defaultCodexTLSProfileForFamily(family CodexClientFamily) string {
	switch family {
	case CodexClientFamilyDesktop:
		return tlsfingerprint.ProfileNameCodexDesktopDefault
	default:
		return tlsfingerprint.ProfileNameCodexCLIDefault
	}
}

func resolveCodexTLSProfile(profile *CodexEnvironmentProfile) *tlsfingerprint.Profile {
	if profile == nil {
		return nil
	}
	return tlsfingerprint.BuiltInProfileByName(strings.TrimSpace(profile.TLSProfile))
}

func applyCodexEnvironmentProfile(req *http.Request, account *Account, profile *CodexEnvironmentProfile, opts CodexProfileApplyOptions) {
	if req == nil || account == nil || profile == nil || !account.IsOpenAIOAuth() {
		return
	}
	if profile.UserAgent != "" {
		req.Header.Set("User-Agent", profile.UserAgent)
	}
	if profile.Version != "" {
		req.Header.Set("Version", profile.Version)
	}
	if opts.CompatMessagesBridge {
		req.Header.Del("OpenAI-Beta")
		req.Header.Del("originator")
	} else {
		if opts.WSBetaValue != "" {
			req.Header.Set("OpenAI-Beta", opts.WSBetaValue)
		} else if req.Header.Get("OpenAI-Beta") == "" {
			req.Header.Set("OpenAI-Beta", "responses=experimental")
		}
		if profile.Originator != "" {
			req.Header.Set("originator", profile.Originator)
		}
	}
	if opts.AcceptLanguage != "" && req.Header.Get("accept-language") == "" {
		req.Header.Set("accept-language", opts.AcceptLanguage)
	}
	stableSession := codexProfileSessionValue(profile.SessionSeed, opts.PromptCacheKey)
	stableConversation := codexProfileSessionValue(profile.ConversationSeed, opts.PromptCacheKey)
	if stableSession != "" {
		isolated := isolateOpenAISessionID(opts.APIKeyID, stableSession)
		req.Header.Set("session_id", isolated)
		req.Header.Set("Session_Id", isolated)
	}
	if stableConversation != "" {
		isolated := isolateOpenAISessionID(opts.APIKeyID, stableConversation)
		req.Header.Set("conversation_id", isolated)
		req.Header.Set("Conversation_Id", isolated)
	}
}

func codexProfileSessionValue(seed, promptCacheKey string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return strings.TrimSpace(promptCacheKey)
	}
	promptCacheKey = strings.TrimSpace(promptCacheKey)
	if promptCacheKey == "" {
		return seed
	}
	return seed + ":" + promptCacheKey
}

func codexEnvironmentProfileFields(profile *CodexEnvironmentProfile) []zap.Field {
	if profile == nil {
		return nil
	}
	return []zap.Field{
		zap.String("codex_environment_profile_family", string(profile.Family)),
		zap.String("codex_environment_profile_source", profile.Source),
		zap.String("codex_environment_profile_originator", profile.Originator),
		zap.String("codex_environment_profile_version", profile.Version),
	}
}

func (s *OpenAIGatewayService) getOrCreateCodexEnvironmentProfile(ctx context.Context, account *Account, headers http.Header) (*CodexEnvironmentProfile, error) {
	if account == nil || !account.IsCodexSingleEnvironmentEnabled() {
		return nil, nil
	}
	if profile, ok := account.GetCodexEnvironmentProfile(); ok {
		logCodexEnvironmentFamilyMismatch(ctx, account, profile, headers)
		return profile, nil
	}
	if s == nil {
		return DefaultCodexCLIEnvironmentProfile(nil)
	}
	if s.accountRepo == nil {
		return nil, nil
	}
	key := fmt.Sprintf("codex_environment_profile:%d", account.ID)
	result, err, _ := s.codexEnvironmentProfileSF.Do(key, func() (any, error) {
		if fresh, freshErr := s.accountRepo.GetByID(ctx, account.ID); freshErr == nil && fresh != nil {
			if profile, ok := fresh.GetCodexEnvironmentProfile(); ok {
				return profile, nil
			}
		}
		if profile, ok := account.GetCodexEnvironmentProfile(); ok {
			return profile, nil
		}
		var profile *CodexEnvironmentProfile
		var learned bool
		var err error
		if account.AllowCodexOfficialClientEnvironmentLearn() {
			profile, learned, err = LearnCodexEnvironmentProfileFromHeaders(headers, s.identityRegistry)
			if err != nil {
				return nil, err
			}
		}
		if !learned {
			profile, err = DefaultCodexCLIEnvironmentProfile(s.identityRegistry)
			if err != nil {
				return nil, err
			}
		}
		encoded, err := EncodeCodexEnvironmentProfile(profile)
		if err != nil {
			return nil, err
		}
		updates := map[string]any{
			codexSingleEnvironmentKey:                   true,
			codexEnvironmentProfileKey:                  encoded,
			codexEnvironmentProfileLockedKey:            true,
			codexEnvironmentAllowOfficialClientLearnKey: account.AllowCodexOfficialClientEnvironmentLearn(),
		}
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
			return nil, err
		}
		log := codexProfileLogger(ctx)
		fields := append([]zap.Field{zap.Int64("account_id", account.ID)}, codexEnvironmentProfileFields(profile)...)
		if learned {
			log.With(fields...).Info("codex_environment_profile_learned")
		} else {
			log.With(fields...).Info("codex_environment_profile_created")
		}
		return profile, nil
	})
	if err != nil {
		codexProfileLogger(ctx).With(zap.Int64("account_id", account.ID), zap.Error(err)).Warn("codex_environment_profile_update_failed")
		return nil, err
	}
	profile, _ := result.(*CodexEnvironmentProfile)
	logCodexEnvironmentFamilyMismatch(ctx, account, profile, headers)
	return profile, nil
}

func logCodexEnvironmentFamilyMismatch(ctx context.Context, account *Account, profile *CodexEnvironmentProfile, headers http.Header) {
	if account == nil || profile == nil || headers == nil {
		return
	}
	incoming := detectCodexClientFamilyFromHeaders(headers)
	if incoming == "" || incoming == profile.Family {
		return
	}
	codexProfileLogger(ctx).With(
		zap.Int64("account_id", account.ID),
		zap.String("codex_environment_profile_family", string(profile.Family)),
		zap.String("request_codex_family", string(incoming)),
	).Warn("codex_environment_profile_family_mismatch")
}

func codexProfileLogger(ctx context.Context) *zap.Logger {
	_ = ctx
	return zap.L().WithOptions(zap.AddCallerSkip(1))
}

func ginRequestHeader(c *gin.Context) http.Header {
	if c == nil || c.Request == nil {
		return nil
	}
	return c.Request.Header
}
