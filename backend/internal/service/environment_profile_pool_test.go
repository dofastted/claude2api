package service

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetectEnvironmentClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		headers http.Header
		want    EnvironmentClass
	}{
		{name: "windows", headers: http.Header{"User-Agent": []string{"Mozilla/5.0 Windows NT 10.0"}}, want: EnvironmentClassWindows},
		{name: "linux", headers: http.Header{"X-Stainless-OS": []string{"linux"}}, want: EnvironmentClassLinux},
		{name: "macos", headers: http.Header{"User-Agent": []string{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)"}}, want: EnvironmentClassMacOS},
		{name: "claude desktop", headers: http.Header{"User-Agent": []string{"Claude Desktop/1.0 Electron"}}, want: EnvironmentClassDesktop},
		{name: "codex desktop", headers: http.Header{"Originator": []string{"codex_chatgpt_desktop"}}, want: EnvironmentClassDesktop},
		{name: "codex tui linux", headers: http.Header{"User-Agent": []string{"codex-tui/0.142.0 (Ubuntu 22.4.0; x86_64) xterm (codex-tui; 0.142.0)"}}, want: EnvironmentClassLinux},
		{name: "unknown defaults windows", headers: http.Header{"User-Agent": []string{"unknown-client"}}, want: EnvironmentClassWindows},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.name != "codex desktop" {
				require.Equal(t, tc.want, DetectClaudeEnvironmentClass(tc.headers, nil))
			}
			if tc.name != "claude desktop" {
				require.Equal(t, tc.want, DetectCodexEnvironmentClass(tc.headers))
			}
		})
	}
}

func TestEnvironmentProfileCapacityUsesTierAndManualOverride(t *testing.T) {
	manual := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 5, Extra: map[string]any{environmentProfileManualCapacityKey: 12}}
	require.Equal(t, 12, environmentProfileCapacity(manual))

	claudeMax20 := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 5, Credentials: map[string]any{"plan_type": "Max 20"}, Extra: map[string]any{}}
	require.Equal(t, 20, environmentProfileCapacity(claudeMax20))

	codexPro5 := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3, Credentials: map[string]any{"plan_type": "pro_5"}, Extra: map[string]any{}}
	require.Equal(t, 10, environmentProfileCapacity(codexPro5))

	codexPlus := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3, Credentials: map[string]any{"plan_type": "plus"}, Extra: map[string]any{}}
	require.Equal(t, 5, environmentProfileCapacity(codexPlus))
}

func TestClaudeEnvironmentProfilePoolBindsFiveWindowsSlots(t *testing.T) {
	account := &Account{ID: 701, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 5, Extra: map[string]any{}}
	pool, err := getOrCreateClaudeEnvironmentProfilePool(account)
	require.NoError(t, err)
	require.Equal(t, 5, pool.Capacity)
	manager := NewEnvironmentProfileSlotLeaseManager()
	leases := make([]*EnvironmentProfileSlotLease, 0, account.Concurrency)
	for i := 0; i < account.Concurrency; i++ {
		lease, profile, err := acquireClaudeEnvironmentProfileSlot(pool, manager, account, EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
			profile := buildClaudeEnvironmentProfileForClass(env)
			return profile, ValidateClaudeEnvironmentProfile(profile)
		})
		require.NoError(t, err)
		require.NotNil(t, profile)
		require.Equal(t, EnvironmentClassWindows, lease.Environment)
		leases = append(leases, lease)
	}
	_, _, err = acquireClaudeEnvironmentProfileSlot(pool, manager, account, EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.ErrorIs(t, err, ErrNoEnvironmentProfileSlot)
	for _, lease := range leases {
		lease.ReleaseFunc()
	}
	require.Equal(t, 0, manager.activeCount())
	for _, slot := range pool.Slots {
		if slot.Slot >= pool.Capacity {
			continue
		}
		require.Equal(t, EnvironmentProfileSlotBound, slot.State)
		require.Equal(t, EnvironmentClassWindows, slot.Environment)
		require.NotNil(t, slot.Profile)
	}
}

func TestClaudeEnvironmentProfilePoolDoesNotOverwriteBoundEnvironment(t *testing.T) {
	account := &Account{ID: 702, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 2, Extra: map[string]any{}}
	pool := newClaudeEnvironmentProfilePool(2)
	manager := NewEnvironmentProfileSlotLeaseManager()
	lease, _, err := acquireClaudeEnvironmentProfileSlot(pool, manager, account, EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.NoError(t, err)
	lease.ReleaseFunc()
	lease, _, err = acquireClaudeEnvironmentProfileSlot(pool, manager, account, EnvironmentClassLinux, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.NoError(t, err)
	lease.ReleaseFunc()

	bound := map[EnvironmentClass]int{}
	for _, slot := range pool.Slots {
		bound[slot.Environment]++
	}
	require.Equal(t, 1, bound[EnvironmentClassWindows])
	require.Equal(t, 1, bound[EnvironmentClassLinux])
}

func TestCodexEnvironmentProfilePoolBindsMixedSlots(t *testing.T) {
	account := &Account{ID: 703, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4, Extra: map[string]any{}}
	pool, err := getOrCreateCodexEnvironmentProfilePool(account)
	require.NoError(t, err)
	manager := NewEnvironmentProfileSlotLeaseManager()
	for _, env := range []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop} {
		lease, profile, err := acquireCodexEnvironmentProfileSlot(pool, manager, account, env, "", buildCodexEnvironmentProfileForClass)
		require.NoError(t, err)
		require.NotNil(t, profile)
		require.Equal(t, env, lease.Environment)
		lease.ReleaseFunc()
	}
	seen := map[EnvironmentClass]bool{}
	for _, slot := range pool.Slots {
		seen[slot.Environment] = true
		require.Equal(t, EnvironmentProfileSlotBound, slot.State)
	}
	require.True(t, seen[EnvironmentClassWindows])
	require.True(t, seen[EnvironmentClassLinux])
	require.True(t, seen[EnvironmentClassMacOS])
	require.True(t, seen[EnvironmentClassDesktop])
}

func TestEnvironmentProfilePoolHundredConcurrentTwentyCredentials(t *testing.T) {
	runEnvironmentProfilePoolConcurrentScenario(t, 20, 5, 100)
}

func TestEnvironmentProfilePoolThousandConcurrentTwoHundredCredentials(t *testing.T) {
	runEnvironmentProfilePoolConcurrentScenario(t, 200, 5, 1000)
}

func TestEnvironmentProfilePoolSameEnvironmentFailsOverAcrossCredentials(t *testing.T) {
	accounts := []*Account{
		{ID: 920, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1, Extra: map[string]any{}},
		{ID: 921, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1, Extra: map[string]any{}},
	}
	pools := []*ClaudeEnvironmentProfilePool{newClaudeEnvironmentProfilePool(1), newClaudeEnvironmentProfilePool(1)}
	managers := []*EnvironmentProfileSlotLeaseManager{NewEnvironmentProfileSlotLeaseManager(), NewEnvironmentProfileSlotLeaseManager()}

	firstLease, _, err := acquireClaudeEnvironmentProfileSlot(pools[0], managers[0], accounts[0], EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.NoError(t, err)
	defer firstLease.ReleaseFunc()

	lease, _, err := acquireClaudeEnvironmentProfileSlot(pools[0], managers[0], accounts[0], EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.ErrorIs(t, err, ErrNoEnvironmentProfileSlot)
	require.Nil(t, lease)

	failoverLease, profile, err := acquireClaudeEnvironmentProfileSlot(pools[1], managers[1], accounts[1], EnvironmentClassWindows, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
		profile := buildClaudeEnvironmentProfileForClass(env)
		return profile, ValidateClaudeEnvironmentProfile(profile)
	})
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, EnvironmentClassWindows, failoverLease.Environment)
	failoverLease.ReleaseFunc()
}

func runEnvironmentProfilePoolConcurrentScenario(t *testing.T, credentialCount, concurrencyPerCredential, requestCount int) {
	t.Helper()
	accounts := make([]*Account, credentialCount)
	pools := make([]*ClaudeEnvironmentProfilePool, credentialCount)
	managers := make([]*EnvironmentProfileSlotLeaseManager, credentialCount)
	for i := 0; i < credentialCount; i++ {
		accounts[i] = &Account{ID: int64(800 + i), Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: concurrencyPerCredential, Extra: map[string]any{}}
		pools[i] = newClaudeEnvironmentProfilePool(concurrencyPerCredential)
		managers[i] = NewEnvironmentProfileSlotLeaseManager()
	}
	environments := []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, requestCount)
	leases := make(chan *EnvironmentProfileSlotLease, requestCount)
	for i := 0; i < requestCount; i++ {
		index := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			credential := index % credentialCount
			env := environments[index%len(environments)]
			lease, profile, err := acquireClaudeEnvironmentProfileSlot(pools[credential], managers[credential], accounts[credential], env, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
				profile := buildClaudeEnvironmentProfileForClass(env)
				return profile, ValidateClaudeEnvironmentProfile(profile)
			})
			if err != nil {
				errs <- err
				return
			}
			if profile == nil || lease.Environment != env {
				errs <- ErrNoEnvironmentProfileSlot
				lease.ReleaseFunc()
				return
			}
			leases <- lease
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, leases, requestCount)
	close(leases)
	for lease := range leases {
		lease.ReleaseFunc()
	}
	for i := 0; i < credentialCount; i++ {
		require.Equal(t, 0, managers[i].activeCount())
		bound := 0
		for _, slot := range pools[i].Slots {
			if slot.Slot < pools[i].Capacity && slot.State == EnvironmentProfileSlotBound {
				bound++
			}
		}
		require.Equal(t, concurrencyPerCredential, bound)
	}
}

func TestEnvironmentProfilePoolSequentialTenThousandRequests(t *testing.T) {
	const requestCount = 10000
	account := &Account{ID: 901, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 5, Extra: map[string]any{}}
	pool := newClaudeEnvironmentProfilePool(account.Concurrency)
	manager := NewEnvironmentProfileSlotLeaseManager()
	environments := []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop, EnvironmentClassWindows}
	for i := 0; i < requestCount; i++ {
		env := environments[i%len(environments)]
		lease, profile, err := acquireClaudeEnvironmentProfileSlot(pool, manager, account, env, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
			profile := buildClaudeEnvironmentProfileForClass(env)
			return profile, ValidateClaudeEnvironmentProfile(profile)
		})
		require.NoError(t, err)
		require.NotNil(t, profile)
		require.Equal(t, env, lease.Environment)
		lease.ReleaseFunc()
	}
	require.Equal(t, 0, manager.activeCount())
	bound := map[EnvironmentClass]int{}
	for _, slot := range pool.Slots {
		if slot.State == EnvironmentProfileSlotBound {
			bound[slot.Environment]++
		}
	}
	require.Equal(t, 1, bound[EnvironmentClassWindows])
	require.Equal(t, 1, bound[EnvironmentClassLinux])
	require.Equal(t, 1, bound[EnvironmentClassMacOS])
	require.Equal(t, 1, bound[EnvironmentClassDesktop])
}

func TestEnvironmentProfilePoolHundredConcurrentRequestsHandlesErrors(t *testing.T) {
	const requestCount = 100
	credentials := newClaudeProfilePoolTestCredentials(24, 5)
	credentials.accounts[0].Status = StatusError
	credentials.accounts[0].ErrorMessage = "invalid credential"
	cooldownUntil := time.Now().Add(time.Hour)
	credentials.accounts[1].RateLimitResetAt = &cooldownUntil
	credentials.accounts[2].Type = AccountTypeAPIKey
	credentials.accounts[2].Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0}

	start := make(chan struct{})
	results := make(chan profilePoolDispatchResult, requestCount)
	errs := make(chan error, requestCount)
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		index := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			failedOnce := false
			result, err := dispatchClaudeProfilePoolRequest(credentials, EnvironmentClassWindows, index, func(*Account, int) bool {
				if failedOnce || index%17 != 0 {
					return false
				}
				failedOnce = true
				return true
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, results, requestCount)
	require.Equal(t, requestCount, credentials.activeCount())

	seenFailover := false
	close(results)
	for result := range results {
		require.NotContains(t, []int64{credentials.accounts[0].ID, credentials.accounts[1].ID, credentials.accounts[2].ID}, result.account.ID)
		if result.failovers > 0 {
			seenFailover = true
		}
		result.lease.ReleaseFunc()
	}
	require.True(t, seenFailover)
	require.Equal(t, 0, credentials.activeCount())
}

func TestEnvironmentProfilePoolContinuousTenMinuteRequestsHandlesErrors(t *testing.T) {
	const virtualDuration = 10 * time.Minute
	const virtualInterval = 50 * time.Millisecond
	requestCount := int(virtualDuration / virtualInterval)
	credentials := newClaudeProfilePoolTestCredentials(8, 3)
	credentials.accounts[0].Status = StatusError
	credentials.accounts[0].ErrorMessage = "expired access token"
	cooldownUntil := time.Now().Add(time.Hour)
	credentials.accounts[1].OverloadUntil = &cooldownUntil
	credentials.accounts[2].Type = AccountTypeAPIKey
	credentials.accounts[2].Extra = map[string]any{"quota_daily_limit": 1.0, "quota_daily_used": 1.0, "quota_daily_start": time.Now().Add(-time.Hour).Format(time.RFC3339)}

	environments := []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop}
	failovers := 0
	for i := 0; i < requestCount; i++ {
		env := environments[i%len(environments)]
		failedOnce := false
		result, err := dispatchClaudeProfilePoolRequest(credentials, env, i, func(*Account, int) bool {
			if failedOnce || i%101 != 0 {
				return false
			}
			failedOnce = true
			return true
		})
		require.NoError(t, err)
		require.Equal(t, env, result.lease.Environment)
		require.NotContains(t, []int64{credentials.accounts[0].ID, credentials.accounts[1].ID, credentials.accounts[2].ID}, result.account.ID)
		failovers += result.failovers
		result.lease.ReleaseFunc()
		require.Equal(t, 0, credentials.activeCount())
	}
	require.Greater(t, failovers, 0)

	bound := map[EnvironmentClass]int{}
	for _, pool := range credentials.pools {
		for _, slot := range pool.Slots {
			if slot.State == EnvironmentProfileSlotBound {
				bound[slot.Environment]++
			}
		}
	}
	require.GreaterOrEqual(t, bound[EnvironmentClassWindows], 1)
	require.GreaterOrEqual(t, bound[EnvironmentClassLinux], 1)
	require.GreaterOrEqual(t, bound[EnvironmentClassMacOS], 1)
	require.GreaterOrEqual(t, bound[EnvironmentClassDesktop], 1)
}

func TestEnvironmentProfilePoolRealTenMinuteRequestsHandlesErrors(t *testing.T) {
	if os.Getenv("SUB2API_REAL_TEN_MINUTE_PROFILE_POOL_TEST") != "1" {
		t.Skip("set SUB2API_REAL_TEN_MINUTE_PROFILE_POOL_TEST=1 to run the real 10-minute profile pool test")
	}

	const duration = 10 * time.Minute
	const interval = 50 * time.Millisecond
	credentials := newClaudeProfilePoolTestCredentials(8, 3)
	credentials.accounts[0].Status = StatusError
	credentials.accounts[0].ErrorMessage = "expired access token"
	cooldownUntil := time.Now().Add(time.Hour)
	credentials.accounts[1].RateLimitResetAt = &cooldownUntil
	credentials.accounts[2].Type = AccountTypeAPIKey
	credentials.accounts[2].Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0}

	environments := []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop}
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	requests := 0
	failovers := 0
	for now := range ticker.C {
		if !now.Before(deadline) {
			break
		}
		env := environments[requests%len(environments)]
		failedOnce := false
		result, err := dispatchClaudeProfilePoolRequest(credentials, env, requests, func(*Account, int) bool {
			if failedOnce || requests%101 != 0 {
				return false
			}
			failedOnce = true
			return true
		})
		require.NoError(t, err)
		require.Equal(t, env, result.lease.Environment)
		require.NotContains(t, []int64{credentials.accounts[0].ID, credentials.accounts[1].ID, credentials.accounts[2].ID}, result.account.ID)
		failovers += result.failovers
		result.lease.ReleaseFunc()
		require.Equal(t, 0, credentials.activeCount())
		requests++
	}

	require.GreaterOrEqual(t, requests, int(duration/interval)-1)
	require.Greater(t, failovers, 0)
	require.Equal(t, 0, credentials.activeCount())
}

type claudeProfilePoolTestCredentials struct {
	accounts []*Account
	pools    []*ClaudeEnvironmentProfilePool
	managers []*EnvironmentProfileSlotLeaseManager
}

type profilePoolDispatchResult struct {
	lease     *EnvironmentProfileSlotLease
	account   *Account
	failovers int
}

func newClaudeProfilePoolTestCredentials(credentialCount, concurrencyPerCredential int) *claudeProfilePoolTestCredentials {
	credentials := &claudeProfilePoolTestCredentials{
		accounts: make([]*Account, credentialCount),
		pools:    make([]*ClaudeEnvironmentProfilePool, credentialCount),
		managers: make([]*EnvironmentProfileSlotLeaseManager, credentialCount),
	}
	for i := 0; i < credentialCount; i++ {
		credentials.accounts[i] = &Account{
			ID:          int64(10000 + i),
			Platform:    PlatformAnthropic,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: concurrencyPerCredential,
			Extra:       map[string]any{},
		}
		credentials.pools[i] = newClaudeEnvironmentProfilePool(concurrencyPerCredential)
		credentials.managers[i] = NewEnvironmentProfileSlotLeaseManager()
	}
	return credentials
}

func (c *claudeProfilePoolTestCredentials) activeCount() int {
	active := 0
	for _, manager := range c.managers {
		active += manager.activeCount()
	}
	return active
}

func dispatchClaudeProfilePoolRequest(credentials *claudeProfilePoolTestCredentials, env EnvironmentClass, startIndex int, shouldFail func(*Account, int) bool) (profilePoolDispatchResult, error) {
	if credentials == nil || len(credentials.accounts) == 0 {
		return profilePoolDispatchResult{}, ErrNoEnvironmentProfileSlot
	}
	excluded := make(map[int64]struct{})
	failovers := 0
	for attempt := 0; attempt < len(credentials.accounts)*2; attempt++ {
		index := (startIndex + attempt) % len(credentials.accounts)
		account := credentials.accounts[index]
		if _, ok := excluded[account.ID]; ok {
			continue
		}
		if !account.IsSchedulable() {
			excluded[account.ID] = struct{}{}
			failovers++
			continue
		}
		lease, profile, err := acquireClaudeEnvironmentProfileSlot(credentials.pools[index], credentials.managers[index], account, env, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
			profile := buildClaudeEnvironmentProfileForClass(env)
			return profile, ValidateClaudeEnvironmentProfile(profile)
		})
		if err != nil {
			if err == ErrNoEnvironmentProfileSlot {
				excluded[account.ID] = struct{}{}
				failovers++
				continue
			}
			return profilePoolDispatchResult{}, err
		}
		if profile == nil || lease.Environment != env {
			lease.ReleaseFunc()
			return profilePoolDispatchResult{}, fmt.Errorf("profile slot environment mismatch")
		}
		if shouldFail != nil && shouldFail(account, attempt) {
			lease.ReleaseFunc()
			excluded[account.ID] = struct{}{}
			failovers++
			continue
		}
		return profilePoolDispatchResult{lease: lease, account: account, failovers: failovers}, nil
	}
	return profilePoolDispatchResult{}, ErrNoEnvironmentProfileSlot
}
