package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/claude"
	"github.com/google/uuid"
)

const claudeEnvironmentProfilePoolKey = "claude_environment_profile_pool"

// claudeEnvironmentProfilePoolSchemaV2 是 3 OS 槽位冻结式 pool 的 schema 标记。
// v2: 固定 windows/macos/linux 三个槽位，每槽预生成冻结 profile（device_id/client_id/beta_set 终身不变）。
// 旧 pool（无 Schema 字段或 Schema != v2）视为 legacy，回退现有逻辑，不读写不覆盖。
const claudeEnvironmentProfilePoolSchemaV2 = "v2"

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
	Schema   string                         `json:"schema,omitempty"`
	Version  int                            `json:"version"`
	Capacity int                            `json:"capacity"`
	Slots    []ClaudeEnvironmentProfileSlot `json:"slots"`
}

// IsV2 报告 pool 是否为 schema v2（3 OS 槽位冻结）。
func (p *ClaudeEnvironmentProfilePool) IsV2() bool {
	return p != nil && p.Schema == claudeEnvironmentProfilePoolSchemaV2
}

func DecodeClaudeEnvironmentProfilePool(raw any) (*ClaudeEnvironmentProfilePool, error) {
	if raw == nil {
		return nil, nil
	}
	if pool, ok := raw.(*ClaudeEnvironmentProfilePool); ok {
		return pool, nil
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

// buildFrozenClaudeEnvironmentProfileForSlot 为指定 OS 槽位模拟生成一份冻结 profile。
// device_id/client_id 模拟生成并冻结；cli_version/beta_set 取传入版本的自洽集合。
// desktop 槽位不应出现（routeToSlot 已归并到 windows），此处仅处理 windows/macos/linux。
func buildFrozenClaudeEnvironmentProfileForSlot(env EnvironmentClass, cliVersion string) *ClaudeEnvironmentProfile {
	profile := buildClaudeEnvironmentProfileForClass(env)
	if cliVersion = strings.TrimSpace(cliVersion); cliVersion == "" {
		cliVersion = ExtractCLIVersion(profile.UserAgent)
	}
	profile.Source = claudeEnvironmentProfileSourceSimulated
	profile.ClientID = generateClientID()
	profile.DeviceID = generateClientID()
	profile.SessionSeed = uuid.NewString()
	profile.ClientVersion = cliVersion
	profile.UserAgent = "claude-cli/" + cliVersion + " (external, cli)"
	profile.XApp = "claude-code"
	profile.ClientType = "cli"
	profile.Family = ClaudeClientFamilyCodeCLI
	profile.Runtime = "node"
	if profile.RuntimeVersion == "" {
		profile.RuntimeVersion = defaultClaudeCodeRuntimeVersion()
	}
	profile.BetaSet = betaSetForCLIVersion(cliVersion)
	profile.Headers = map[string]string{}
	profile.TelemetryPolicy = claudeEnvironmentTelemetryPolicyLocalAck
	profile.TLSProfile = claudeTLSProfileForEnvironment(env, profile.Family)
	profile.TerminalType = claudeTerminalTypeForEnvironment(env, profile.Family)
	profile.FrozenAt = nowForEnvironmentProfilePool()
	profile.CreatedAt = profile.FrozenAt
	profile.UpdatedAt = profile.FrozenAt
	ensureClaudeTelemetryIdentity(profile)
	return profile
}

// newFrozenClaudeEnvironmentProfilePool 一次性模拟生成 schema v2 pool（windows/macos/linux 三个冻结槽位）。
func newFrozenClaudeEnvironmentProfilePool(cliVersion string) *ClaudeEnvironmentProfilePool {
	now := nowForEnvironmentProfilePool()
	slots := make([]ClaudeEnvironmentProfileSlot, len(fixedClaudeEnvironmentSlotClasses))
	for i, env := range fixedClaudeEnvironmentSlotClasses {
		profile := buildFrozenClaudeEnvironmentProfileForSlot(env, cliVersion)
		slots[i] = ClaudeEnvironmentProfileSlot{
			Slot:        i,
			Environment: env,
			State:       EnvironmentProfileSlotBound,
			Profile:     profile,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return &ClaudeEnvironmentProfilePool{
		Schema:   claudeEnvironmentProfilePoolSchemaV2,
		Version:  2,
		Capacity: len(slots),
		Slots:    slots,
	}
}

// upgradeLegacyClaudePoolToV2 将 legacy（auto_default_pool 等非 v2）pool 原地升级为 schema v2
// 三 OS 槽位冻结 pool，并按 OS 复用 legacy 中已有的设备身份（client_id/device_id/session_seed），
// 以维持上游指纹连续。legacy 中无对应 OS 身份的槽位保留模板新生成的身份。不修改入参 legacy。
func upgradeLegacyClaudePoolToV2(legacy *ClaudeEnvironmentProfilePool, cliVersion string) *ClaudeEnvironmentProfilePool {
	type preservedIdentity struct {
		clientID    string
		deviceID    string
		sessionSeed string
	}
	// 按 OS 收集 legacy 身份（routeToSlot 归一到 windows/macos/linux，首个命中为准）。
	identities := make(map[EnvironmentClass]preservedIdentity)
	if legacy != nil {
		for i := range legacy.Slots {
			profile := legacy.Slots[i].Profile
			if profile == nil {
				continue
			}
			clientID := strings.TrimSpace(profile.ClientID)
			deviceID := strings.TrimSpace(profile.DeviceID)
			sessionSeed := strings.TrimSpace(profile.SessionSeed)
			if clientID == "" || deviceID == "" || sessionSeed == "" {
				continue
			}
			os := routeToSlot(legacy.Slots[i].Environment)
			if _, exists := identities[os]; exists {
				continue
			}
			identities[os] = preservedIdentity{clientID: clientID, deviceID: deviceID, sessionSeed: sessionSeed}
		}
	}

	now := nowForEnvironmentProfilePool()
	slots := make([]ClaudeEnvironmentProfileSlot, len(fixedClaudeEnvironmentSlotClasses))
	for i, env := range fixedClaudeEnvironmentSlotClasses {
		profile := buildFrozenClaudeEnvironmentProfileForSlot(env, cliVersion)
		if id, ok := identities[env]; ok {
			profile.ClientID = id.clientID
			profile.DeviceID = id.deviceID
			profile.SessionSeed = id.sessionSeed
		}
		slots[i] = ClaudeEnvironmentProfileSlot{
			Slot:        i,
			Environment: env,
			State:       EnvironmentProfileSlotBound,
			Profile:     profile,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return &ClaudeEnvironmentProfilePool{
		Schema:   claudeEnvironmentProfilePoolSchemaV2,
		Version:  2,
		Capacity: len(slots),
		Slots:    slots,
	}
}

// betaSetForCLIVersion 返回指定 CLI 版本对应的自洽 anthropic-beta 集合。
// 当前对齐 FullClaudeCodeMimicryBetas；版本维度差异留待后续按版本细化。
func betaSetForCLIVersion(cliVersion string) []string {
	out := make([]string, len(claude.FullClaudeCodeMimicryBetas()))
	copy(out, claude.FullClaudeCodeMimicryBetas())
	return out
}

func defaultClaudeCodeRuntimeVersion() string {
	headers := claude.GetHeaders(nil)
	return strings.TrimPrefix(headers["X-Stainless-Runtime-Version"], "v")
}
func finalizeClaudeEnvironmentProfilePoolTelemetry(account *Account) bool {
	if account == nil || account.ID <= 0 || account.Extra == nil {
		return false
	}
	pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
	if err != nil || pool == nil || !pool.IsV2() {
		return false
	}
	changed := false
	for i := range pool.Slots {
		profile := pool.Slots[i].Profile
		if profile == nil {
			continue
		}
		scope := string(routeToSlot(pool.Slots[i].Environment))
		if scope == "" {
			scope = strconv.Itoa(pool.Slots[i].Slot)
		}
		wantUserID := stableClaudeTelemetryUserID(account.ID, scope)
		wantSessionID := stableClaudeTelemetrySessionID(account.ID, scope)
		if profile.TelemetryUserID != wantUserID || profile.TelemetrySessionID != wantSessionID || profile.StatsigStableID != wantUserID {
			profile.TelemetryUserID = wantUserID
			profile.TelemetrySessionID = wantSessionID
			profile.StatsigStableID = wantUserID
			changed = true
		}
		beforeTelemetryAttrs := profile.TelemetryAttributes
		beforeFeatureAttrs := profile.FeatureFlagAttributes
		applyClaudeTelemetryContext(profile, account)
		if !stringMapEqual(beforeTelemetryAttrs, profile.TelemetryAttributes) || !stringMapEqual(beforeFeatureAttrs, profile.FeatureFlagAttributes) {
			changed = true
		}
		if strings.TrimSpace(profile.TerminalType) == "" {
			profile.TerminalType = claudeTerminalTypeForEnvironment(pool.Slots[i].Environment, profile.Family)
			changed = true
		}
	}
	if changed {
		account.Extra[claudeEnvironmentProfilePoolKey] = pool
	}
	return changed
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

	// v2 路径：3 OS 槽位冻结式 pool。
	if pool, err := decodeClaudeEnvironmentProfilePool(account); err != nil {
		return nil, nil, err
	} else if pool != nil && pool.IsV2() {
		if finalizeClaudeEnvironmentProfilePoolTelemetry(account) {
			updatedPool, decodeErr := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
			if decodeErr != nil {
				return nil, nil, decodeErr
			}
			if updatedPool != nil {
				pool = updatedPool
			}
			if s.accountRepo != nil {
				if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{claudeEnvironmentProfilePoolKey: pool}); err != nil {
					return nil, nil, err
				}
			}
		}
		return s.acquireV2ClaudeEnvironmentProfileSlot(ctx, account, pool, headers, body)
	}

	// 旧账号不改动：存在 legacy pool 或旧 claude_environment_profile 时，回退现有逻辑。
	if accountHasLegacyClaudeEnvironmentProfile(account) {
		slog.Debug("claude_environment_profile_legacy_fallback",
			"account_id", account.ID,
			"reason", "legacy_schema_unmigrated")
		return s.acquireLegacyClaudeEnvironmentProfileSlot(ctx, account, headers, body)
	}

	// 未绑定 pool 的凭证：懒生成 v2 pool 并落库。
	cliVersion := s.claudeCLIVersion()
	pool := newFrozenClaudeEnvironmentProfilePool(cliVersion)
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[claudeEnvironmentProfilePoolKey] = pool
	finalizeClaudeEnvironmentProfilePoolTelemetry(account)
	pool, _ = DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
	if s.accountRepo != nil {
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{claudeEnvironmentProfilePoolKey: pool}); err != nil {
			return nil, nil, err
		}
	}
	slog.Info("claude_environment_profile_pool_generated",
		"account_id", account.ID,
		"schema", claudeEnvironmentProfilePoolSchemaV2,
		"cli_version", cliVersion)
	return s.acquireV2ClaudeEnvironmentProfileSlot(ctx, account, pool, headers, body)
}

// acquireV2ClaudeEnvironmentProfileSlot 在 schema v2 pool 上按客户端来源 OS 选槽。
// v2 槽位是共享身份而非互斥资源：并发请求复用同一槽位（如 5 个 windows 请求都走 windows 槽），
// 不占用 lease manager 的串行锁。lease.ReleaseFunc 为 no-op，仅保留 lease 结构以兼容下游
// attachEnvironmentProfileLeaseToRequest / wrapResponseBodyWithEnvironmentProfileLease。
func (s *GatewayService) acquireV2ClaudeEnvironmentProfileSlot(ctx context.Context, account *Account, pool *ClaudeEnvironmentProfilePool, headers http.Header, body []byte) (*EnvironmentProfileSlotLease, *ClaudeEnvironmentProfile, error) {
	env := routeToSlot(DetectClaudeEnvironmentClass(headers, body))
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
		return nil, nil, fmt.Errorf("v2 claude environment profile slot %d has no frozen profile", slotIdx)
	}
	lease := &EnvironmentProfileSlotLease{
		AccountID:   account.ID,
		Slot:        slotIdx,
		Environment: pool.Slots[slotIdx].Environment,
		ReleaseFunc: func() {}, // v2 无互斥，释放为 no-op
	}
	slog.Debug("claude_environment_profile_slot_applied",
		"account_id", account.ID,
		"slot", string(env),
		"device_id", profile.DeviceID,
		"cli_version", profile.ClientVersion)
	return lease, profile, nil
}

// acquireLegacyClaudeEnvironmentProfileSlot 是旧 schema 账号的回退路径，保持现有动态分桶行为。
func (s *GatewayService) acquireLegacyClaudeEnvironmentProfileSlot(ctx context.Context, account *Account, headers http.Header, body []byte) (*EnvironmentProfileSlotLease, *ClaudeEnvironmentProfile, error) {
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

// accountHasLegacyClaudeEnvironmentProfile 报告账号是否持有旧 schema pool 或旧 claude_environment_profile。
// 这类账号不改动，回退现有逻辑。
func accountHasLegacyClaudeEnvironmentProfile(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	if _, ok := account.GetClaudeEnvironmentProfile(); ok {
		return true
	}
	if pool, err := DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey]); err == nil && pool != nil && !pool.IsV2() {
		return true
	}
	return false
}

func decodeClaudeEnvironmentProfilePool(account *Account) (*ClaudeEnvironmentProfilePool, error) {
	if account == nil || account.Extra == nil {
		return nil, nil
	}
	return DecodeClaudeEnvironmentProfilePool(account.Extra[claudeEnvironmentProfilePoolKey])
}

// isV2ClaudeEnvironmentProfile 报告 profile 是否为 schema v2 槽位冻结式（模拟生成）。
// 用于决定是否强制重写 device_id（透传路径也强制）。
// 判据：source == simulated 且 FrozenAt 非零。
func isV2ClaudeEnvironmentProfile(profile *ClaudeEnvironmentProfile) bool {
	return profile != nil && profile.Source == claudeEnvironmentProfileSourceSimulated && !profile.FrozenAt.IsZero()
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
