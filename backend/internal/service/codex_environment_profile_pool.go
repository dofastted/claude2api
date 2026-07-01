package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
)

const codexEnvironmentProfilePoolKey = "codex_environment_profile_pool"

// codexEnvironmentProfilePoolSchemaV2 是 3 OS 槽位冻结式 pool 的 schema 标记（与 Claude v2 对齐）。
const codexEnvironmentProfilePoolSchemaV2 = "v2"

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
	Schema   string                        `json:"schema,omitempty"`
	Version  int                           `json:"version"`
	Capacity int                           `json:"capacity"`
	Slots    []CodexEnvironmentProfileSlot `json:"slots"`
}

// IsV2 报告 pool 是否为 schema v2（3 OS 槽位冻结）。
func (p *CodexEnvironmentProfilePool) IsV2() bool {
	return p != nil && p.Schema == codexEnvironmentProfilePoolSchemaV2
}

func DecodeCodexEnvironmentProfilePool(raw any) (*CodexEnvironmentProfilePool, error) {
	if raw == nil {
		return nil, nil
	}
	if pool, ok := raw.(*CodexEnvironmentProfilePool); ok {
		return pool, nil
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
	return buildCodexEnvironmentProfileForClassWithRegistry(env, nil)
}

func buildCodexEnvironmentProfileForClassWithRegistry(env EnvironmentClass, registry *clientidentity.Registry) (*CodexEnvironmentProfile, error) {
	family := CodexClientFamilyCLI
	baseProfile, err := DefaultCodexCLIEnvironmentProfile(registry)
	if err != nil {
		return nil, err
	}
	ua := baseProfile.UserAgent
	originator := baseProfile.Originator
	version := baseProfile.Version
	if normalizeEnvironmentClass(env) == EnvironmentClassDesktop {
		family = CodexClientFamilyDesktop
		originator = "codex_chatgpt_desktop"
		ua = "Codex Desktop/1.0"
		version = "1.0.0"
	}
	tlsProfile := codexTLSProfileForEnvironment(env, family)
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

// buildFrozenCodexEnvironmentProfileForSlot 为指定 OS 槽位模拟生成一份冻结 Codex profile。
// session_seed/conversation_seed 模拟生成并冻结；originator/version/tls_profile/platform/arch 按 OS 归一。
func buildFrozenCodexEnvironmentProfileForSlot(env EnvironmentClass, registry *clientidentity.Registry) (*CodexEnvironmentProfile, error) {
	profile, err := buildCodexEnvironmentProfileForClassWithRegistry(env, registry)
	if err != nil {
		return nil, err
	}
	profile.Source = codexEnvironmentProfileSourceSimulated
	profile.FrozenAt = nowForEnvironmentProfilePool()
	profile.CreatedAt = profile.FrozenAt
	profile.UpdatedAt = profile.FrozenAt
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return profile, nil
}

// newFrozenCodexEnvironmentProfilePool 一次性模拟生成 schema v2 pool（windows/macos/linux 三个冻结槽位）。
func newFrozenCodexEnvironmentProfilePool(registries ...*clientidentity.Registry) *CodexEnvironmentProfilePool {
	now := nowForEnvironmentProfilePool()
	slots := make([]CodexEnvironmentProfileSlot, len(fixedClaudeEnvironmentSlotClasses))
	for i, env := range fixedClaudeEnvironmentSlotClasses {
		profile, err := buildFrozenCodexEnvironmentProfileForSlot(env, firstCodexRegistry(registries))
		if err != nil {
			// 不应发生：buildCodexEnvironmentProfileForClass 已 Validate 过
			profile = mustBuildFallbackFrozenCodexProfile(env, registries...)
		}
		slots[i] = CodexEnvironmentProfileSlot{
			Slot:        i,
			Environment: env,
			State:       EnvironmentProfileSlotBound,
			Profile:     profile,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return &CodexEnvironmentProfilePool{
		Schema:   codexEnvironmentProfilePoolSchemaV2,
		Version:  2,
		Capacity: len(slots),
		Slots:    slots,
	}
}

func mustBuildFallbackFrozenCodexProfile(env EnvironmentClass, registries ...*clientidentity.Registry) *CodexEnvironmentProfile {
	registry := firstCodexRegistry(registries)
	profile, err := buildCodexEnvironmentProfileForClassWithRegistry(env, registry)
	if err != nil {
		profile, _ = buildCodexEnvironmentProfileForClassWithRegistry(EnvironmentClassWindows, registry)
	}
	profile.Source = codexEnvironmentProfileSourceSimulated
	profile.FrozenAt = nowForEnvironmentProfilePool()
	return profile
}

func firstCodexRegistry(registries []*clientidentity.Registry) *clientidentity.Registry {
	if len(registries) == 0 {
		return nil
	}
	return registries[0]
}

func (s *OpenAIGatewayService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers, s.identityRegistry)
}

func (s *AccountTestService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers, s.identityRegistry)
}

func (s *AccountUsageService) acquireCodexEnvironmentProfileForRequest(ctx context.Context, account *Account, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	if s == nil {
		return nil, nil, nil
	}
	if s.codexEnvironmentProfileSlotLeases == nil {
		s.codexEnvironmentProfileSlotLeases = NewEnvironmentProfileSlotLeaseManager()
	}
	return acquireCodexEnvironmentProfileForRequestWithRepo(ctx, s.accountRepo, s.codexEnvironmentProfileSlotLeases, account, headers, s.identityRegistry)
}

func acquireCodexEnvironmentProfileForRequestWithRepo(ctx context.Context, accountRepo AccountRepository, manager *EnvironmentProfileSlotLeaseManager, account *Account, headers http.Header, registry *clientidentity.Registry) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
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

	// v2 路径：已绑定 schema v2 pool。
	if pool, err := DecodeCodexEnvironmentProfilePool(account.Extra[codexEnvironmentProfilePoolKey]); err != nil {
		return nil, nil, err
	} else if pool != nil && pool.IsV2() {
		return acquireV2CodexEnvironmentProfileSlot(account, pool, headers)
	}

	// Codex 旧账号统一升级迁移：生成 v2 pool 并落库，删除旧 pool / 旧 codex_environment_profile。
	if account.Extra != nil {
		if _, exists := account.Extra[codexEnvironmentProfilePoolKey]; exists {
			if deleter, ok := accountRepo.(accountExtraKeyDeleter); ok {
				_ = deleter.DeleteExtraKeys(ctx, account.ID, []string{codexEnvironmentProfileKey})
			}
		}
	}
	pool := newFrozenCodexEnvironmentProfilePool(registry)
	if accountRepo != nil {
		updates := map[string]any{codexEnvironmentProfilePoolKey: pool}
		if err := accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
			return nil, nil, err
		}
	}
	slog.Info("codex_environment_profile_pool_generated",
		"account_id", account.ID,
		"schema", codexEnvironmentProfilePoolSchemaV2,
		"reason", "unified_migration")
	return acquireV2CodexEnvironmentProfileSlot(account, pool, headers)
}

// acquireV2CodexEnvironmentProfileSlot 在 schema v2 pool 上按客户端来源 OS 选槽。
// v2 槽位是共享身份而非互斥资源：并发请求复用同一槽位，不占用 lease manager 串行锁。
func acquireV2CodexEnvironmentProfileSlot(account *Account, pool *CodexEnvironmentProfilePool, headers http.Header) (*EnvironmentProfileSlotLease, *CodexEnvironmentProfile, error) {
	env := routeToSlot(DetectCodexEnvironmentClass(headers))
	slotIdx := slotIndexOfEnvironmentClass(env)
	if slotIdx < 0 || slotIdx >= len(pool.Slots) {
		return nil, nil, environmentProfileSlotExhaustedError()
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if err := pool.Normalize(); err != nil {
		return nil, nil, err
	}
	profile := pool.Slots[slotIdx].Profile
	if profile == nil {
		return nil, nil, fmt.Errorf("v2 codex environment profile slot %d has no frozen profile", slotIdx)
	}
	lease := &EnvironmentProfileSlotLease{
		AccountID:   account.ID,
		Slot:        slotIdx,
		Environment: pool.Slots[slotIdx].Environment,
		ReleaseFunc: func() {}, // v2 无互斥，释放为 no-op
	}
	slog.Debug("codex_environment_profile_slot_applied",
		"account_id", account.ID,
		"slot", string(env),
		"platform", profile.Platform,
		"version", profile.Version)
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
