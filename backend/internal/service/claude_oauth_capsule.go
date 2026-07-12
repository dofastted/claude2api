package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const claudeOAuthCapsulePayloadKey = "environment_profile_pool"

func BuildClaudeOAuthCapsuleSet(poolID, version int64, cliVersion, profileTimezone string) (*OAuthCapsuleSet, error) {
	if poolID <= 0 || version <= 0 {
		return nil, fmt.Errorf("%w: pool and version are required", ErrOAuthPoolInvalid)
	}
	profilePool := newFrozenClaudeEnvironmentProfilePoolWithTimezone(cliVersion, profileTimezone)
	if err := validateClaudeOAuthCapsuleProfilePool(profilePool); err != nil {
		return nil, err
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"schema":                     "claude-oauth-capsule-v1",
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
