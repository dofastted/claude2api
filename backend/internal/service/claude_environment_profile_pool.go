package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const claudeEnvironmentProfilePoolKey = "claude_environment_profile_pool"

type ClaudeEnvironmentProfileSlot struct {
	Slot        int                         `json:"slot"`
	Environment EnvironmentClass            `json:"environment"`
	State       EnvironmentProfileSlotState `json:"state"`
	Profile     *ClaudeEnvironmentProfile   `json:"profile"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type ClaudeEnvironmentProfilePool struct {
	mu       sync.Mutex                     `json:"-"`
	Version  int                            `json:"version"`
	Capacity int                            `json:"capacity"`
	Slots    []ClaudeEnvironmentProfileSlot `json:"slots"`
}

func DecodeClaudeEnvironmentProfilePool(raw any) (*ClaudeEnvironmentProfilePool, error) {
	if raw == nil {
		return nil, nil
	}
	if pool, ok := raw.(*ClaudeEnvironmentProfilePool); ok {
		return pool, nil
	}
	if pool, ok := raw.(ClaudeEnvironmentProfilePool); ok {
		return &pool, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var pool ClaudeEnvironmentProfilePool
	if err := json.Unmarshal(encoded, &pool); err != nil {
		return nil, err
	}
	if pool.Version == 0 && pool.Capacity == 0 && len(pool.Slots) == 0 {
		return nil, nil
	}
	if err := pool.Normalize(); err != nil {
		return nil, err
	}
	return &pool, nil
}

func (p *ClaudeEnvironmentProfilePool) Normalize() error {
	if p == nil {
		return fmt.Errorf("claude environment profile pool is required")
	}
	if p.Version <= 0 {
		p.Version = 1
	}
	if p.Capacity <= 0 {
		p.Capacity = 1
	}
	seen := make(map[int]struct{}, len(p.Slots))
	for i := range p.Slots {
		slot := &p.Slots[i]
		if slot.Slot < 0 {
			return fmt.Errorf("claude environment profile pool slot must be non-negative")
		}
		if _, exists := seen[slot.Slot]; exists {
			return fmt.Errorf("duplicate claude environment profile pool slot %d", slot.Slot)
		}
		seen[slot.Slot] = struct{}{}
		slot.Environment = normalizeEnvironmentClass(slot.Environment)
		if slot.State == "" {
			if slot.Profile == nil {
				slot.State = EnvironmentProfileSlotEmpty
			} else {
				slot.State = EnvironmentProfileSlotBound
			}
		}
		switch slot.State {
		case EnvironmentProfileSlotEmpty:
			slot.Profile = nil
		case EnvironmentProfileSlotBound:
			if slot.Profile == nil {
				return fmt.Errorf("bound claude environment profile slot %d has no profile", slot.Slot)
			}
			if err := ValidateClaudeEnvironmentProfile(slot.Profile); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported claude environment profile slot state %s", slot.State)
		}
		if slot.CreatedAt.IsZero() {
			slot.CreatedAt = nowForEnvironmentProfilePool()
		}
		if slot.UpdatedAt.IsZero() {
			slot.UpdatedAt = slot.CreatedAt
		}
	}
	return nil
}

func newClaudeEnvironmentProfilePool(capacity int) *ClaudeEnvironmentProfilePool {
	if capacity <= 0 {
		capacity = 1
	}
	now := nowForEnvironmentProfilePool()
	pool := &ClaudeEnvironmentProfilePool{Version: 1, Capacity: capacity, Slots: make([]ClaudeEnvironmentProfileSlot, capacity)}
	for i := 0; i < capacity; i++ {
		pool.Slots[i] = ClaudeEnvironmentProfileSlot{Slot: i, State: EnvironmentProfileSlotEmpty, Environment: EnvironmentClassWindows, CreatedAt: now, UpdatedAt: now}
	}
	return pool
}

func getOrCreateClaudeEnvironmentProfilePool(account *Account) (*ClaudeEnvironmentProfilePool, error) {
	capacity := environmentProfileCapacity(account)
	if account != nil && account.Extra != nil {
		if pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey]); err != nil {
			return nil, err
		} else if pool != nil {
			ensureClaudeEnvironmentProfilePoolCapacity(pool, capacity)
			return pool, nil
		}
		if profile, ok := account.GetClaudeEnvironmentProfile(); ok {
			pool := newClaudeEnvironmentProfilePool(capacity)
			env := environmentClassFromClaudeProfile(profile)
			pool.Slots[0].Environment = env
			pool.Slots[0].State = EnvironmentProfileSlotBound
			pool.Slots[0].Profile = profile
			return pool, nil
		}
	}
	return newClaudeEnvironmentProfilePool(capacity), nil
}

func ensureClaudeEnvironmentProfilePoolCapacity(pool *ClaudeEnvironmentProfilePool, capacity int) {
	if pool == nil {
		return
	}
	if capacity <= 0 {
		capacity = 1
	}
	pool.Capacity = capacity
	existing := make(map[int]struct{}, len(pool.Slots))
	for _, slot := range pool.Slots {
		existing[slot.Slot] = struct{}{}
	}
	now := nowForEnvironmentProfilePool()
	for i := 0; i < capacity; i++ {
		if _, ok := existing[i]; !ok {
			pool.Slots = append(pool.Slots, ClaudeEnvironmentProfileSlot{Slot: i, State: EnvironmentProfileSlotEmpty, Environment: EnvironmentClassWindows, CreatedAt: now, UpdatedAt: now})
		}
	}
}

func acquireClaudeEnvironmentProfileSlot(pool *ClaudeEnvironmentProfilePool, manager *EnvironmentProfileSlotLeaseManager, account *Account, env EnvironmentClass, requestID string, build func(EnvironmentClass) (*ClaudeEnvironmentProfile, error)) (*EnvironmentProfileSlotLease, *ClaudeEnvironmentProfile, error) {
	if pool == nil || account == nil {
		return nil, nil, ErrNoEnvironmentProfileSlot
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	env = normalizeEnvironmentClass(env)
	if err := pool.Normalize(); err != nil {
		return nil, nil, err
	}
	capacity := pool.Capacity
	lease, err := manager.acquire(account.ID, capacity, requestID, func(isActive func(int) bool) (int, error) {
		if slot, ok := findClaudeEnvironmentProfileSlot(pool, env, false, isActive); ok {
			return slot, nil
		}
		if slot, ok := findClaudeEnvironmentProfileSlot(pool, env, true, isActive); ok {
			return slot, nil
		}
		return -1, ErrNoEnvironmentProfileSlot
	})
	if err != nil {
		return nil, nil, err
	}
	for i := range pool.Slots {
		if pool.Slots[i].Slot != lease.Slot {
			continue
		}
		if pool.Slots[i].State == EnvironmentProfileSlotEmpty {
			profile, err := build(env)
			if err != nil {
				lease.ReleaseFunc()
				return nil, nil, err
			}
			pool.Slots[i].Environment = env
			pool.Slots[i].State = EnvironmentProfileSlotBound
			pool.Slots[i].Profile = profile
			pool.Slots[i].UpdatedAt = nowForEnvironmentProfilePool()
			lease.BoundNew = true
		}
		lease.Environment = pool.Slots[i].Environment
		return lease, pool.Slots[i].Profile, nil
	}
	lease.ReleaseFunc()
	return nil, nil, ErrNoEnvironmentProfileSlot
}

func findClaudeEnvironmentProfileSlot(pool *ClaudeEnvironmentProfilePool, env EnvironmentClass, empty bool, isActive func(int) bool) (int, bool) {
	for _, slot := range pool.Slots {
		if slot.Slot < 0 || slot.Slot >= pool.Capacity || isActive(slot.Slot) {
			continue
		}
		if empty {
			if slot.State == EnvironmentProfileSlotEmpty {
				return slot.Slot, true
			}
			continue
		}
		if slot.State == EnvironmentProfileSlotBound && slot.Environment == env {
			return slot.Slot, true
		}
	}
	return -1, false
}

func buildClaudeEnvironmentProfileForClass(env EnvironmentClass) *ClaudeEnvironmentProfile {
	profile := defaultClaudeCodeEnvironmentProfile(nil)
	profile.Source = "auto_default_pool"
	switch normalizeEnvironmentClass(env) {
	case EnvironmentClassWindows:
		profile.Platform = "windows"
		profile.PlatformRaw = "windows"
		profile.Arch = "x64"
	case EnvironmentClassLinux:
		profile.Platform = "linux"
		profile.PlatformRaw = "linux"
		profile.Arch = "x64"
	case EnvironmentClassMacOS:
		profile.Platform = "darwin"
		profile.PlatformRaw = "darwin"
		profile.Arch = "arm64"
	case EnvironmentClassDesktop:
		profile.Family = ClaudeClientFamilyDesktop
		profile.ClientType = "desktop"
		profile.XApp = "claude-desktop"
		profile.Platform = "windows"
		profile.PlatformRaw = "windows"
		profile.Arch = "x64"
	}
	profile.UpdatedAt = nowForEnvironmentProfilePool()
	return profile
}

func (s *GatewayService) acquireClaudeEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header, body []byte) (*EnvironmentProfileSlotLease, *ClaudeEnvironmentProfile, error) {
	if s == nil || account == nil || !account.IsClaudeSingleEnvironmentEnabled() {
		return nil, nil, nil
	}
	if s.accountRepo == nil && !accountHasClaudeEnvironmentProfileSource(account) {
		return nil, nil, nil
	}
	if s.claudeEnvironmentProfileSlotLeases == nil {
		s.claudeEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	unlock := s.claudeEnvironmentProfileSlotLeases.lockAccount(account.ID)
	defer unlock()
	if s.accountRepo != nil {
		if fresh, err := s.accountRepo.GetByID(ctx, account.ID); err == nil && fresh != nil {
			account = fresh
		} else if err != nil && err != ErrAccountNotFound {
			return nil, nil, err
		}
	}
	pool, err := getOrCreateClaudeEnvironmentProfilePool(account)
	if err != nil {
		return nil, nil, err
	}
	env := DetectClaudeEnvironmentClass(headers, body)
	lease, profile, err := acquireClaudeEnvironmentProfileSlot(pool, s.claudeEnvironmentProfileSlotLeases, account, env, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	if err != nil {
		if err == ErrNoEnvironmentProfileSlot {
			return nil, nil, environmentProfileSlotExhaustedError()
		}
		return nil, nil, err
	}
	if lease != nil && lease.BoundNew && s.accountRepo != nil {
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{claudeEnvironmentProfilePoolKey: pool}); err != nil {
			lease.ReleaseFunc()
			return nil, nil, err
		}
	}
	return lease, profile, nil
}

func environmentClassFromClaudeProfile(profile *ClaudeEnvironmentProfile) EnvironmentClass {
	if profile == nil {
		return EnvironmentClassWindows
	}
	if profile.Family == ClaudeClientFamilyDesktop || strings.EqualFold(profile.ClientType, "desktop") || strings.EqualFold(profile.XApp, "claude-desktop") {
		return EnvironmentClassDesktop
	}
	return detectEnvironmentClassFromHeaders(httpHeaderFromProfileFields(profile.UserAgent, profile.PlatformRaw, profile.Platform))
}

func accountHasClaudeEnvironmentProfileSource(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	if _, ok := account.GetClaudeEnvironmentProfile(); ok {
		return true
	}
	pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
	return err == nil && pool != nil
}

func httpHeaderFromProfileFields(ua string, values ...string) http.Header {
	headers := http.Header{"User-Agent": []string{ua}}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			headers["X-Stainless-OS"] = []string{value}
			break
		}
	}
	return headers
}
