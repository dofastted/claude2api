package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/dofastted/claude2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newClaudeOAuthBindingTestStore(t *testing.T) (*claudeOAuthBindingRedisStore, *miniredis.Miniredis) {
	t.Helper()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &claudeOAuthBindingRedisStore{rdb: client, now: time.Now}, mini
}

func TestClaudeOAuthBindingGetOrCreateIsStickyAndRefreshesTTL(t *testing.T) {
	store, mini := newClaudeOAuthBindingTestStore(t)
	ctx := context.Background()
	candidate := service.ClaudeOAuthBindingCandidate{
		PoolID:            11,
		BindingHash:       "binding-hash",
		AccountID:         101,
		CapsuleSetVersion: 7,
		CapsuleSlot:       2,
	}

	binding, created, err := store.GetOrCreateBinding(ctx, candidate)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, int64(101), binding.AccountID)
	require.Equal(t, int64(0), binding.Epoch)

	key := claudeOAuthBindingKey(candidate.PoolID, candidate.BindingHash)
	ttl := mini.TTL(key)
	require.Greater(t, ttl, 59*time.Minute)
	require.LessOrEqual(t, ttl, service.ClaudeOAuthBindingTTL)

	mini.FastForward(30 * time.Minute)
	candidate.AccountID = 202
	candidate.CapsuleSetVersion = 8
	candidate.CapsuleSlot = 0
	binding, created, err = store.GetOrCreateBinding(ctx, candidate)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, int64(101), binding.AccountID)
	require.Equal(t, int64(7), binding.CapsuleSetVersion)
	require.Equal(t, 2, binding.CapsuleSlot)
	require.Greater(t, mini.TTL(key), 59*time.Minute)

	keys, err := store.ListCredentialBindingKeys(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, []string{key}, keys)
	keys, err = store.ListCredentialBindingKeys(ctx, 202)
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestClaudeOAuthBindingDeleteCredentialBindingsRemovesForwardAndReverseKeys(t *testing.T) {
	store, mini := newClaudeOAuthBindingTestStore(t)
	ctx := context.Background()
	for _, hash := range []string{"binding-a", "binding-b"} {
		_, _, err := store.GetOrCreateBinding(ctx, service.ClaudeOAuthBindingCandidate{
			PoolID: 11, BindingHash: hash, AccountID: 101, CapsuleSetVersion: 7, CapsuleSlot: 1,
		})
		require.NoError(t, err)
	}

	deleted, err := store.DeleteCredentialBindings(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.False(t, mini.Exists(claudeOAuthBindingKey(11, "binding-a")))
	require.False(t, mini.Exists(claudeOAuthBindingKey(11, "binding-b")))
	require.False(t, mini.Exists(claudeOAuthCredentialBindingsKey(101)))
}

func TestClaudeOAuthBindingMigrateCASMovesReverseIndex(t *testing.T) {
	store, _ := newClaudeOAuthBindingTestStore(t)
	ctx := context.Background()
	candidate := service.ClaudeOAuthBindingCandidate{
		PoolID:            11,
		BindingHash:       "binding-hash",
		AccountID:         101,
		CapsuleSetVersion: 7,
		CapsuleSlot:       2,
	}
	_, _, err := store.GetOrCreateBinding(ctx, candidate)
	require.NoError(t, err)

	migrated, err := store.MigrateBindingCAS(ctx, service.ClaudeOAuthBindingMigration{
		PoolID:            11,
		BindingHash:       "binding-hash",
		ExpectedAccountID: 101,
		ExpectedEpoch:     0,
		NewAccountID:      202,
	})
	require.NoError(t, err)
	require.Equal(t, int64(202), migrated.AccountID)
	require.Equal(t, int64(1), migrated.Epoch)
	require.Equal(t, int64(7), migrated.CapsuleSetVersion)
	require.Equal(t, 2, migrated.CapsuleSlot)

	oldKeys, err := store.ListCredentialBindingKeys(ctx, 101)
	require.NoError(t, err)
	require.Empty(t, oldKeys)
	newKeys, err := store.ListCredentialBindingKeys(ctx, 202)
	require.NoError(t, err)
	require.Equal(t, []string{claudeOAuthBindingKey(11, "binding-hash")}, newKeys)

	_, err = store.MigrateBindingCAS(ctx, service.ClaudeOAuthBindingMigration{
		PoolID:            11,
		BindingHash:       "binding-hash",
		ExpectedAccountID: 101,
		ExpectedEpoch:     0,
		NewAccountID:      303,
	})
	require.ErrorIs(t, err, service.ErrClaudeOAuthBindingCASConflict)
}

func TestClaudeOAuthBindingMigrateCASRejectsMissingBinding(t *testing.T) {
	store, _ := newClaudeOAuthBindingTestStore(t)
	_, err := store.MigrateBindingCAS(context.Background(), service.ClaudeOAuthBindingMigration{
		PoolID:            11,
		BindingHash:       "missing",
		ExpectedAccountID: 101,
		ExpectedEpoch:     0,
		NewAccountID:      202,
	})
	require.ErrorIs(t, err, service.ErrClaudeOAuthBindingMissing)
}
