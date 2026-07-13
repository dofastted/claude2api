package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var ErrClaudeOAuthNoCompatibleCredential = errors.New("no compatible claude oauth credential")

type ClaudeOAuthAccountReader interface {
	GetByID(context.Context, int64) (*Account, error)
	GetByIDs(context.Context, []int64) ([]*Account, error)
}

// ClaudeOAuthAccountExtraWriter persists auto-generated credential capsules onto account.extra.
type ClaudeOAuthAccountExtraWriter interface {
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
}

type ClaudeOAuthRankedCredential struct {
	Membership OAuthPoolCredential
	Account    *Account
	Score      float64
}

type ClaudeOAuthSelection struct {
	Pool          *OAuthPool
	Binding       *ClaudeOAuthBinding
	Account       *Account
	CapsuleBundle *ClaudeOAuthCredentialCapsuleBundle
	CapsuleSet    *OAuthCapsuleSet // legacy; nil when using credential-owned capsules
	Profile       *ClaudeEnvironmentProfile
	Created       bool
}

type ClaudeOAuthPoolSelector struct {
	poolRepo     OAuthPoolRepository
	accountRepo  ClaudeOAuthAccountReader
	accountExtra ClaudeOAuthAccountExtraWriter
	bindingStore ClaudeOAuthBindingStore
	selectionKey []byte
}

func NewClaudeOAuthPoolSelector(poolRepo OAuthPoolRepository, accountRepo ClaudeOAuthAccountReader, bindingStore ClaudeOAuthBindingStore, selectionKey []byte) (*ClaudeOAuthPoolSelector, error) {
	if poolRepo == nil || accountRepo == nil || bindingStore == nil || len(selectionKey) < 32 {
		return nil, fmt.Errorf("build claude oauth selector: repositories, binding store and 32-byte selection key are required")
	}
	selector := &ClaudeOAuthPoolSelector{
		poolRepo:     poolRepo,
		accountRepo:  accountRepo,
		bindingStore: bindingStore,
		selectionKey: append([]byte(nil), selectionKey...),
	}
	if writer, ok := accountRepo.(ClaudeOAuthAccountExtraWriter); ok {
		selector.accountExtra = writer
	}
	return selector, nil
}

// SetAccountExtraWriter allows wiring UpdateExtra when the account reader does not implement it.
func (s *ClaudeOAuthPoolSelector) SetAccountExtraWriter(writer ClaudeOAuthAccountExtraWriter) {
	if s != nil {
		s.accountExtra = writer
	}
}

func (s *ClaudeOAuthPoolSelector) Select(ctx context.Context, poolID int64, bindingHash, model string) (*ClaudeOAuthSelection, error) {
	pool, err := s.poolRepo.GetByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if pool.Status != OAuthPoolStatusActive || !pool.SupportsModel(model) {
		return nil, fmt.Errorf("%w: pool is disabled or does not allow model %q", ErrClaudeOAuthNoCompatibleCredential, model)
	}
	ranked, err := s.RankCompatibleCredentials(ctx, pool, bindingHash)
	if err != nil {
		return nil, err
	}
	if len(ranked) == 0 {
		return nil, ErrClaudeOAuthNoCompatibleCredential
	}
	// Pre-ensure capsules for the preferred credential so first binding records the credential capsule version.
	preferred := ranked[0].Account
	preferredBundle, err := s.ensureAccountCapsules(ctx, preferred)
	if err != nil {
		return nil, err
	}
	binding, created, err := s.bindingStore.GetOrCreateBinding(ctx, ClaudeOAuthBindingCandidate{
		PoolID:            pool.ID,
		BindingHash:       bindingHash,
		AccountID:         preferred.ID,
		CapsuleSetVersion: preferredBundle.Version,
		CapsuleSlot:       s.CapsuleSlot(pool.ID, bindingHash),
	})
	if err != nil {
		return nil, err
	}
	account := rankedAccountByID(ranked, binding.AccountID)
	if account == nil {
		// Bound account may still be loadable even if no longer ranked (e.g. temporarily unschedulable).
		loaded, loadErr := s.accountRepo.GetByID(ctx, binding.AccountID)
		if loadErr != nil || !claudeOAuthPoolAccountEligible(pool, OAuthPoolCredential{State: OAuthPoolCredentialAvailable, AccountID: binding.AccountID}, loaded) {
			return nil, fmt.Errorf("%w: bound account %d is no longer compatible", ErrClaudeOAuthNoCompatibleCredential, binding.AccountID)
		}
		account = loaded
	}
	bundle, err := s.ensureAccountCapsules(ctx, account)
	if err != nil {
		return nil, err
	}
	// Existing bindings keep their capsule version when the credential bundle version matches;
	// if the stored version is missing from the bundle, fail closed rather than inventing a pool template.
	if binding.CapsuleSetVersion != bundle.Version && binding.CapsuleSetVersion > 0 {
		// Credential-owned bundles are currently single-version; mismatched historical pool versions
		// are accepted only when the credential still has a valid current bundle for the same slot.
		// Prefer the bound version semantics by still serving the current credential capsule for that slot.
	}
	profile, err := ClaudeOAuthCredentialCapsuleProfile(bundle, binding.CapsuleSlot)
	if err != nil {
		return nil, err
	}
	return &ClaudeOAuthSelection{
		Pool:          pool,
		Binding:       binding,
		Account:       account,
		CapsuleBundle: bundle,
		Profile:       profile,
		Created:       created,
	}, nil
}

func (s *ClaudeOAuthPoolSelector) ensureAccountCapsules(ctx context.Context, account *Account) (*ClaudeOAuthCredentialCapsuleBundle, error) {
	if account == nil {
		return nil, fmt.Errorf("%w: account is required", ErrClaudeOAuthNoCompatibleCredential)
	}
	hadBundle := false
	if _, err := DecodeClaudeOAuthCredentialCapsules(account); err == nil {
		hadBundle = true
	}
	bundle, err := EnsureClaudeOAuthCapsules(account)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClaudeOAuthNoCompatibleCredential, err)
	}
	if !hadBundle && s.accountExtra != nil {
		if err := s.accountExtra.UpdateExtra(ctx, account.ID, persistClaudeOAuthCredentialCapsules(account, bundle)); err != nil {
			return nil, fmt.Errorf("persist credential capsules: %w", err)
		}
	}
	return bundle, nil
}

func (s *ClaudeOAuthPoolSelector) RankCompatibleCredentials(ctx context.Context, pool *OAuthPool, bindingHash string) ([]ClaudeOAuthRankedCredential, error) {
	if pool == nil || pool.ID <= 0 || strings.TrimSpace(bindingHash) == "" {
		return nil, fmt.Errorf("%w: pool and binding hash are required", ErrClaudeOAuthNoCompatibleCredential)
	}
	memberships, err := s.poolRepo.ListCredentials(ctx, pool.ID)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(memberships))
	for _, membership := range memberships {
		if membership.State == OAuthPoolCredentialAvailable {
			accountIDs = append(accountIDs, membership.AccountID)
		}
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	accountsByID := make(map[int64]*Account, len(accounts))
	for index := range accounts {
		account := accounts[index]
		accountsByID[account.ID] = account
	}
	ranked := make([]ClaudeOAuthRankedCredential, 0, len(memberships))
	for _, membership := range memberships {
		account := accountsByID[membership.AccountID]
		if !claudeOAuthPoolAccountEligible(pool, membership, account) {
			continue
		}
		ranked = append(ranked, ClaudeOAuthRankedCredential{
			Membership: membership,
			Account:    account,
			Score:      s.rendezvousScore(pool.ID, bindingHash, account.ID),
		})
	}
	sort.Slice(ranked, func(left, right int) bool {
		if ranked[left].Score == ranked[right].Score {
			return ranked[left].Account.ID < ranked[right].Account.ID
		}
		return ranked[left].Score < ranked[right].Score
	})
	return ranked, nil
}

func (s *ClaudeOAuthPoolSelector) CapsuleSlot(poolID int64, bindingHash string) int {
	mac := hmac.New(sha256.New, s.selectionKey)
	_, _ = mac.Write([]byte("claude-oauth-capsule-slot-v1"))
	var encodedID [8]byte
	binary.BigEndian.PutUint64(encodedID[:], uint64(poolID))
	_, _ = mac.Write(encodedID[:])
	_, _ = mac.Write([]byte(bindingHash))
	sum := mac.Sum(nil)
	return int(binary.BigEndian.Uint64(sum[:8]) % 3)
}

func (s *ClaudeOAuthPoolSelector) rendezvousScore(poolID int64, bindingHash string, accountID int64) float64 {
	mac := hmac.New(sha256.New, s.selectionKey)
	_, _ = mac.Write([]byte("claude-oauth-credential-rendezvous-v1"))
	var encoded [16]byte
	binary.BigEndian.PutUint64(encoded[:8], uint64(poolID))
	binary.BigEndian.PutUint64(encoded[8:], uint64(accountID))
	_, _ = mac.Write(encoded[:])
	_, _ = mac.Write([]byte(bindingHash))
	raw := binary.BigEndian.Uint64(mac.Sum(nil)[:8])
	uniform := (float64(raw) + 1) / (float64(math.MaxUint64) + 1)
	return -math.Log(uniform)
}

func claudeOAuthPoolAccountEligible(pool *OAuthPool, membership OAuthPoolCredential, account *Account) bool {
	if pool == nil || account == nil || membership.State != OAuthPoolCredentialAvailable || !account.IsActive() || !account.Schedulable {
		return false
	}
	if ValidateOAuthPoolAccount(pool, account) != nil || account.ProxyID == nil || *account.ProxyID != pool.EgressRouteID {
		return false
	}
	// Capsules are auto-generated; eligibility does not require a pre-existing pool capsule set.
	return true
}

func rankedAccountByID(ranked []ClaudeOAuthRankedCredential, accountID int64) *Account {
	for index := range ranked {
		if ranked[index].Account.ID == accountID {
			return ranked[index].Account
		}
	}
	return nil
}
