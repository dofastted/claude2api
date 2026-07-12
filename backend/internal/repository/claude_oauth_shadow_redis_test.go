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

func TestClaudeOAuthShadowMetricsRedisStoreRecordsAndResets(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store := &claudeOAuthShadowMetricsRedisStore{rdb: client, now: func() time.Time {
		return time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)
	}}

	ctx := context.Background()
	require.NoError(t, store.Record(ctx, service.ClaudeOAuthShadowObservation{
		PoolID: 9, RoutingDiff: true, BindingError: true,
	}))

	metrics, err := store.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Equal(t, int64(1), metrics.Requests)
	require.Equal(t, int64(1), metrics.RoutingDiffs)
	require.Equal(t, int64(1), metrics.BindingErrors)
	require.Equal(t, 1, metrics.ConsecutiveDays)
	require.False(t, metrics.Qualified)

	require.NoError(t, store.Reset(ctx, 9))
	metrics, err = store.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, metrics.Requests)
	require.Zero(t, metrics.ConsecutiveDays)
}

func TestConsecutiveClaudeOAuthShadowDays(t *testing.T) {
	require.Equal(t, 3, consecutiveClaudeOAuthShadowDays([]string{
		"2026-07-10", "2026-07-11", "2026-07-12", "2026-07-08", "invalid",
	}))
	require.Equal(t, 1, consecutiveClaudeOAuthShadowDays([]string{
		"2026-07-01", "2026-07-02", "2026-07-03", "2026-07-04", "2026-07-05", "2026-07-06", "2026-07-07", "2026-07-10",
	}))
}
