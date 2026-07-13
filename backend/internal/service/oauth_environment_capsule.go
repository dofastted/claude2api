package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/claude"
	"github.com/dofastted/claude2api/internal/pkg/xai"
)

// Environment capsule families for the three supported CLI OAuth credentials only.
const (
	EnvironmentCapsuleFamilyClaude = "claude"
	EnvironmentCapsuleFamilyCodex  = "codex"
	EnvironmentCapsuleFamilyGrok   = "grok"

	// Shared extra key for non-Claude families; Claude keeps claudeOAuthCredentialCapsulesKey for compatibility.
	codexOAuthCredentialCapsulesKey = "codex_oauth_credential_capsules"
	grokOAuthCredentialCapsulesKey  = "grok_oauth_credential_capsules"
)

// IsEnvironmentCapsuleOAuthAccount reports whether the account is one of the three
// CLI OAuth credential types that auto-bind Windows/macOS/Linux environment capsules.
func IsEnvironmentCapsuleOAuthAccount(account *Account) bool {
	if account == nil || account.Type != AccountTypeOAuth {
		return false
	}
	switch account.Platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformGrok:
		return true
	default:
		return false
	}
}

func environmentCapsuleFamily(account *Account) (string, error) {
	if !IsEnvironmentCapsuleOAuthAccount(account) {
		return "", fmt.Errorf("%w: environment capsules only apply to claude/codex/grok oauth credentials", ErrOAuthPoolCredentialInvalid)
	}
	switch account.Platform {
	case PlatformAnthropic:
		return EnvironmentCapsuleFamilyClaude, nil
	case PlatformOpenAI:
		return EnvironmentCapsuleFamilyCodex, nil
	case PlatformGrok:
		return EnvironmentCapsuleFamilyGrok, nil
	default:
		return "", fmt.Errorf("%w: unsupported capsule family", ErrOAuthPoolCredentialInvalid)
	}
}

func environmentCapsuleExtraKey(family string) string {
	switch family {
	case EnvironmentCapsuleFamilyClaude:
		return claudeOAuthCredentialCapsulesKey
	case EnvironmentCapsuleFamilyCodex:
		return codexOAuthCredentialCapsulesKey
	case EnvironmentCapsuleFamilyGrok:
		return grokOAuthCredentialCapsulesKey
	default:
		return ""
	}
}

// EnsureEnvironmentCapsules auto-binds exactly three system environment capsules
// for Claude / Codex / Grok CLI OAuth credentials. Other account types are rejected.
func EnsureEnvironmentCapsules(account *Account) (any, error) {
	return EnsureEnvironmentCapsulesWithOptions(account, "", "")
}

// EnsureEnvironmentCapsulesWithOptions is EnsureEnvironmentCapsules with optional CLI/timezone overrides.
func EnsureEnvironmentCapsulesWithOptions(account *Account, cliVersion, profileTimezone string) (any, error) {
	family, err := environmentCapsuleFamily(account)
	if err != nil {
		return nil, err
	}
	switch family {
	case EnvironmentCapsuleFamilyClaude:
		return EnsureClaudeOAuthCapsulesWithOptions(account, cliVersion, profileTimezone)
	case EnvironmentCapsuleFamilyCodex:
		return EnsureCodexOAuthCapsulesWithOptions(account, profileTimezone)
	case EnvironmentCapsuleFamilyGrok:
		return EnsureGrokOAuthCapsulesWithOptions(account, profileTimezone)
	default:
		return nil, fmt.Errorf("%w: unsupported capsule family %q", ErrOAuthPoolCredentialInvalid, family)
	}
}

// PersistEnvironmentCapsules returns UpdateExtra payload for the ensured capsule bundle on account.
func PersistEnvironmentCapsules(account *Account) (map[string]any, error) {
	if account == nil || account.Extra == nil {
		return nil, fmt.Errorf("%w: account capsules missing", ErrOAuthPoolInvalid)
	}
	family, err := environmentCapsuleFamily(account)
	if err != nil {
		return nil, err
	}
	key := environmentCapsuleExtraKey(family)
	raw, ok := account.Extra[key]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%w: capsule bundle not present on account", ErrOAuthPoolInvalid)
	}
	updates := map[string]any{key: raw}
	// Keep legacy profile pools in sync during transition for Claude/Codex.
	switch family {
	case EnvironmentCapsuleFamilyClaude:
		if pool, ok := account.Extra[claudeEnvironmentProfilePoolKey]; ok {
			updates[claudeEnvironmentProfilePoolKey] = pool
		}
	case EnvironmentCapsuleFamilyCodex:
		if pool, ok := account.Extra[codexEnvironmentProfilePoolKey]; ok {
			updates[codexEnvironmentProfilePoolKey] = pool
		}
	}
	return updates, nil
}

// EnvironmentCapsuleSummary is a safe admin/UI view of auto-bound capsules.
type EnvironmentCapsuleSummary struct {
	Family       string                       `json:"family"`
	CredentialID int64                        `json:"credential_id"`
	Version      int64                        `json:"version"`
	Digest       string                       `json:"digest"`
	Slots        []EnvironmentCapsuleSlotView `json:"slots"`
}

// EnvironmentCapsuleSlotView is one OS capsule without secrets.
type EnvironmentCapsuleSlotView struct {
	Slot        int    `json:"slot"`
	Environment string `json:"environment"`
	Digest      string `json:"digest"`
	Identity    string `json:"identity,omitempty"`
}

// EnvironmentCapsuleSummaryFromAccount builds a read-only summary when capsules exist.
func EnvironmentCapsuleSummaryFromAccount(account *Account) (*EnvironmentCapsuleSummary, error) {
	family, err := environmentCapsuleFamily(account)
	if err != nil {
		return nil, err
	}
	switch family {
	case EnvironmentCapsuleFamilyClaude:
		bundle, err := DecodeClaudeOAuthCredentialCapsules(account)
		if err != nil {
			return nil, err
		}
		slots := make([]EnvironmentCapsuleSlotView, 0, len(bundle.Capsules))
		for _, c := range bundle.Capsules {
			identity := ""
			if c.Profile != nil {
				identity = shortIdentity(c.Profile.DeviceID)
			}
			slots = append(slots, EnvironmentCapsuleSlotView{
				Slot: c.Slot, Environment: string(c.Environment), Digest: c.Digest, Identity: identity,
			})
		}
		return &EnvironmentCapsuleSummary{
			Family: family, CredentialID: bundle.CredentialID, Version: bundle.Version, Digest: bundle.Digest, Slots: slots,
		}, nil
	case EnvironmentCapsuleFamilyCodex:
		bundle, err := DecodeCodexOAuthCredentialCapsules(account)
		if err != nil {
			return nil, err
		}
		slots := make([]EnvironmentCapsuleSlotView, 0, len(bundle.Capsules))
		for _, c := range bundle.Capsules {
			identity := ""
			if c.Profile != nil {
				identity = shortIdentity(c.Profile.InstallationID)
			}
			slots = append(slots, EnvironmentCapsuleSlotView{
				Slot: c.Slot, Environment: string(c.Environment), Digest: c.Digest, Identity: identity,
			})
		}
		return &EnvironmentCapsuleSummary{
			Family: family, CredentialID: bundle.CredentialID, Version: bundle.Version, Digest: bundle.Digest, Slots: slots,
		}, nil
	case EnvironmentCapsuleFamilyGrok:
		bundle, err := DecodeGrokOAuthCredentialCapsules(account)
		if err != nil {
			return nil, err
		}
		slots := make([]EnvironmentCapsuleSlotView, 0, len(bundle.Capsules))
		for _, c := range bundle.Capsules {
			slots = append(slots, EnvironmentCapsuleSlotView{
				Slot: c.Slot, Environment: string(c.Environment), Digest: c.Digest, Identity: shortIdentity(c.ClientID),
			})
		}
		return &EnvironmentCapsuleSummary{
			Family: family, CredentialID: bundle.CredentialID, Version: bundle.Version, Digest: bundle.Digest, Slots: slots,
		}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported family", ErrOAuthPoolInvalid)
	}
}

func shortIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// --- Codex credential capsules ------------------------------------------------

// CodexOAuthCredentialCapsuleBundle binds three Codex OS capsules to one OpenAI OAuth credential.
type CodexOAuthCredentialCapsuleBundle struct {
	Schema       string                        `json:"schema"`
	Family       string                        `json:"family"`
	CredentialID int64                         `json:"credential_id"`
	Version      int64                         `json:"version"`
	Digest       string                        `json:"digest"`
	Timezone     string                        `json:"timezone,omitempty"`
	Capsules     []CodexOAuthCredentialCapsule `json:"capsules"`
	CreatedAt    time.Time                     `json:"created_at"`
	UpdatedAt    time.Time                     `json:"updated_at"`
}

// CodexOAuthCredentialCapsule is one frozen Codex environment capsule.
type CodexOAuthCredentialCapsule struct {
	Slot        int                      `json:"slot"`
	Environment EnvironmentClass         `json:"environment"`
	Digest      string                   `json:"digest"`
	Profile     *CodexEnvironmentProfile `json:"profile"`
}

func EnsureCodexOAuthCapsulesWithOptions(account *Account, profileTimezone string) (*CodexOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("%w: codex capsules require openai oauth credential", ErrOAuthPoolCredentialInvalid)
	}
	if account.ID <= 0 {
		return nil, fmt.Errorf("%w: account id is required before binding capsules", ErrOAuthPoolCredentialInvalid)
	}
	if bundle, err := DecodeCodexOAuthCredentialCapsules(account); err == nil {
		if err := validateCodexOAuthCredentialCapsuleBundle(bundle, account.ID); err == nil {
			return bundle, nil
		}
	}
	profileTimezone = NormalizeEnvironmentProfileTimezone(profileTimezone)
	profilePool, err := materializeCodexOAuthCapsuleProfilePool(account, profileTimezone)
	if err != nil {
		return nil, err
	}
	bundle, err := buildCodexOAuthCredentialCapsuleBundle(account.ID, 1, profileTimezone, profilePool)
	if err != nil {
		return nil, err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 2)
	}
	account.Extra[codexEnvironmentProfilePoolKey] = profilePool
	account.Extra[codexOAuthCredentialCapsulesKey] = bundle
	return bundle, nil
}

func DecodeCodexOAuthCredentialCapsules(account *Account) (*CodexOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Extra == nil {
		return nil, fmt.Errorf("%w: codex capsules missing", ErrOAuthPoolInvalid)
	}
	raw, ok := account.Extra[codexOAuthCredentialCapsulesKey]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%w: codex capsules missing", ErrOAuthPoolInvalid)
	}
	if bundle, ok := raw.(*CodexOAuthCredentialCapsuleBundle); ok {
		if err := validateCodexOAuthCredentialCapsuleBundle(bundle, account.ID); err != nil {
			return nil, err
		}
		return bundle, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var bundle CodexOAuthCredentialCapsuleBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, err
	}
	if err := validateCodexOAuthCredentialCapsuleBundle(&bundle, account.ID); err != nil {
		return nil, err
	}
	return &bundle, nil
}

func materializeCodexOAuthCapsuleProfilePool(account *Account, profileTimezone string) (*CodexEnvironmentProfilePool, error) {
	if account != nil && account.Extra != nil {
		if raw, ok := account.Extra[codexEnvironmentProfilePoolKey]; ok && raw != nil {
			if legacy, err := DecodeCodexEnvironmentProfilePool(raw); err == nil && legacy != nil {
				if legacy.IsV2() && len(legacy.Slots) == len(fixedClaudeEnvironmentSlotClasses) {
					for i := range legacy.Slots {
						if legacy.Slots[i].Profile != nil && strings.TrimSpace(legacy.Slots[i].Profile.Timezone) == "" {
							legacy.Slots[i].Profile.Timezone = profileTimezone
						}
					}
					return legacy, nil
				}
			}
		}
	}
	return newFrozenCodexEnvironmentProfilePoolWithTimezone(profileTimezone), nil
}

func buildCodexOAuthCredentialCapsuleBundle(credentialID, version int64, profileTimezone string, profilePool *CodexEnvironmentProfilePool) (*CodexOAuthCredentialCapsuleBundle, error) {
	if credentialID <= 0 || version <= 0 || profilePool == nil || len(profilePool.Slots) != len(fixedClaudeEnvironmentSlotClasses) {
		return nil, fmt.Errorf("%w: codex capsule pool must contain three frozen slots", ErrOAuthPoolInvalid)
	}
	now := time.Now().UTC()
	capsules := make([]CodexOAuthCredentialCapsule, 0, len(profilePool.Slots))
	for index := range profilePool.Slots {
		slot := profilePool.Slots[index]
		if slot.Profile == nil {
			return nil, fmt.Errorf("%w: codex capsule slot %d missing profile", ErrOAuthPoolInvalid, index)
		}
		expected := fixedClaudeEnvironmentSlotClasses[index]
		env := slot.Environment
		if env == "" {
			env = expected
		}
		digest, err := digestGenericCapsuleSlot(credentialID, index, string(env), slot.Profile.InstallationID, slot.Profile.SessionSeed, slot.Profile.UserAgent)
		if err != nil {
			return nil, err
		}
		capsules = append(capsules, CodexOAuthCredentialCapsule{
			Slot: index, Environment: expected, Digest: digest, Profile: slot.Profile,
		})
	}
	bundle := &CodexOAuthCredentialCapsuleBundle{
		Schema: claudeOAuthCapsuleSchemaV1, Family: EnvironmentCapsuleFamilyCodex,
		CredentialID: credentialID, Version: version, Timezone: profileTimezone,
		Capsules: capsules, CreatedAt: now, UpdatedAt: now,
	}
	digest, err := digestGenericCapsuleBundle(bundle.Schema, bundle.CredentialID, bundle.Version, capsulesToDigestParts(len(capsules), func(i int) (int, string, string) {
		return capsules[i].Slot, string(capsules[i].Environment), capsules[i].Digest
	}))
	if err != nil {
		return nil, err
	}
	bundle.Digest = digest
	return bundle, validateCodexOAuthCredentialCapsuleBundle(bundle, credentialID)
}

func validateCodexOAuthCredentialCapsuleBundle(bundle *CodexOAuthCredentialCapsuleBundle, expectedCredentialID int64) error {
	if bundle == nil || bundle.Schema != claudeOAuthCapsuleSchemaV1 || bundle.Family != EnvironmentCapsuleFamilyCodex {
		return fmt.Errorf("%w: invalid codex capsule bundle", ErrOAuthPoolInvalid)
	}
	if expectedCredentialID > 0 && bundle.CredentialID != expectedCredentialID {
		return fmt.Errorf("%w: codex capsule credential_id mismatch", ErrOAuthPoolInvalid)
	}
	if bundle.CredentialID <= 0 || bundle.Version <= 0 || len(bundle.Capsules) != len(fixedClaudeEnvironmentSlotClasses) {
		return fmt.Errorf("%w: codex capsule must contain exactly three system environments", ErrOAuthPoolInvalid)
	}
	for i, c := range bundle.Capsules {
		if c.Slot != i || c.Environment != fixedClaudeEnvironmentSlotClasses[i] || c.Profile == nil {
			return fmt.Errorf("%w: codex capsule slot %d invalid", ErrOAuthPoolInvalid, i)
		}
	}
	return nil
}

// --- Grok credential capsules -------------------------------------------------

// GrokOAuthCredentialCapsuleBundle binds three Grok CLI environment capsules to one Grok OAuth credential.
type GrokOAuthCredentialCapsuleBundle struct {
	Schema       string                       `json:"schema"`
	Family       string                       `json:"family"`
	CredentialID int64                        `json:"credential_id"`
	Version      int64                        `json:"version"`
	Digest       string                       `json:"digest"`
	Timezone     string                       `json:"timezone,omitempty"`
	Capsules     []GrokOAuthCredentialCapsule `json:"capsules"`
	CreatedAt    time.Time                    `json:"created_at"`
	UpdatedAt    time.Time                    `json:"updated_at"`
}

// GrokOAuthCredentialCapsule freezes one OS-shaped Grok CLI identity for a credential.
type GrokOAuthCredentialCapsule struct {
	Slot        int              `json:"slot"`
	Environment EnvironmentClass `json:"environment"`
	Digest      string           `json:"digest"`
	UserAgent   string           `json:"user_agent"`
	ClientID    string           `json:"client_id"`
	ClientVer   string           `json:"client_version"`
	TokenAuth   string           `json:"token_auth"`
	Timezone    string           `json:"timezone,omitempty"`
}

func EnsureGrokOAuthCapsulesWithOptions(account *Account, profileTimezone string) (*GrokOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Platform != PlatformGrok || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("%w: grok capsules require grok oauth credential", ErrOAuthPoolCredentialInvalid)
	}
	if account.ID <= 0 {
		return nil, fmt.Errorf("%w: account id is required before binding capsules", ErrOAuthPoolCredentialInvalid)
	}
	if bundle, err := DecodeGrokOAuthCredentialCapsules(account); err == nil {
		if err := validateGrokOAuthCredentialCapsuleBundle(bundle, account.ID); err == nil {
			return bundle, nil
		}
	}
	profileTimezone = NormalizeEnvironmentProfileTimezone(profileTimezone)
	bundle, err := buildGrokOAuthCredentialCapsuleBundle(account.ID, 1, profileTimezone)
	if err != nil {
		return nil, err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 1)
	}
	account.Extra[grokOAuthCredentialCapsulesKey] = bundle
	return bundle, nil
}

func DecodeGrokOAuthCredentialCapsules(account *Account) (*GrokOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Extra == nil {
		return nil, fmt.Errorf("%w: grok capsules missing", ErrOAuthPoolInvalid)
	}
	raw, ok := account.Extra[grokOAuthCredentialCapsulesKey]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%w: grok capsules missing", ErrOAuthPoolInvalid)
	}
	if bundle, ok := raw.(*GrokOAuthCredentialCapsuleBundle); ok {
		if err := validateGrokOAuthCredentialCapsuleBundle(bundle, account.ID); err != nil {
			return nil, err
		}
		return bundle, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var bundle GrokOAuthCredentialCapsuleBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, err
	}
	if err := validateGrokOAuthCredentialCapsuleBundle(&bundle, account.ID); err != nil {
		return nil, err
	}
	return &bundle, nil
}

func buildGrokOAuthCredentialCapsuleBundle(credentialID, version int64, profileTimezone string) (*GrokOAuthCredentialCapsuleBundle, error) {
	if credentialID <= 0 || version <= 0 {
		return nil, fmt.Errorf("%w: grok credential and version required", ErrOAuthPoolInvalid)
	}
	now := time.Now().UTC()
	// OS platform tags embedded in Grok CLI UA family strings.
	osTags := []string{"windows; x86_64", "darwin; arm64", "linux; x86_64"}
	capsules := make([]GrokOAuthCredentialCapsule, 0, 3)
	for i, env := range fixedClaudeEnvironmentSlotClasses {
		clientID, err := randomHexID(16)
		if err != nil {
			return nil, err
		}
		ua := rewriteGrokUserAgentOS(xai.DefaultCLIUserAgent, osTags[i])
		digest, err := digestGenericCapsuleSlot(credentialID, i, string(env), clientID, ua, xai.DefaultCLIClientVersion)
		if err != nil {
			return nil, err
		}
		capsules = append(capsules, GrokOAuthCredentialCapsule{
			Slot: i, Environment: env, Digest: digest,
			UserAgent: ua, ClientID: clientID,
			ClientVer: xai.DefaultCLIClientVersion, TokenAuth: xai.DefaultCLITokenAuth,
			Timezone: profileTimezone,
		})
	}
	bundle := &GrokOAuthCredentialCapsuleBundle{
		Schema: claudeOAuthCapsuleSchemaV1, Family: EnvironmentCapsuleFamilyGrok,
		CredentialID: credentialID, Version: version, Timezone: profileTimezone,
		Capsules: capsules, CreatedAt: now, UpdatedAt: now,
	}
	digest, err := digestGenericCapsuleBundle(bundle.Schema, bundle.CredentialID, bundle.Version, capsulesToDigestParts(len(capsules), func(i int) (int, string, string) {
		return capsules[i].Slot, string(capsules[i].Environment), capsules[i].Digest
	}))
	if err != nil {
		return nil, err
	}
	bundle.Digest = digest
	return bundle, validateGrokOAuthCredentialCapsuleBundle(bundle, credentialID)
}

func rewriteGrokUserAgentOS(base, osTag string) string {
	// Default: "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)"
	if i := strings.LastIndex(base, "("); i >= 0 {
		return strings.TrimSpace(base[:i]) + " (" + osTag + ")"
	}
	return base + " (" + osTag + ")"
}

func validateGrokOAuthCredentialCapsuleBundle(bundle *GrokOAuthCredentialCapsuleBundle, expectedCredentialID int64) error {
	if bundle == nil || bundle.Schema != claudeOAuthCapsuleSchemaV1 || bundle.Family != EnvironmentCapsuleFamilyGrok {
		return fmt.Errorf("%w: invalid grok capsule bundle", ErrOAuthPoolInvalid)
	}
	if expectedCredentialID > 0 && bundle.CredentialID != expectedCredentialID {
		return fmt.Errorf("%w: grok capsule credential_id mismatch", ErrOAuthPoolInvalid)
	}
	if bundle.CredentialID <= 0 || bundle.Version <= 0 || len(bundle.Capsules) != len(fixedClaudeEnvironmentSlotClasses) {
		return fmt.Errorf("%w: grok capsule must contain exactly three system environments", ErrOAuthPoolInvalid)
	}
	for i, c := range bundle.Capsules {
		if c.Slot != i || c.Environment != fixedClaudeEnvironmentSlotClasses[i] || strings.TrimSpace(c.UserAgent) == "" || strings.TrimSpace(c.ClientID) == "" {
			return fmt.Errorf("%w: grok capsule slot %d invalid", ErrOAuthPoolInvalid, i)
		}
	}
	return nil
}

func digestGenericCapsuleSlot(credentialID int64, slot int, env, idA, idB, idC string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"credential_id": credentialID,
		"slot":          slot,
		"environment":   env,
		"a":             idA,
		"b":             idB,
		"c":             idC,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func capsulesToDigestParts(n int, at func(int) (int, string, string)) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		slot, env, dig := at(i)
		out = append(out, map[string]any{"slot": slot, "environment": env, "digest": dig})
	}
	return out
}

func digestGenericCapsuleBundle(schema string, credentialID, version int64, slots []map[string]any) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"schema": schema, "credential_id": credentialID, "version": version, "capsules": slots,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func randomHexID(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ErrEnvironmentCapsuleIdentityLocked is returned when admin tries to edit frozen OAuth capsule identity.
var ErrEnvironmentCapsuleIdentityLocked = fmt.Errorf("%w: oauth environment capsules are auto-bound and not editable", ErrOAuthPoolInvalid)

// RejectEnvironmentCapsuleIdentityEdit blocks profile slot edits for capsule OAuth accounts.
func RejectEnvironmentCapsuleIdentityEdit(account *Account) error {
	if IsEnvironmentCapsuleOAuthAccount(account) {
		return ErrEnvironmentCapsuleIdentityLocked
	}
	return nil
}

// EnsureDefaultCLIVersion is exported for admin create path convenience.
func EnsureDefaultCLIVersion(account *Account) string {
	if account != nil && account.Platform == PlatformAnthropic {
		return claude.CLICurrentVersion
	}
	return ""
}
