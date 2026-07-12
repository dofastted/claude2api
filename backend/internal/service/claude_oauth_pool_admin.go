package service

import (
	"context"
	"fmt"
	"time"
)

type ClaudeOAuthPoolAdminService struct {
	pools         OAuthPoolRepository
	accounts      AccountRepository
	proxies       ProxyRepository
	bindings      ClaudeOAuthBindingStore
	shadowMetrics ClaudeOAuthShadowMetricsStore
	now           func() time.Time
}

func NewClaudeOAuthPoolAdminService(
	pools OAuthPoolRepository,
	accounts AccountRepository,
	bindings ClaudeOAuthBindingStore,
	proxies ProxyRepository,
	shadowMetrics ClaudeOAuthShadowMetricsStore,
) *ClaudeOAuthPoolAdminService {
	return &ClaudeOAuthPoolAdminService{
		pools: pools, accounts: accounts, proxies: proxies, shadowMetrics: shadowMetrics,
		bindings: bindings, now: time.Now,
	}
}

func (s *ClaudeOAuthPoolAdminService) CreatePool(ctx context.Context, pool *OAuthPool) (*OAuthPool, error) {
	if pool == nil {
		return nil, fmt.Errorf("create oauth pool: %w", ErrOAuthPoolInvalid)
	}
	pool.ID = 0
	pool.Provider = OAuthPoolProviderClaude
	pool.Mode = OAuthPoolModeShadow
	pool.ActiveCapsuleSetVersion = 0
	pool.PreviousCapsuleSetVersion = nil
	pool.CompatibilityDigest = ""
	now := s.now().UTC()
	pool.ShadowStartedAt = &now
	pool.ShadowQualifiedAt = nil
	if err := s.validateEgressRoute(ctx, pool.EgressRouteID); err != nil {
		return nil, err
	}
	if err := ValidateOAuthPool(pool); err != nil {
		return nil, err
	}
	if err := s.pools.Create(ctx, pool); err != nil {
		return nil, fmt.Errorf("create oauth pool: %w", err)
	}
	return pool, nil
}

func (s *ClaudeOAuthPoolAdminService) UpdatePool(ctx context.Context, poolID int64, update *OAuthPool) (*OAuthPool, error) {
	current, err := s.pools.GetByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if update == nil {
		return nil, fmt.Errorf("update oauth pool: %w", ErrOAuthPoolInvalid)
	}
	if update.EgressRouteID != current.EgressRouteID {
		credentials, listErr := s.pools.ListCredentials(ctx, poolID)
		if listErr != nil {
			return nil, listErr
		}
		if len(credentials) > 0 {
			return nil, fmt.Errorf("%w: remove credentials before changing egress route", ErrOAuthPoolInvalid)
		}
	}
	if err := s.validateEgressRoute(ctx, update.EgressRouteID); err != nil {
		return nil, err
	}
	current.Name = update.Name
	current.Status = update.Status
	current.EgressRouteID = update.EgressRouteID
	current.AllowedOrigins = update.AllowedOrigins
	current.AllowedModels = update.AllowedModels
	if err := ValidateOAuthPool(current); err != nil {
		return nil, err
	}
	if err := s.pools.Update(ctx, current); err != nil {
		return nil, fmt.Errorf("update oauth pool: %w", err)
	}
	return current, nil
}

func (s *ClaudeOAuthPoolAdminService) ListPools(ctx context.Context) ([]OAuthPool, error) {
	return s.pools.List(ctx)
}

func (s *ClaudeOAuthPoolAdminService) GetPool(ctx context.Context, poolID int64) (*OAuthPool, error) {
	return s.pools.GetByID(ctx, poolID)
}

func (s *ClaudeOAuthPoolAdminService) DeletePool(ctx context.Context, poolID int64) error {
	credentials, err := s.pools.ListCredentials(ctx, poolID)
	if err != nil {
		return err
	}
	if len(credentials) > 0 {
		return fmt.Errorf("%w: remove credentials before deleting pool", ErrOAuthPoolInvalid)
	}
	return s.pools.Delete(ctx, poolID)
}

func (s *ClaudeOAuthPoolAdminService) AddCredential(ctx context.Context, poolID, accountID int64) (*OAuthPoolCredential, error) {
	pool, err := s.pools.GetByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	account, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("load oauth pool account: %w", err)
	}
	if err := ValidateOAuthPoolAccount(pool, account); err != nil {
		return nil, err
	}
	credential := &OAuthPoolCredential{PoolID: poolID, AccountID: accountID, State: OAuthPoolCredentialAvailable}
	if err := s.pools.AddCredential(ctx, credential); err != nil {
		return nil, fmt.Errorf("add oauth pool credential: %w", err)
	}
	return credential, nil
}

func (s *ClaudeOAuthPoolAdminService) RemoveCredential(ctx context.Context, poolID, accountID int64) error {
	if _, err := s.ResetCredentialBindings(ctx, poolID, accountID); err != nil {
		return err
	}
	return s.pools.RemoveCredential(ctx, poolID, accountID)
}

func (s *ClaudeOAuthPoolAdminService) ListCredentials(ctx context.Context, poolID int64) ([]OAuthPoolCredential, error) {
	return s.pools.ListCredentials(ctx, poolID)
}

func (s *ClaudeOAuthPoolAdminService) BindingCounts(ctx context.Context, poolID int64) (map[int64]int, error) {
	credentials, err := s.pools.ListCredentials(ctx, poolID)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(credentials))
	for _, credential := range credentials {
		keys, listErr := s.bindings.ListCredentialBindingKeys(ctx, credential.AccountID)
		if listErr != nil {
			return nil, listErr
		}
		counts[credential.AccountID] = len(keys)
	}
	return counts, nil
}

func (s *ClaudeOAuthPoolAdminService) ResetCredentialBindings(ctx context.Context, poolID, accountID int64) (int64, error) {
	credentials, err := s.pools.ListCredentials(ctx, poolID)
	if err != nil {
		return 0, err
	}
	for _, credential := range credentials {
		if credential.AccountID == accountID {
			return s.bindings.DeleteCredentialBindings(ctx, accountID)
		}
	}
	return 0, fmt.Errorf("%w: account %d is not enrolled in pool %d", ErrOAuthPoolCredentialInvalid, accountID, poolID)
}

func (s *ClaudeOAuthPoolAdminService) CreateCapsuleSet(ctx context.Context, poolID, version int64, cliVersion, profileTimezone string) (*OAuthCapsuleSet, error) {
	if _, err := s.pools.GetByID(ctx, poolID); err != nil {
		return nil, err
	}
	set, err := BuildClaudeOAuthCapsuleSet(poolID, version, cliVersion, profileTimezone)
	if err != nil {
		return nil, err
	}
	if err := s.pools.CreateCapsuleSet(ctx, set); err != nil {
		return nil, fmt.Errorf("create oauth capsule set: %w", err)
	}
	return set, nil
}

func (s *ClaudeOAuthPoolAdminService) ActivateCapsuleSet(ctx context.Context, poolID, version int64) (*OAuthPool, error) {
	set, err := s.pools.GetCapsuleSet(ctx, poolID, version)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeClaudeOAuthCapsuleProfilePool(set); err != nil {
		return nil, err
	}
	return s.pools.ActivateCapsuleSet(ctx, poolID, version, set.CompatibilityDigest)
}

func (s *ClaudeOAuthPoolAdminService) ShadowMetrics(ctx context.Context, poolID int64) (*ClaudeOAuthShadowMetrics, error) {
	if _, err := s.pools.GetByID(ctx, poolID); err != nil {
		return nil, err
	}
	return s.shadowMetrics.Snapshot(ctx, poolID)
}

func (s *ClaudeOAuthPoolAdminService) SetMode(ctx context.Context, poolID int64, mode string) (*OAuthPool, error) {
	pool, err := s.pools.GetByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if mode != OAuthPoolModeShadow && mode != OAuthPoolModeEnforce {
		return nil, fmt.Errorf("%w: unsupported mode %q", ErrOAuthPoolInvalid, mode)
	}
	if pool.Mode == mode {
		return pool, nil
	}
	if mode == OAuthPoolModeShadow {
		now := s.now().UTC()
		pool.Mode = mode
		pool.ShadowStartedAt = &now
		pool.ShadowQualifiedAt = nil
		if err := s.shadowMetrics.Reset(ctx, poolID); err != nil {
			return nil, err
		}
	} else {
		metrics, metricsErr := s.shadowMetrics.Snapshot(ctx, poolID)
		if metricsErr != nil {
			return nil, metricsErr
		}
		credentials, credentialsErr := s.pools.ListCredentials(ctx, poolID)
		if credentialsErr != nil {
			return nil, credentialsErr
		}
		if !metrics.Qualified || pool.Status != OAuthPoolStatusActive || pool.ActiveCapsuleSetVersion <= 0 || pool.CompatibilityDigest == "" || len(credentials) == 0 {
			return nil, fmt.Errorf("%w: requires active pool, credential, capsule, %d consecutive days and %d shadow requests with zero hard failures", ErrOAuthPoolEnforceGateNotReached, ClaudeOAuthShadowRequiredDays, ClaudeOAuthShadowRequiredRequests)
		}
		now := s.now().UTC()
		pool.Mode = mode
		pool.ShadowQualifiedAt = &now
	}
	if err := ValidateOAuthPool(pool); err != nil {
		return nil, err
	}
	if err := s.pools.Update(ctx, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

func (s *ClaudeOAuthPoolAdminService) validateEgressRoute(ctx context.Context, proxyID int64) error {
	if proxyID <= 0 {
		return fmt.Errorf("%w: egress route is required", ErrOAuthPoolInvalid)
	}
	if _, err := s.proxies.GetByID(ctx, proxyID); err != nil {
		return fmt.Errorf("validate oauth pool egress route: %w", err)
	}
	return nil
}
