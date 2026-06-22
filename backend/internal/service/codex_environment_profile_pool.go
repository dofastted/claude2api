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

const codexEnvironmentProfilePoolKey = "codex_environment_profile_pool"

type CodexEnvironmentProfileSlot struct {
	Slot        int                         `json:"slot"`
	Environment EnvironmentClass            `json:"environment"`
	State       EnvironmentProfileSlotState `json:"state"`
	Profile     *CodexEnvironmentProfile    `json:"profile"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type CodexEnvironmentProfilePool struct {
	mu       sync.Mutex                    `json:"-"`
	Version  int                           `json:"version"`
	Capacity int                           `json:"capacity"`
	Slots    []CodexEnvironmentProfileSlot `json:"slots"`
}

func DecodeCodexEnvironmentProfilePool(raw any) (*CodexEnvironmentProfilePool, error) {
	if raw == nil {
		return nil, nil
	}
	if pool, ok := raw.(*CodexEnvironmentProfilePool); ok {
		return pool, nil
	}
	if pool, ok := raw.(CodexEnvironmentProfilePool); ok {
		return &pool, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var pool CodexEnvironmentProfilePool
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

func (p *CodexEnvironmentProfilePool) Normalize() error {
	if p == nil {
		return fmt.Errorf("codex environment profile pool is required")
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
			return fmt.Errorf("codex environment profile pool slot must be non-negative")
		}
		if _, exists := seen[slot.Slot]; exists {
			return fmt.Errorf("duplicate codex environment profile pool slot %d", slot.Slot)
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
				return fmt.Errorf("bound codex environment profile slot %d has no profile", slot.Slot)
			}
			if err := slot.Profile.Validate(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported codex environment profile slot state %s", slot.State)
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

func newCodexEnvironmentProfilePool(capacity int) *CodexEnvironmentProfilePool {
	if capacity <= 0 {
		capacity = 1
	}
	now := nowForEnvironmentProfilePool()
	pool := &CodexEnvironmentProfilePool{Version: 1, Capacity: capacity, Slots: make([]CodexEnvironmentProfileSlot, capacity)}
	for i := 0; i < capacity; i++ {
		pool.Slots[i] = CodexEnvironmentProfileSlot{Slot: i, State: EnvironmentProfileSlotEmpty, Environment: EnvironmentClassWindows, CreatedAt: now, UpdatedAt: now}
	}
	return pool
}

func getOrCreateCodexEnvironmentProfilePool(account *Account) (*CodexEnvironmentProfilePool, error) {
	capacity := environmentProfileCapacity(account)
	if account != nil && account.Extra != nil {
		if pool, err := DecodeCodexEnvironmentProfilePool(account.Extra[codexEnvironmentProfilePoolKey]); err != nil {
			return nil, err
		} else if pool != nil {
			ensureCodexEnvironmentProfilePoolCapacity(pool, capacity)
			return pool, nil
		}
		if profile, ok := account.GetCodexEnvironmentProfile(); ok {
			pool := newCodexEnvironmentProfilePool(capacity)
			env := environmentClassFromCodexProfile(profile)
			pool.Slots[0].Environment = env
			pool.Slots[0].State = EnvironmentProfileSlotBound
			pool.Slots[0].Profile = profile
			return pool, nil
		}
	}
	return newCodexEnvironmentProfilePool(capacity), nil
}

func ensureCodexEnvironmentProfilePoolCapacity(pool *CodexEnvironmentProfilePool, capacity int) {
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
			pool.Slots = append(pool.Slots, CodexEnvironmentProfileSlot{Slot: i, State: EnvironmentProfileSlotEmpty, Environment: EnvironmentClassWindows, CreatedAt: now, UpdatedAt: now})
		}
	}
}

func acquireCodexEnvironmentProfileSlot(pool *CodexEnvironmentProfilePool, manager *EnvironmentProfileSlotLeaseManager, account *Account, env EnvironmentClass, requestID string, build func(EnvironmentClass) (*CodexEnvironmentProfile, error)) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
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
		if slot, ok := findCodexEnvironmentProfileSlot(pool, env, false, isActive); ok {
			return slot, nil
		}
		if slot, ok := findCodexEnvironmentProfileSlot(pool, env, true, isActive); ok {
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

func findCodexEnvironmentProfileSlot(pool *CodexEnvironmentProfilePool, env EnvironmentClass, empty bool, isActive func(int) bool) (int, bool) {
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

func buildCodexEnvironmentProfileForClass(env EnvironmentClass) (*CodexEnvironmentProfile, error) {
	family := CodexClientFamilyCLI
	baseProfile, err := DefaultCodexCLIEnvironmentProfile(nil)
	if err != nil {
		return nil, err
	}
	ua := baseProfile.UserAgent
	originator := baseProfile.Originator
	version := baseProfile.Version
	tlsProfile := baseProfile.TLSProfile
	if normalizeEnvironmentClass(env) == EnvironmentClassDesktop {
		family = CodexClientFamilyDesktop
		originator = "codex_chatgpt_desktop"
		ua = "Codex Desktop/1.0"
		version = "1.0.0"
		tlsProfile = defaultCodexTLSProfileForFamily(family)
	}
	profile, err := newCodexEnvironmentProfile(family, "auto_default_pool", ua, originator, version, tlsProfile, nil)
	if err != nil {
		return nil, err
	}
	switch normalizeEnvironmentClass(env) {
	case EnvironmentClassWindows:
		profile.Platform = "windows"
		profile.Arch = "x64"
	case EnvironmentClassLinux:
		profile.Platform = "linux"
		profile.Arch = "x64"
	case EnvironmentClassMacOS:
		profile.Platform = "darwin"
		profile.Arch = "arm64"
	case EnvironmentClassDesktop:
		profile.Platform = "windows"
		profile.Arch = "x64"
	}
	return profile, profile.Validate()
}

func (s *OpenAIGatewayService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers)
}

func (s *AccountTestService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers)
}

func (s *AccountUsageService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers)
}

func acquireCodexEnvironmentProfileForRequestWithRepo(ctx context.Context, accountRepo AccountRepository, manager *EnvironmentProfileSlotLeaseManager, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if account == nil || !account.IsCodexSingleEnvironmentEnabled() {
		return nil, nil, nil
	}
	if accountRepo == nil && !accountHasCodexEnvironmentProfileSource(account) {
		return nil, nil, nil
	}
	if manager == nil {
		manager = NewEnvironmentProfileSlotLeaseManager()
	}
	unlock := manager.lockAccount(account.ID)
	defer unlock()
	if accountRepo != nil {
		if fresh, err := accountRepo.GetByID(ctx, account.ID); err == nil && fresh != nil {
			account = fresh
		} else if err != nil && err != ErrAccountNotFound {
			return nil, nil, err
		}
	}
	pool, err := getOrCreateCodexEnvironmentProfilePool(account)
	if err != nil {
		return nil, nil, err
	}
	env := DetectCodexEnvironmentClass(headers)
	lease, profile, err := acquireCodexEnvironmentProfileSlot(pool, manager, account, env, "", buildCodexEnvironmentProfileForClass)
	if err != nil {
		if err == ErrNoEnvironmentProfileSlot {
			return nil, nil, environmentProfileSlotExhaustedError()
		}
		return nil, nil, err
	}
	if lease != nil && lease.BoundNew && accountRepo != nil {
		if err := accountRepo.UpdateExtra(ctx, account.ID, map[string]any{codexEnvironmentProfilePoolKey: pool}); err != nil {
			lease.ReleaseFunc()
			return nil, nil, err
		}
	}
	return lease, profile, nil

}

func accountHasCodexEnvironmentProfileSource(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	if _, ok := account.GetCodexEnvironmentProfile(); ok {
		return true
	}
	pool, err := DecodeCodexEnvironmentProfilePool(account.Extra[codexEnvironmentProfilePoolKey])
	return err == nil && pool != nil
}

func environmentClassFromCodexProfile(profile *CodexEnvironmentProfile) EnvironmentClass {
	if profile == nil {
		return EnvironmentClassWindows
	}
	if profile.Family == CodexClientFamilyDesktop || strings.Contains(strings.ToLower(profile.Originator), "desktop") {
		return EnvironmentClassDesktop
	}
	headers := http.Header{"User-Agent": []string{profile.UserAgent}, "X-Stainless-OS": []string{profile.Platform}, "originator": []string{profile.Originator}}
	return detectEnvironmentClassFromHeaders(headers)
}
