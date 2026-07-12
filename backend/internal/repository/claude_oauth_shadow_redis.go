package repository

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/dofastted/claude2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const claudeOAuthShadowMetricsTTL = 30 * 24 * time.Hour

type claudeOAuthShadowMetricsRedisStore struct {
	rdb *redis.Client
	now func() time.Time
}

func NewClaudeOAuthShadowMetricsRedisStore(rdb *redis.Client) service.ClaudeOAuthShadowMetricsStore {
	return &claudeOAuthShadowMetricsRedisStore{rdb: rdb, now: time.Now}
}

func (s *claudeOAuthShadowMetricsRedisStore) Record(ctx context.Context, observation service.ClaudeOAuthShadowObservation) error {
	if observation.PoolID <= 0 || s == nil || s.rdb == nil {
		return fmt.Errorf("record claude oauth shadow metrics: pool and redis client are required")
	}
	now := s.now().UTC()
	metricsKey := claudeOAuthShadowMetricsKey(observation.PoolID)
	daysKey := claudeOAuthShadowDaysKey(observation.PoolID)
	pipe := s.rdb.TxPipeline()
	pipe.HSetNX(ctx, metricsKey, "started_at", now.Unix())
	pipe.HSet(ctx, metricsKey, "last_observed_at", now.Unix())
	pipe.HIncrBy(ctx, metricsKey, "requests", 1)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "routing_diffs", observation.RoutingDiff)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "binding_errors", observation.BindingError)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "capsule_invariant_failures", observation.CapsuleInvariantFailure)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "direct_egress_attempts", observation.DirectEgressAttempt)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "session_conflicts", observation.SessionConflict)
	incrementClaudeOAuthShadowMetric(ctx, pipe, metricsKey, "unapproved_business_diffs", observation.UnapprovedBusinessDiff)
	pipe.SAdd(ctx, daysKey, now.Format(time.DateOnly))
	pipe.Expire(ctx, metricsKey, claudeOAuthShadowMetricsTTL)
	pipe.Expire(ctx, daysKey, claudeOAuthShadowMetricsTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record claude oauth shadow metrics: %w", err)
	}
	return nil
}

func (s *claudeOAuthShadowMetricsRedisStore) Snapshot(ctx context.Context, poolID int64) (*service.ClaudeOAuthShadowMetrics, error) {
	if poolID <= 0 || s == nil || s.rdb == nil {
		return nil, fmt.Errorf("read claude oauth shadow metrics: pool and redis client are required")
	}
	pipe := s.rdb.Pipeline()
	metricsCommand := pipe.HGetAll(ctx, claudeOAuthShadowMetricsKey(poolID))
	daysCommand := pipe.SMembers(ctx, claudeOAuthShadowDaysKey(poolID))
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("read claude oauth shadow metrics: %w", err)
	}
	values := metricsCommand.Val()
	metrics := &service.ClaudeOAuthShadowMetrics{
		PoolID:                   poolID,
		Requests:                 parseClaudeOAuthShadowInt(values["requests"]),
		RoutingDiffs:             parseClaudeOAuthShadowInt(values["routing_diffs"]),
		BindingErrors:            parseClaudeOAuthShadowInt(values["binding_errors"]),
		CapsuleInvariantFailures: parseClaudeOAuthShadowInt(values["capsule_invariant_failures"]),
		DirectEgressAttempts:     parseClaudeOAuthShadowInt(values["direct_egress_attempts"]),
		SessionConflicts:         parseClaudeOAuthShadowInt(values["session_conflicts"]),
		UnapprovedBusinessDiffs:  parseClaudeOAuthShadowInt(values["unapproved_business_diffs"]),
		ConsecutiveDays:          consecutiveClaudeOAuthShadowDays(daysCommand.Val()),
	}
	if unix := parseClaudeOAuthShadowInt(values["started_at"]); unix > 0 {
		started := time.Unix(unix, 0).UTC()
		metrics.StartedAt = &started
	}
	if unix := parseClaudeOAuthShadowInt(values["last_observed_at"]); unix > 0 {
		last := time.Unix(unix, 0).UTC()
		metrics.LastObservedAt = &last
	}
	metrics.Qualified = service.QualifyClaudeOAuthShadowMetrics(metrics)
	return metrics, nil
}

func (s *claudeOAuthShadowMetricsRedisStore) Reset(ctx context.Context, poolID int64) error {
	if poolID <= 0 || s == nil || s.rdb == nil {
		return fmt.Errorf("reset claude oauth shadow metrics: pool and redis client are required")
	}
	return s.rdb.Del(ctx, claudeOAuthShadowMetricsKey(poolID), claudeOAuthShadowDaysKey(poolID)).Err()
}

func incrementClaudeOAuthShadowMetric(ctx context.Context, pipe redis.Pipeliner, key, field string, enabled bool) {
	if enabled {
		pipe.HIncrBy(ctx, key, field, 1)
	}
}

func claudeOAuthShadowMetricsKey(poolID int64) string {
	return "claude_oauth:shadow_metrics:" + strconv.FormatInt(poolID, 10)
}

func claudeOAuthShadowDaysKey(poolID int64) string {
	return "claude_oauth:shadow_days:" + strconv.FormatInt(poolID, 10)
}

func parseClaudeOAuthShadowInt(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func consecutiveClaudeOAuthShadowDays(values []string) int {
	dates := make([]time.Time, 0, len(values))
	for _, value := range values {
		date, err := time.Parse(time.DateOnly, value)
		if err == nil {
			dates = append(dates, date)
		}
	}
	sort.Slice(dates, func(left, right int) bool { return dates[left].Before(dates[right]) })
	current := 0
	var previous time.Time
	for _, date := range dates {
		switch {
		case previous.IsZero():
			current = 1
		case date.Equal(previous):
			continue
		case date.Equal(previous.AddDate(0, 0, 1)):
			current++
		default:
			current = 1
		}
		previous = date
	}
	return current
}
