package service

import (
	"context"
	"time"
)

const (
	ClaudeOAuthShadowRequiredDays     = 7
	ClaudeOAuthShadowRequiredRequests = int64(10_000)
)

type ClaudeOAuthShadowObservation struct {
	PoolID                  int64
	RoutingDiff             bool
	BindingError            bool
	CapsuleInvariantFailure bool
	DirectEgressAttempt     bool
	SessionConflict         bool
	UnapprovedBusinessDiff  bool
}

type ClaudeOAuthShadowMetrics struct {
	PoolID                   int64      `json:"pool_id"`
	Requests                 int64      `json:"requests"`
	RoutingDiffs             int64      `json:"routing_diffs"`
	BindingErrors            int64      `json:"binding_errors"`
	CapsuleInvariantFailures int64      `json:"capsule_invariant_failures"`
	DirectEgressAttempts     int64      `json:"direct_egress_attempts"`
	SessionConflicts         int64      `json:"session_conflicts"`
	UnapprovedBusinessDiffs  int64      `json:"unapproved_business_diffs"`
	ConsecutiveDays          int        `json:"consecutive_days"`
	StartedAt                *time.Time `json:"started_at,omitempty"`
	LastObservedAt           *time.Time `json:"last_observed_at,omitempty"`
	Qualified                bool       `json:"qualified"`
}

type ClaudeOAuthShadowMetricsStore interface {
	Record(context.Context, ClaudeOAuthShadowObservation) error
	Snapshot(context.Context, int64) (*ClaudeOAuthShadowMetrics, error)
	Reset(context.Context, int64) error
}

type ClaudeOAuthShadowProbe struct {
	PoolID     int64
	Prediction *ClaudeOAuthSelection
	BindingErr error
	Recorded   bool
}

type claudeOAuthShadowProbeContextKey struct{}

func WithClaudeOAuthShadowProbe(ctx context.Context, probe *ClaudeOAuthShadowProbe) context.Context {
	if probe == nil {
		return ctx
	}
	return context.WithValue(ctx, claudeOAuthShadowProbeContextKey{}, probe)
}

func ClaudeOAuthShadowProbeFromContext(ctx context.Context) (*ClaudeOAuthShadowProbe, bool) {
	if ctx == nil {
		return nil, false
	}
	probe, ok := ctx.Value(claudeOAuthShadowProbeContextKey{}).(*ClaudeOAuthShadowProbe)
	return probe, ok && probe != nil
}

func QualifyClaudeOAuthShadowMetrics(metrics *ClaudeOAuthShadowMetrics) bool {
	if metrics == nil || metrics.Requests < ClaudeOAuthShadowRequiredRequests || metrics.ConsecutiveDays < ClaudeOAuthShadowRequiredDays {
		return false
	}
	return metrics.BindingErrors == 0 &&
		metrics.CapsuleInvariantFailures == 0 &&
		metrics.DirectEgressAttempts == 0 &&
		metrics.SessionConflicts == 0 &&
		metrics.UnapprovedBusinessDiffs == 0
}
