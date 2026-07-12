package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeClaudeOAuthPoolRepository struct {
	pool              *OAuthPool
	credentials       []OAuthPoolCredential
	capsules          map[int64]*OAuthCapsuleSet
	updatedCredential *OAuthPoolCredential
}

func (f *fakeClaudeOAuthPoolRepository) Create(context.Context, *OAuthPool) error  { return nil }
func (f *fakeClaudeOAuthPoolRepository) Update(context.Context, *OAuthPool) error  { return nil }
func (f *fakeClaudeOAuthPoolRepository) Delete(context.Context, int64) error       { return nil }
func (f *fakeClaudeOAuthPoolRepository) List(context.Context) ([]OAuthPool, error) { return nil, nil }
func (f *fakeClaudeOAuthPoolRepository) AddCredential(context.Context, *OAuthPoolCredential) error {
	return nil
}
func (f *fakeClaudeOAuthPoolRepository) UpdateCredential(_ context.Context, credential *OAuthPoolCredential) error {
	copy := *credential
	f.updatedCredential = &copy
	for index := range f.credentials {
		if f.credentials[index].ID == credential.ID {
			f.credentials[index] = copy
		}
	}
	return nil
}
func (f *fakeClaudeOAuthPoolRepository) RemoveCredential(context.Context, int64, int64) error {
	return nil
}
func (f *fakeClaudeOAuthPoolRepository) CreateCapsuleSet(context.Context, *OAuthCapsuleSet) error {
	return nil
}
func (f *fakeClaudeOAuthPoolRepository) ActivateCapsuleSet(context.Context, int64, int64, string) (*OAuthPool, error) {
	return nil, nil
}
func (f *fakeClaudeOAuthPoolRepository) GetByID(_ context.Context, id int64) (*OAuthPool, error) {
	if f.pool == nil || f.pool.ID != id {
		return nil, errors.New("pool not found")
	}
	copy := *f.pool
	return &copy, nil
}
func (f *fakeClaudeOAuthPoolRepository) ListCredentials(context.Context, int64) ([]OAuthPoolCredential, error) {
	return append([]OAuthPoolCredential(nil), f.credentials...), nil
}
func (f *fakeClaudeOAuthPoolRepository) GetCapsuleSet(_ context.Context, _ int64, version int64) (*OAuthCapsuleSet, error) {
	set := f.capsules[version]
	if set == nil {
		return nil, errors.New("capsule not found")
	}
	return set, nil
}

type fakeClaudeOAuthAccountReader struct{ accounts []Account }

func (f *fakeClaudeOAuthAccountReader) GetByID(_ context.Context, id int64) (*Account, error) {
	for index := range f.accounts {
		if f.accounts[index].ID == id {
			account := f.accounts[index]
			return &account, nil
		}
	}
	return nil, errors.New("account not found")
}
func (f *fakeClaudeOAuthAccountReader) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	wanted := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	result := make([]*Account, 0, len(ids))
	for index := range f.accounts {
		if _, ok := wanted[f.accounts[index].ID]; ok {
			account := f.accounts[index]
			result = append(result, &account)
		}
	}
	return result, nil
}

type fakeClaudeOAuthBindingStore struct {
	bindings map[string]*ClaudeOAuthBinding
}

func (f *fakeClaudeOAuthBindingStore) GetOrCreateBinding(_ context.Context, candidate ClaudeOAuthBindingCandidate) (*ClaudeOAuthBinding, bool, error) {
	if binding := f.bindings[candidate.BindingHash]; binding != nil {
		copy := *binding
		return &copy, false, nil
	}
	binding := &ClaudeOAuthBinding{
		PoolID:            candidate.PoolID,
		BindingHash:       candidate.BindingHash,
		AccountID:         candidate.AccountID,
		CapsuleSetVersion: candidate.CapsuleSetVersion,
		CapsuleSlot:       candidate.CapsuleSlot,
	}
	f.bindings[candidate.BindingHash] = binding
	copy := *binding
	return &copy, true, nil
}
func (f *fakeClaudeOAuthBindingStore) MigrateBindingCAS(_ context.Context, migration ClaudeOAuthBindingMigration) (*ClaudeOAuthBinding, error) {
	binding := f.bindings[migration.BindingHash]
	if binding == nil {
		return nil, ErrClaudeOAuthBindingMissing
	}
	if binding.AccountID != migration.ExpectedAccountID || binding.Epoch != migration.ExpectedEpoch {
		return nil, ErrClaudeOAuthBindingCASConflict
	}
	binding.AccountID = migration.NewAccountID
	binding.Epoch++
	copy := *binding
	return &copy, nil
}
func (f *fakeClaudeOAuthBindingStore) ListCredentialBindingKeys(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (f *fakeClaudeOAuthBindingStore) DeleteCredentialBindings(context.Context, int64) (int64, error) {
	return 0, nil
}

func TestBuildClaudeOAuthCapsuleSetCreatesThreeFrozenSlots(t *testing.T) {
	set, err := BuildClaudeOAuthCapsuleSet(11, 1, "2.1.5", "America/Los_Angeles")
	require.NoError(t, err)
	require.Len(t, set.CompatibilityDigest, 64)

	pool, err := DecodeClaudeOAuthCapsuleProfilePool(set)
	require.NoError(t, err)
	require.True(t, pool.IsV2())
	require.Len(t, pool.Slots, 3)
	identities := make(map[string]struct{}, 3)
	for index, expectedEnvironment := range fixedClaudeEnvironmentSlotClasses {
		slot := pool.Slots[index]
		require.Equal(t, index, slot.Slot)
		require.Equal(t, expectedEnvironment, slot.Environment)
		require.NotNil(t, slot.Profile)
		require.False(t, slot.Profile.FrozenAt.IsZero())
		require.Equal(t, "America/Los_Angeles", slot.Profile.Timezone)
		identities[slot.Profile.DeviceID] = struct{}{}
	}
	require.Len(t, identities, 3)
}

func TestClaudeOAuthPoolSelectorUsesStableRendezvousAndCopyOnWriteCapsule(t *testing.T) {
	proxyID := int64(9)
	setOne, err := BuildClaudeOAuthCapsuleSet(11, 1, "2.1.5", "UTC")
	require.NoError(t, err)
	setTwo, err := BuildClaudeOAuthCapsuleSet(11, 2, "2.1.6", "UTC")
	require.NoError(t, err)
	poolRepo := &fakeClaudeOAuthPoolRepository{
		pool: &OAuthPool{
			ID:                      11,
			Status:                  OAuthPoolStatusActive,
			Mode:                    OAuthPoolModeShadow,
			EgressRouteID:           proxyID,
			AllowedModels:           []string{"claude-sonnet-4-6"},
			ActiveCapsuleSetVersion: 1,
		},
		credentials: []OAuthPoolCredential{
			{PoolID: 11, AccountID: 101, State: OAuthPoolCredentialAvailable},
			{PoolID: 11, AccountID: 102, State: OAuthPoolCredentialAvailable},
			{PoolID: 11, AccountID: 103, State: OAuthPoolCredentialAvailable},
		},
		capsules: map[int64]*OAuthCapsuleSet{1: setOne, 2: setTwo},
	}
	accountRepo := &fakeClaudeOAuthAccountReader{accounts: []Account{
		{ID: 101, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &proxyID},
		{ID: 102, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &proxyID},
		{ID: 103, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Status: StatusActive, Schedulable: true, ProxyID: &proxyID},
	}}
	bindings := &fakeClaudeOAuthBindingStore{bindings: make(map[string]*ClaudeOAuthBinding)}
	selector, err := NewClaudeOAuthPoolSelector(poolRepo, accountRepo, bindings, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	first, err := selector.Select(context.Background(), 11, "stable-binding", "claude-sonnet-4-6")
	require.NoError(t, err)
	require.True(t, first.Created)
	require.Contains(t, []int64{101, 102}, first.Account.ID)
	require.Equal(t, int64(1), first.Binding.CapsuleSetVersion)
	require.GreaterOrEqual(t, first.Binding.CapsuleSlot, 0)
	require.LessOrEqual(t, first.Binding.CapsuleSlot, 2)

	poolRepo.pool.ActiveCapsuleSetVersion = 2
	poolRepo.credentials[0], poolRepo.credentials[1] = poolRepo.credentials[1], poolRepo.credentials[0]
	second, err := selector.Select(context.Background(), 11, "stable-binding", "claude-sonnet-4-6")
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.Account.ID, second.Account.ID)
	require.Equal(t, first.Binding.CapsuleSlot, second.Binding.CapsuleSlot)
	require.Equal(t, int64(1), second.Binding.CapsuleSetVersion)
	require.Equal(t, "2.1.5", second.Profile.ClientVersion)
}

func TestClaudeOAuthPoolSelectorRejectsModelOutsidePool(t *testing.T) {
	selector := &ClaudeOAuthPoolSelector{
		poolRepo: &fakeClaudeOAuthPoolRepository{pool: &OAuthPool{
			ID: 11, Status: OAuthPoolStatusActive, Mode: OAuthPoolModeShadow, ActiveCapsuleSetVersion: 1, AllowedModels: []string{"claude-sonnet-4-6"},
		}},
	}
	_, err := selector.Select(context.Background(), 11, "stable-binding", "claude-opus-4-6")
	require.ErrorIs(t, err, ErrClaudeOAuthNoCompatibleCredential)
}
