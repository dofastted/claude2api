package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type claudeOAuthPoolAdminRepoStub struct {
	pool        *OAuthPool
	credentials []OAuthPoolCredential
	updated     *OAuthPool
	removed     int64
}

func (r *claudeOAuthPoolAdminRepoStub) Create(context.Context, *OAuthPool) error { return nil }
func (r *claudeOAuthPoolAdminRepoStub) Update(_ context.Context, pool *OAuthPool) error {
	copy := *pool
	r.updated = &copy
	r.pool = &copy
	return nil
}
func (r *claudeOAuthPoolAdminRepoStub) GetByID(context.Context, int64) (*OAuthPool, error) {
	copy := *r.pool
	return &copy, nil
}
func (r *claudeOAuthPoolAdminRepoStub) List(context.Context) ([]OAuthPool, error) { return nil, nil }
func (r *claudeOAuthPoolAdminRepoStub) Delete(context.Context, int64) error       { return nil }
func (r *claudeOAuthPoolAdminRepoStub) AddCredential(context.Context, *OAuthPoolCredential) error {
	return nil
}
func (r *claudeOAuthPoolAdminRepoStub) UpdateCredential(context.Context, *OAuthPoolCredential) error {
	return nil
}
func (r *claudeOAuthPoolAdminRepoStub) RemoveCredential(_ context.Context, _ int64, accountID int64) error {
	r.removed = accountID
	return nil
}
func (r *claudeOAuthPoolAdminRepoStub) ListCredentials(context.Context, int64) ([]OAuthPoolCredential, error) {
	return r.credentials, nil
}
func (r *claudeOAuthPoolAdminRepoStub) CreateCapsuleSet(context.Context, *OAuthCapsuleSet) error {
	return nil
}
func (r *claudeOAuthPoolAdminRepoStub) GetCapsuleSet(context.Context, int64, int64) (*OAuthCapsuleSet, error) {
	return nil, nil
}
func (r *claudeOAuthPoolAdminRepoStub) ActivateCapsuleSet(context.Context, int64, int64, string) (*OAuthPool, error) {
	return nil, nil
}

type claudeOAuthShadowMetricsStub struct {
	metrics *ClaudeOAuthShadowMetrics
	reset   bool
}

func (s *claudeOAuthShadowMetricsStub) Record(context.Context, ClaudeOAuthShadowObservation) error {
	return nil
}
func (s *claudeOAuthShadowMetricsStub) Snapshot(context.Context, int64) (*ClaudeOAuthShadowMetrics, error) {
	copy := *s.metrics
	copy.Qualified = QualifyClaudeOAuthShadowMetrics(&copy)
	return &copy, nil
}
func (s *claudeOAuthShadowMetricsStub) Reset(context.Context, int64) error {
	s.reset = true
	return nil
}

type claudeOAuthBindingAdminStub struct {
	keys    map[int64][]string
	deleted map[int64]int64
}

func (s *claudeOAuthBindingAdminStub) GetOrCreateBinding(context.Context, ClaudeOAuthBindingCandidate) (*ClaudeOAuthBinding, bool, error) {
	return nil, false, nil
}
func (s *claudeOAuthBindingAdminStub) MigrateBindingCAS(context.Context, ClaudeOAuthBindingMigration) (*ClaudeOAuthBinding, error) {
	return nil, nil
}
func (s *claudeOAuthBindingAdminStub) ListCredentialBindingKeys(_ context.Context, accountID int64) ([]string, error) {
	return s.keys[accountID], nil
}
func (s *claudeOAuthBindingAdminStub) DeleteCredentialBindings(_ context.Context, accountID int64) (int64, error) {
	return s.deleted[accountID], nil
}

func TestClaudeOAuthPoolAdminServiceEnforceGate(t *testing.T) {
	pool := &OAuthPool{
		ID: 7, Name: "primary", Provider: OAuthPoolProviderClaude, Status: OAuthPoolStatusActive,
		Mode: OAuthPoolModeShadow, EgressRouteID: 11,
		AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"},
		AllowedModels:  []string{"claude-*"}, ActiveCapsuleSetVersion: 3,
		CompatibilityDigest: "digest", SessionTTLSeconds: ClaudeOAuthSessionTTLSeconds,
	}
	repo := &claudeOAuthPoolAdminRepoStub{pool: pool, credentials: []OAuthPoolCredential{{AccountID: 22}}}
	metrics := &claudeOAuthShadowMetricsStub{metrics: &ClaudeOAuthShadowMetrics{
		PoolID: 7, Requests: ClaudeOAuthShadowRequiredRequests, ConsecutiveDays: ClaudeOAuthShadowRequiredDays,
		BindingErrors: 1,
	}}
	admin := NewClaudeOAuthPoolAdminService(repo, nil, nil, nil, metrics)

	_, err := admin.SetMode(context.Background(), 7, OAuthPoolModeEnforce)
	require.ErrorIs(t, err, ErrOAuthPoolEnforceGateNotReached)
	require.Nil(t, repo.updated)

	metrics.metrics.BindingErrors = 0
	updated, err := admin.SetMode(context.Background(), 7, OAuthPoolModeEnforce)
	require.NoError(t, err)
	require.Equal(t, OAuthPoolModeEnforce, updated.Mode)
	require.NotNil(t, updated.ShadowQualifiedAt)
	require.Equal(t, OAuthPoolModeEnforce, repo.updated.Mode)
}

func TestClaudeOAuthPoolAdminServiceReturningToShadowResetsMetrics(t *testing.T) {
	repo := &claudeOAuthPoolAdminRepoStub{pool: &OAuthPool{
		ID: 8, Name: "primary", Provider: OAuthPoolProviderClaude, Status: OAuthPoolStatusActive,
		Mode: OAuthPoolModeEnforce, EgressRouteID: 11,
		AllowedOrigins: []string{"https://api.anthropic.com/v1/messages"}, AllowedModels: []string{"claude-*"},
		ActiveCapsuleSetVersion: 3, CompatibilityDigest: "digest", SessionTTLSeconds: ClaudeOAuthSessionTTLSeconds,
	}}
	metrics := &claudeOAuthShadowMetricsStub{metrics: &ClaudeOAuthShadowMetrics{PoolID: 8}}
	admin := NewClaudeOAuthPoolAdminService(repo, nil, nil, nil, metrics)

	updated, err := admin.SetMode(context.Background(), 8, OAuthPoolModeShadow)
	require.NoError(t, err)
	require.Equal(t, OAuthPoolModeShadow, updated.Mode)
	require.True(t, metrics.reset)
	require.NotNil(t, updated.ShadowStartedAt)
}

func TestClaudeOAuthPoolAdminServiceBindingManagementIsPoolScoped(t *testing.T) {
	repo := &claudeOAuthPoolAdminRepoStub{
		pool:        &OAuthPool{ID: 7},
		credentials: []OAuthPoolCredential{{PoolID: 7, AccountID: 22}},
	}
	bindings := &claudeOAuthBindingAdminStub{
		keys: map[int64][]string{22: {"a", "b"}}, deleted: map[int64]int64{22: 2},
	}
	admin := NewClaudeOAuthPoolAdminService(repo, nil, bindings, nil, &claudeOAuthShadowMetricsStub{metrics: &ClaudeOAuthShadowMetrics{}})

	counts, err := admin.BindingCounts(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, map[int64]int{22: 2}, counts)

	deleted, err := admin.ResetCredentialBindings(context.Background(), 7, 22)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	require.NoError(t, admin.RemoveCredential(context.Background(), 7, 22))
	require.Equal(t, int64(22), repo.removed)

	_, err = admin.ResetCredentialBindings(context.Background(), 7, 99)
	require.ErrorIs(t, err, ErrOAuthPoolCredentialInvalid)
}
