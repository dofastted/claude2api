package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/claude"
)

const (
	// claudeOAuthCredentialCapsulesKey stores the credential-owned capsule bundle on account.extra.
	claudeOAuthCredentialCapsulesKey = "claude_oauth_credential_capsules"
	claudeOAuthCapsuleSchemaV1       = "claude-oauth-capsule-v1"
	// legacy pool-level payload key kept for decode/migrate of 2af181e2 capsule sets.
	claudeOAuthCapsulePayloadKey = "environment_profile_pool"
)

// ClaudeOAuthCredentialCapsuleBundle is the durable environment fact source for one Anthropic OAuth credential.
// Exactly three system capsules (windows/macos/linux) are bound to the credential automatically.
type ClaudeOAuthCredentialCapsuleBundle struct {
	Schema       string                         `json:"schema"`
	CredentialID int64                          `json:"credential_id"`
	Version      int64                          `json:"version"`
	Digest       string                         `json:"digest"`
	CLIVersion   string                         `json:"cli_version,omitempty"`
	Timezone     string                         `json:"timezone,omitempty"`
	Capsules     []ClaudeOAuthCredentialCapsule `json:"capsules"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
}

// ClaudeOAuthCredentialCapsule is one frozen environment capsule for a credential slot.
type ClaudeOAuthCredentialCapsule struct {
	Slot        int                       `json:"slot"`
	Environment EnvironmentClass          `json:"environment"`
	Digest      string                    `json:"digest"`
	Profile     *ClaudeEnvironmentProfile `json:"profile"`
}

// EnsureClaudeOAuthCapsules guarantees the account has a valid credential-owned 3-capsule bundle.
// Existing complete bundles are returned as-is. Missing bundles are generated from a frozen
// profile pool, preferring identity continuity from account.extra.claude_environment_profile_pool.
// On success the bundle is written into account.Extra (caller persists via UpdateExtra when needed).
func EnsureClaudeOAuthCapsules(account *Account) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	return EnsureClaudeOAuthCapsulesWithOptions(account, "", "")
}

// EnsureClaudeOAuthCapsulesWithOptions is EnsureClaudeOAuthCapsules with optional CLI/timezone overrides
// used when first materializing a bundle (empty values fall back to defaults).
func EnsureClaudeOAuthCapsulesWithOptions(account *Account, cliVersion, profileTimezone string) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	if account == nil {
		return nil, fmt.Errorf("%w: account is required", ErrOAuthPoolCredentialInvalid)
	}
	if account.Platform != PlatformAnthropic || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("%w: capsules require anthropic oauth credential", ErrOAuthPoolCredentialInvalid)
	}
	if account.ID <= 0 {
		return nil, fmt.Errorf("%w: account id is required before binding capsules", ErrOAuthPoolCredentialInvalid)
	}

	if bundle, err := DecodeClaudeOAuthCredentialCapsules(account); err == nil {
		if err := validateClaudeOAuthCredentialCapsuleBundle(bundle, account.ID); err == nil {
			return bundle, nil
		}
	}

	cliVersion = strings.TrimSpace(cliVersion)
	if cliVersion == "" {
		cliVersion = claude.CLICurrentVersion
	}
	profileTimezone = NormalizeEnvironmentProfileTimezone(profileTimezone)

	profilePool, err := materializeClaudeOAuthCapsuleProfilePool(account, cliVersion, profileTimezone)
	if err != nil {
		return nil, err
	}
	bundle, err := buildClaudeOAuthCredentialCapsuleBundle(account.ID, 1, cliVersion, profileTimezone, profilePool)
	if err != nil {
		return nil, err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 2)
	}
	// Keep legacy profile pool in sync so non-capsule code paths remain coherent during transition.
	account.Extra[claudeEnvironmentProfilePoolKey] = profilePool
	account.Extra[claudeOAuthCredentialCapsulesKey] = bundle
	return bundle, nil
}

// DecodeClaudeOAuthCredentialCapsules loads a capsule bundle from account.extra without generating.
func DecodeClaudeOAuthCredentialCapsules(account *Account) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Extra == nil {
		return nil, fmt.Errorf("%w: credential capsules are missing", ErrOAuthPoolInvalid)
	}
	raw, ok := account.Extra[claudeOAuthCredentialCapsulesKey]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%w: credential capsules are missing", ErrOAuthPoolInvalid)
	}
	if bundle, ok := raw.(*ClaudeOAuthCredentialCapsuleBundle); ok {
		if err := validateClaudeOAuthCredentialCapsuleBundle(bundle, account.ID); err != nil {
			return nil, err
		}
		return bundle, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode credential capsules: %w", err)
	}
	var bundle ClaudeOAuthCredentialCapsuleBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, fmt.Errorf("decode credential capsules: %w", err)
	}
	if err := validateClaudeOAuthCredentialCapsuleBundle(&bundle, account.ID); err != nil {
		return nil, err
	}
	return &bundle, nil
}

// ClaudeOAuthCredentialCapsuleProfile returns the frozen profile for a credential capsule slot.
func ClaudeOAuthCredentialCapsuleProfile(bundle *ClaudeOAuthCredentialCapsuleBundle, slot int) (*ClaudeEnvironmentProfile, error) {
	if slot < 0 || slot >= len(fixedClaudeEnvironmentSlotClasses) {
		return nil, fmt.Errorf("%w: capsule slot must be between 0 and 2", ErrOAuthPoolInvalid)
	}
	if err := validateClaudeOAuthCredentialCapsuleBundleStructure(bundle); err != nil {
		return nil, err
	}
	for index := range bundle.Capsules {
		if bundle.Capsules[index].Slot == slot {
			if bundle.Capsules[index].Profile == nil {
				return nil, fmt.Errorf("%w: capsule slot %d has no profile", ErrOAuthPoolInvalid, slot)
			}
			return bundle.Capsules[index].Profile, nil
		}
	}
	return nil, fmt.Errorf("%w: capsule slot %d is missing", ErrOAuthPoolInvalid, slot)
}

// BuildClaudeOAuthCapsuleSet builds a legacy pool-level template (shared 3-slot profile pool).
// Prefer EnsureClaudeOAuthCapsules for the credential-owned path. Kept for decode/tests of old pool capsule sets.
func BuildClaudeOAuthCapsuleSet(poolID, version int64, cliVersion, profileTimezone string) (*OAuthCapsuleSet, error) {
	if poolID <= 0 || version <= 0 {
		return nil, fmt.Errorf("%w: pool and version are required", ErrOAuthPoolInvalid)
	}
	profilePool := newFrozenClaudeEnvironmentProfilePoolWithTimezone(cliVersion, profileTimezone)
	if err := validateClaudeOAuthCapsuleProfilePool(profilePool); err != nil {
		return nil, err
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"schema":                     claudeOAuthCapsuleSchemaV1,
		claudeOAuthCapsulePayloadKey: profilePool,
	})
	if err != nil {
		return nil, fmt.Errorf("encode claude oauth capsule set: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode claude oauth capsule payload: %w", err)
	}
	digest := sha256.Sum256(payloadBytes)
	return &OAuthCapsuleSet{
		PoolID:              poolID,
		Version:             version,
		CompatibilityDigest: hex.EncodeToString(digest[:]),
		Payload:             payload,
	}, nil
}

func DecodeClaudeOAuthCapsuleProfilePool(set *OAuthCapsuleSet) (*ClaudeEnvironmentProfilePool, error) {
	if set == nil || set.PoolID <= 0 || set.Version <= 0 || set.Payload == nil {
		return nil, fmt.Errorf("%w: capsule set is incomplete", ErrOAuthPoolInvalid)
	}
	raw, ok := set.Payload[claudeOAuthCapsulePayloadKey]
	if !ok {
		return nil, fmt.Errorf("%w: capsule profile pool is missing", ErrOAuthPoolInvalid)
	}
	profilePool, err := DecodeClaudeEnvironmentProfilePool(raw)
	if err != nil {
		return nil, fmt.Errorf("decode claude oauth capsule profile pool: %w", err)
	}
	if err := validateClaudeOAuthCapsuleProfilePool(profilePool); err != nil {
		return nil, err
	}
	return profilePool, nil
}

// ClaudeOAuthCapsuleProfile resolves a slot from a legacy pool-level capsule set.
func ClaudeOAuthCapsuleProfile(set *OAuthCapsuleSet, slot int) (*ClaudeEnvironmentProfile, error) {
	if slot < 0 || slot >= len(fixedClaudeEnvironmentSlotClasses) {
		return nil, fmt.Errorf("%w: capsule slot must be between 0 and 2", ErrOAuthPoolInvalid)
	}
	profilePool, err := DecodeClaudeOAuthCapsuleProfilePool(set)
	if err != nil {
		return nil, err
	}
	for index := range profilePool.Slots {
		candidate := &profilePool.Slots[index]
		if candidate.Slot == slot {
			if candidate.Profile == nil {
				return nil, fmt.Errorf("%w: capsule slot %d has no profile", ErrOAuthPoolInvalid, slot)
			}
			return candidate.Profile, nil
		}
	}
	return nil, fmt.Errorf("%w: capsule slot %d is missing", ErrOAuthPoolInvalid, slot)
}

func materializeClaudeOAuthCapsuleProfilePool(account *Account, cliVersion, profileTimezone string) (*ClaudeEnvironmentProfilePool, error) {
	if account != nil && account.Extra != nil {
		if raw, ok := account.Extra[claudeEnvironmentProfilePoolKey]; ok && raw != nil {
			if legacy, err := DecodeClaudeEnvironmentProfilePool(raw); err == nil && legacy != nil {
				if legacy.IsV2() {
					if err := validateClaudeOAuthCapsuleProfilePool(legacy); err == nil {
						// re-stamp timezone if profiles lack it
						for i := range legacy.Slots {
							if legacy.Slots[i].Profile != nil && strings.TrimSpace(legacy.Slots[i].Profile.Timezone) == "" {
								legacy.Slots[i].Profile.Timezone = profileTimezone
							}
						}
						return legacy, nil
					}
				}
				upgraded := upgradeLegacyClaudePoolToV2WithTimezone(legacy, cliVersion, profileTimezone)
				if err := validateClaudeOAuthCapsuleProfilePool(upgraded); err != nil {
					return nil, err
				}
				return upgraded, nil
			}
		}
	}
	profilePool := newFrozenClaudeEnvironmentProfilePoolWithTimezone(cliVersion, profileTimezone)
	if err := validateClaudeOAuthCapsuleProfilePool(profilePool); err != nil {
		return nil, err
	}
	return profilePool, nil
}

func buildClaudeOAuthCredentialCapsuleBundle(credentialID, version int64, cliVersion, profileTimezone string, profilePool *ClaudeEnvironmentProfilePool) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	if credentialID <= 0 || version <= 0 {
		return nil, fmt.Errorf("%w: credential and version are required", ErrOAuthPoolInvalid)
	}
	if err := validateClaudeOAuthCapsuleProfilePool(profilePool); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	capsules := make([]ClaudeOAuthCredentialCapsule, 0, len(profilePool.Slots))
	for index := range profilePool.Slots {
		slot := profilePool.Slots[index]
		profile := slot.Profile
		slotDigest, err := digestClaudeOAuthCapsuleSlot(credentialID, slot.Slot, slot.Environment, profile)
		if err != nil {
			return nil, err
		}
		capsules = append(capsules, ClaudeOAuthCredentialCapsule{
			Slot:        slot.Slot,
			Environment: slot.Environment,
			Digest:      slotDigest,
			Profile:     profile,
		})
	}
	bundle := &ClaudeOAuthCredentialCapsuleBundle{
		Schema:       claudeOAuthCapsuleSchemaV1,
		CredentialID: credentialID,
		Version:      version,
		CLIVersion:   cliVersion,
		Timezone:     profileTimezone,
		Capsules:     capsules,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	digest, err := digestClaudeOAuthCredentialCapsuleBundle(bundle)
	if err != nil {
		return nil, err
	}
	bundle.Digest = digest
	if err := validateClaudeOAuthCredentialCapsuleBundle(bundle, credentialID); err != nil {
		return nil, err
	}
	return bundle, nil
}

func validateClaudeOAuthCapsuleProfilePool(profilePool *ClaudeEnvironmentProfilePool) error {
	if profilePool == nil || !profilePool.IsV2() || profilePool.Capacity != len(fixedClaudeEnvironmentSlotClasses) || len(profilePool.Slots) != len(fixedClaudeEnvironmentSlotClasses) {
		return fmt.Errorf("%w: capsule must contain the frozen windows, macos and linux slots", ErrOAuthPoolInvalid)
	}
	seen := make(map[EnvironmentClass]struct{}, len(profilePool.Slots))
	for index := range profilePool.Slots {
		slot := &profilePool.Slots[index]
		if slot.Slot < 0 || slot.Slot >= len(fixedClaudeEnvironmentSlotClasses) || slot.State != EnvironmentProfileSlotBound || slot.Profile == nil {
			return fmt.Errorf("%w: capsule slot %d is invalid", ErrOAuthPoolInvalid, slot.Slot)
		}
		expected := fixedClaudeEnvironmentSlotClasses[slot.Slot]
		if slot.Environment != expected {
			return fmt.Errorf("%w: capsule slot %d environment mismatch", ErrOAuthPoolInvalid, slot.Slot)
		}
		if _, exists := seen[slot.Environment]; exists {
			return fmt.Errorf("%w: duplicate capsule environment %s", ErrOAuthPoolInvalid, slot.Environment)
		}
		seen[slot.Environment] = struct{}{}
		if !isV2ClaudeEnvironmentProfile(slot.Profile) {
			return fmt.Errorf("%w: capsule slot %d is not frozen", ErrOAuthPoolInvalid, slot.Slot)
		}
	}
	return nil
}

func validateClaudeOAuthCredentialCapsuleBundle(bundle *ClaudeOAuthCredentialCapsuleBundle, expectedCredentialID int64) error {
	if err := validateClaudeOAuthCredentialCapsuleBundleStructure(bundle); err != nil {
		return err
	}
	if expectedCredentialID > 0 && bundle.CredentialID != expectedCredentialID {
		return fmt.Errorf("%w: capsule credential_id mismatch", ErrOAuthPoolInvalid)
	}
	return nil
}

func validateClaudeOAuthCredentialCapsuleBundleStructure(bundle *ClaudeOAuthCredentialCapsuleBundle) error {
	if bundle == nil {
		return fmt.Errorf("%w: capsule bundle is nil", ErrOAuthPoolInvalid)
	}
	if bundle.Schema != claudeOAuthCapsuleSchemaV1 {
		return fmt.Errorf("%w: unsupported capsule schema %q", ErrOAuthPoolInvalid, bundle.Schema)
	}
	if bundle.CredentialID <= 0 || bundle.Version <= 0 {
		return fmt.Errorf("%w: capsule credential and version are required", ErrOAuthPoolInvalid)
	}
	if len(bundle.Capsules) != len(fixedClaudeEnvironmentSlotClasses) {
		return fmt.Errorf("%w: capsule must contain exactly three system environments", ErrOAuthPoolInvalid)
	}
	seen := make(map[EnvironmentClass]struct{}, len(bundle.Capsules))
	for index := range bundle.Capsules {
		capsule := &bundle.Capsules[index]
		if capsule.Slot < 0 || capsule.Slot >= len(fixedClaudeEnvironmentSlotClasses) || capsule.Profile == nil {
			return fmt.Errorf("%w: capsule slot %d is invalid", ErrOAuthPoolInvalid, capsule.Slot)
		}
		expected := fixedClaudeEnvironmentSlotClasses[capsule.Slot]
		if capsule.Environment != expected {
			return fmt.Errorf("%w: capsule slot %d environment mismatch", ErrOAuthPoolInvalid, capsule.Slot)
		}
		if _, exists := seen[capsule.Environment]; exists {
			return fmt.Errorf("%w: duplicate capsule environment %s", ErrOAuthPoolInvalid, capsule.Environment)
		}
		seen[capsule.Environment] = struct{}{}
		if !isV2ClaudeEnvironmentProfile(capsule.Profile) {
			return fmt.Errorf("%w: capsule slot %d is not frozen", ErrOAuthPoolInvalid, capsule.Slot)
		}
	}
	return nil
}

func digestClaudeOAuthCapsuleSlot(credentialID int64, slot int, env EnvironmentClass, profile *ClaudeEnvironmentProfile) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"credential_id": credentialID,
		"slot":          slot,
		"environment":   env,
		"device_id":     profile.DeviceID,
		"client_id":     profile.ClientID,
		"session_seed":  profile.SessionSeed,
		"cli_version":   profile.ClientVersion,
		"timezone":      profile.Timezone,
		"user_agent":    profile.UserAgent,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func digestClaudeOAuthCredentialCapsuleBundle(bundle *ClaudeOAuthCredentialCapsuleBundle) (string, error) {
	type slotDigest struct {
		Slot        int    `json:"slot"`
		Environment string `json:"environment"`
		Digest      string `json:"digest"`
	}
	slots := make([]slotDigest, 0, len(bundle.Capsules))
	for _, capsule := range bundle.Capsules {
		slots = append(slots, slotDigest{
			Slot:        capsule.Slot,
			Environment: string(capsule.Environment),
			Digest:      capsule.Digest,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"schema":        bundle.Schema,
		"credential_id": bundle.CredentialID,
		"version":       bundle.Version,
		"capsules":      slots,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func persistClaudeOAuthCredentialCapsules(account *Account, bundle *ClaudeOAuthCredentialCapsuleBundle) map[string]any {
	updates := map[string]any{
		claudeOAuthCredentialCapsulesKey: bundle,
	}
	if account != nil && account.Extra != nil {
		if pool, ok := account.Extra[claudeEnvironmentProfilePoolKey]; ok {
			updates[claudeEnvironmentProfilePoolKey] = pool
		}
	}
	return updates
}

// PublishClaudeOAuthCapsules creates a new credential-local capsule version (copy-on-write).
// Existing session bindings keep their recorded version semantics; new sessions use the new version.
func PublishClaudeOAuthCapsules(account *Account, cliVersion, profileTimezone string) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	if account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("%w: publish requires anthropic oauth credential", ErrOAuthPoolCredentialInvalid)
	}
	if account.ID <= 0 {
		return nil, fmt.Errorf("%w: account id is required", ErrOAuthPoolCredentialInvalid)
	}
	nextVersion := int64(1)
	if current, err := DecodeClaudeOAuthCredentialCapsules(account); err == nil && current != nil {
		nextVersion = current.Version + 1
	}
	cliVersion = strings.TrimSpace(cliVersion)
	if cliVersion == "" {
		cliVersion = claude.CLICurrentVersion
	}
	profileTimezone = NormalizeEnvironmentProfileTimezone(profileTimezone)
	profilePool, err := materializeClaudeOAuthCapsuleProfilePool(account, cliVersion, profileTimezone)
	if err != nil {
		return nil, err
	}
	// Force fresh frozen identities for a true COW publish (do not reuse in-memory pool pointer mutation).
	profilePool = newFrozenClaudeEnvironmentProfilePoolWithTimezone(cliVersion, profileTimezone)
	bundle, err := buildClaudeOAuthCredentialCapsuleBundle(account.ID, nextVersion, cliVersion, profileTimezone, profilePool)
	if err != nil {
		return nil, err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 2)
	}
	account.Extra[claudeEnvironmentProfilePoolKey] = profilePool
	account.Extra[claudeOAuthCredentialCapsulesKey] = bundle
	return bundle, nil
}
