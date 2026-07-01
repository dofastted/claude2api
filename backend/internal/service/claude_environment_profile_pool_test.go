package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestRouteToSlot(t *testing.T) {
	cases := []struct {
		in   EnvironmentClass
		want EnvironmentClass
	}{
		{EnvironmentClassWindows, EnvironmentClassWindows},
		{EnvironmentClassLinux, EnvironmentClassLinux},
		{EnvironmentClassMacOS, EnvironmentClassMacOS},
		{EnvironmentClassDesktop, EnvironmentClassWindows}, // desktop 归并 windows
		{"", EnvironmentClassWindows},                      // 未知默认 windows
	}
	for _, c := range cases {
		require.Equal(t, c.want, routeToSlot(c.in), "routeToSlot(%q)", c.in)
	}
}

func TestSlotIndexOfEnvironmentClass(t *testing.T) {
	require.Equal(t, 0, slotIndexOfEnvironmentClass(EnvironmentClassWindows))
	require.Equal(t, 0, slotIndexOfEnvironmentClass(EnvironmentClassDesktop)) // desktop → windows slot 0
	require.Equal(t, 1, slotIndexOfEnvironmentClass(EnvironmentClassMacOS))
	require.Equal(t, 2, slotIndexOfEnvironmentClass(EnvironmentClassLinux))
}

func TestNewFrozenClaudeEnvironmentProfilePool(t *testing.T) {
	pool := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	require.NotNil(t, pool)
	require.True(t, pool.IsV2())
	require.Equal(t, "v2", pool.Schema)
	require.Len(t, pool.Slots, 3)

	// 三槽位分别是 windows/macos/linux，顺序固定
	require.Equal(t, EnvironmentClassWindows, pool.Slots[0].Environment)
	require.Equal(t, EnvironmentClassMacOS, pool.Slots[1].Environment)
	require.Equal(t, EnvironmentClassLinux, pool.Slots[2].Environment)
	require.Equal(t, tlsfingerprint.ProfileNameClaudeCLIWindows, pool.Slots[0].Profile.TLSProfile)
	require.Equal(t, tlsfingerprint.ProfileNameClaudeCLIMacOS, pool.Slots[1].Profile.TLSProfile)
	require.Equal(t, tlsfingerprint.ProfileNameClaudeCLILinux, pool.Slots[2].Profile.TLSProfile)

	// 每槽 profile 冻结：device_id/client_id 非空、source=simulated、cli_version 一致
	for i, slot := range pool.Slots {
		require.NotNil(t, slot.Profile, "slot %d profile", i)
		require.NotEmpty(t, slot.Profile.DeviceID)
		require.NotEmpty(t, slot.Profile.ClientID)
		require.Equal(t, claudeEnvironmentProfileSourceSimulated, slot.Profile.Source)
		require.Equal(t, "2.1.161", slot.Profile.ClientVersion)
		require.NotZero(t, slot.Profile.FrozenAt)
		require.NotEmpty(t, slot.Profile.BetaSet)
		require.NotEmpty(t, slot.Profile.TelemetryUserID)
		require.NotEmpty(t, slot.Profile.TelemetrySessionID)
		require.Equal(t, slot.Profile.TelemetryUserID, slot.Profile.StatsigStableID)
		require.NotEmpty(t, slot.Profile.TerminalType)
	}

	// windows/linux = x64, macos = arm64
	require.Equal(t, "x64", pool.Slots[0].Profile.Arch)
	require.Equal(t, "arm64", pool.Slots[1].Profile.Arch)
	require.Equal(t, "x64", pool.Slots[2].Profile.Arch)

	// 每槽 device_id 互不相同
	devs := map[string]struct{}{}
	for _, slot := range pool.Slots {
		devs[slot.Profile.DeviceID] = struct{}{}
	}
	require.Len(t, devs, 3, "三个槽位 device_id 应互不相同")
}

func TestNewFrozenPoolDeviceIdStableAcrossCalls(t *testing.T) {
	// 同一 pool 内 device_id 冻结；不同 pool 实例 device_id 不同（模拟生成）
	p1 := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	p2 := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	require.NotEqual(t, p1.Slots[0].Profile.DeviceID, p2.Slots[0].Profile.DeviceID)
}

func TestIsV2ClaudeEnvironmentProfile(t *testing.T) {
	// v2 冻结 profile
	pool := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	require.True(t, isV2ClaudeEnvironmentProfile(pool.Slots[0].Profile))

	// legacy profile（无 FrozenAt / source 非 simulated）
	legacy := defaultClaudeCodeEnvironmentProfile(nil)
	require.False(t, isV2ClaudeEnvironmentProfile(legacy))

	// nil
	require.False(t, isV2ClaudeEnvironmentProfile(nil))
}

func TestAcquireV2SlotRoutesByClientOS(t *testing.T) {
	svc := &GatewayService{claudeEnvironmentProfileSlotLeases: NewEnvironmentProfileSlotLeaseManager()}
	pool := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	account := &Account{ID: 9001, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{claudeEnvironmentProfilePoolKey: pool}}

	// linux 客户端 → linux 槽（slot 2）
	hLinux := http.Header{}
	hLinux.Set("X-Stainless-OS", "Linux")
	lease, profile, err := svc.acquireV2ClaudeEnvironmentProfileSlot(context.TODO(), account, pool, hLinux, nil)
	require.NoError(t, err)
	require.Equal(t, EnvironmentClassLinux, lease.Environment)
	require.Equal(t, pool.Slots[2].Profile.DeviceID, profile.DeviceID)
	lease.ReleaseFunc()

	// windows 客户端 → windows 槽（slot 0）
	hWin := http.Header{}
	hWin.Set("X-Stainless-OS", "Windows")
	lease, profile, err = svc.acquireV2ClaudeEnvironmentProfileSlot(context.TODO(), account, pool, hWin, nil)
	require.NoError(t, err)
	require.Equal(t, EnvironmentClassWindows, lease.Environment)
	require.Equal(t, pool.Slots[0].Profile.DeviceID, profile.DeviceID)
	lease.ReleaseFunc()

	// desktop 客户端 → 归并 windows 槽（slot 0）
	hDesk := http.Header{}
	hDesk.Set("User-Agent", "Claude Desktop (electron)")
	lease, profile, err = svc.acquireV2ClaudeEnvironmentProfileSlot(context.TODO(), account, pool, hDesk, nil)
	require.NoError(t, err)
	require.Equal(t, EnvironmentClassWindows, lease.Environment)
	require.Equal(t, pool.Slots[0].Profile.DeviceID, profile.DeviceID)
	lease.ReleaseFunc()
}

func TestAccountHasLegacyClaudeEnvironmentProfile(t *testing.T) {
	// 旧 claude_environment_profile 字段 → legacy
	account := &Account{Extra: map[string]any{}}
	profile := defaultClaudeCodeEnvironmentProfile(nil)
	account.Extra[claudeEnvironmentProfileKey] = profile
	require.True(t, accountHasLegacyClaudeEnvironmentProfile(account))

	// v2 pool → 非 legacy
	account2 := &Account{Extra: map[string]any{}}
	account2.Extra[claudeEnvironmentProfilePoolKey] = newFrozenClaudeEnvironmentProfilePool("2.1.161")
	require.False(t, accountHasLegacyClaudeEnvironmentProfile(account2))

	// 空 → 非 legacy
	account3 := &Account{Extra: map[string]any{}}
	require.False(t, accountHasLegacyClaudeEnvironmentProfile(account3))
}

func TestAcquireV2SlotConcurrentReuseSameSlot(t *testing.T) {
	// v2 槽位是共享身份：并发请求复用同一槽位，不互斥。
	// 5 个 windows 请求都应成功拿到 windows 槽（slot 0），且 activeCount 保持 0（不占用 lease 锁）。
	svc := &GatewayService{claudeEnvironmentProfileSlotLeases: NewEnvironmentProfileSlotLeaseManager()}
	pool := newFrozenClaudeEnvironmentProfilePool("2.1.161")
	account := &Account{ID: 9102, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{claudeEnvironmentProfilePoolKey: pool}}

	hWin := http.Header{}
	hWin.Set("X-Stainless-OS", "Windows")

	type result struct {
		lease   *EnvironmentProfileSlotLease
		profile *ClaudeEnvironmentProfile
		err     error
	}
	n := 5
	results := make([]result, n)
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(start)
		for i := 0; i < n; i++ {
			lease, profile, err := svc.acquireV2ClaudeEnvironmentProfileSlot(context.TODO(), account, pool, hWin, nil)
			results[i] = result{lease, profile, err}
		}
		close(done)
	}()
	<-start
	<-done

	for i, r := range results {
		require.NoError(t, r.err, "concurrent windows request %d", i)
		require.NotNil(t, r.lease)
		require.Equal(t, EnvironmentClassWindows, r.lease.Environment)
		require.Equal(t, 0, r.lease.Slot)
		require.Equal(t, pool.Slots[0].Profile.DeviceID, r.profile.DeviceID)
	}
	// v2 不占用 lease manager 的串行锁
	require.Equal(t, 0, svc.claudeEnvironmentProfileSlotLeases.activeCount())
}

func TestUpgradeLegacyClaudePoolToV2PreservesIdentity(t *testing.T) {
	// 构造 legacy pool：3 windows + 4 linux bound 槽位，各带模拟身份；3 个空槽。
	winID := claudeProfilePoolTestIdentity("win")
	linID := claudeProfilePoolTestIdentity("linux")
	legacy := &ClaudeEnvironmentProfilePool{
		Version:  1,
		Capacity: 10,
		Slots: []ClaudeEnvironmentProfileSlot{
			{Slot: 0, Environment: EnvironmentClassWindows, State: EnvironmentProfileSlotBound, Profile: winID},
			{Slot: 1, Environment: EnvironmentClassWindows, State: EnvironmentProfileSlotBound, Profile: claudeProfilePoolTestIdentity("win2")},
			{Slot: 2, Environment: EnvironmentClassWindows, State: EnvironmentProfileSlotBound, Profile: claudeProfilePoolTestIdentity("win3")},
			{Slot: 3, Environment: EnvironmentClassLinux, State: EnvironmentProfileSlotBound, Profile: linID},
			{Slot: 4, Environment: EnvironmentClassLinux, State: EnvironmentProfileSlotBound, Profile: claudeProfilePoolTestIdentity("linux2")},
			{Slot: 7, Environment: EnvironmentClassWindows, State: EnvironmentProfileSlotEmpty},
		},
	}

	upgraded := upgradeLegacyClaudePoolToV2(legacy, "2.1.161")
	require.NotNil(t, upgraded)
	require.NoError(t, upgraded.Normalize())

	// 结构对齐 v2：schema/version/capacity/3 槽
	require.True(t, upgraded.IsV2())
	require.Equal(t, "v2", upgraded.Schema)
	require.Equal(t, 2, upgraded.Version)
	require.Equal(t, 3, upgraded.Capacity)
	require.Len(t, upgraded.Slots, 3)
	require.Equal(t, EnvironmentClassWindows, upgraded.Slots[0].Environment)
	require.Equal(t, EnvironmentClassMacOS, upgraded.Slots[1].Environment)
	require.Equal(t, EnvironmentClassLinux, upgraded.Slots[2].Environment)

	// 每槽均为 v2 冻结，且继承 cache 模板字段；TLS 指纹按 OS 槽位分散。
	wantTLS := []string{
		tlsfingerprint.ProfileNameClaudeCLIWindows,
		tlsfingerprint.ProfileNameClaudeCLIMacOS,
		tlsfingerprint.ProfileNameClaudeCLILinux,
	}
	for i, slot := range upgraded.Slots {
		require.NotNil(t, slot.Profile, "slot %d", i)
		require.True(t, isV2ClaudeEnvironmentProfile(slot.Profile), "slot %d isV2", i)
		require.Equal(t, wantTLS[i], slot.Profile.TLSProfile, "slot %d tls", i)
		require.Equal(t, claudeEnvironmentCachePolicyPreserveClient, slot.Profile.CachePolicy, "slot %d cache", i)
	}

	// windows 槽复用首个 windows 身份；linux 槽复用首个 linux 身份（指纹连续）
	require.Equal(t, winID.ClientID, upgraded.Slots[0].Profile.ClientID)
	require.Equal(t, winID.DeviceID, upgraded.Slots[0].Profile.DeviceID)
	require.Equal(t, winID.SessionSeed, upgraded.Slots[0].Profile.SessionSeed)
	require.Equal(t, linID.ClientID, upgraded.Slots[2].Profile.ClientID)
	require.Equal(t, linID.DeviceID, upgraded.Slots[2].Profile.DeviceID)
	require.Equal(t, linID.SessionSeed, upgraded.Slots[2].Profile.SessionSeed)

	// macos 无 legacy 身份 → 新生成，非空且不等于其它槽
	require.NotEmpty(t, upgraded.Slots[1].Profile.ClientID)
	require.NotEmpty(t, upgraded.Slots[1].Profile.DeviceID)
	require.NotEqual(t, winID.DeviceID, upgraded.Slots[1].Profile.DeviceID)
	require.NotEqual(t, linID.DeviceID, upgraded.Slots[1].Profile.DeviceID)

	// 不修改入参 legacy
	require.Equal(t, winID.DeviceID, legacy.Slots[0].Profile.DeviceID)
	require.False(t, legacy.IsV2())
}

func TestUpgradeLegacyClaudePoolToV2NoIdentitiesAllFresh(t *testing.T) {
	// legacy 全空槽 → 升级后三槽均为模板新身份，仍是合法 v2 pool。
	legacy := &ClaudeEnvironmentProfilePool{
		Version:  1,
		Capacity: 3,
		Slots: []ClaudeEnvironmentProfileSlot{
			{Slot: 0, Environment: EnvironmentClassWindows, State: EnvironmentProfileSlotEmpty},
			{Slot: 1, Environment: EnvironmentClassLinux, State: EnvironmentProfileSlotEmpty},
		},
	}
	upgraded := upgradeLegacyClaudePoolToV2(legacy, "2.1.161")
	require.NoError(t, upgraded.Normalize())
	require.True(t, upgraded.IsV2())
	require.Len(t, upgraded.Slots, 3)
	for i, slot := range upgraded.Slots {
		require.True(t, isV2ClaudeEnvironmentProfile(slot.Profile), "slot %d", i)
		require.NotEmpty(t, slot.Profile.DeviceID)
	}
}

// claudeProfilePoolTestIdentity 生成带固定 client_id/device_id/session_seed 的最小 legacy profile。
func claudeProfilePoolTestIdentity(tag string) *ClaudeEnvironmentProfile {
	return &ClaudeEnvironmentProfile{
		Source:      claudeEnvironmentProfileSourceAutoDefault,
		ClientID:    "client-" + tag,
		DeviceID:    "device-" + tag,
		SessionSeed: "seed-" + tag,
	}
}
