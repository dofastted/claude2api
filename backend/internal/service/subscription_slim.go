//go:build slim

package service

import (
	"context"

	infraerrors "github.com/dofastted/claude2api/internal/pkg/errors"
)

var (
	ErrSubscriptionNotFound       = infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")
	ErrSubscriptionExpired        = infraerrors.Forbidden("SUBSCRIPTION_EXPIRED", "subscription has expired")
	ErrSubscriptionSuspended      = infraerrors.Forbidden("SUBSCRIPTION_SUSPENDED", "subscription is suspended")
	ErrSubscriptionAlreadyExists  = infraerrors.Conflict("SUBSCRIPTION_ALREADY_EXISTS", "subscription already exists for this user and group")
	ErrSubscriptionAssignConflict = infraerrors.Conflict("SUBSCRIPTION_ASSIGN_CONFLICT", "subscription exists but request conflicts with existing assignment semantics")
	ErrGroupNotSubscriptionType   = infraerrors.BadRequest("GROUP_NOT_SUBSCRIPTION_TYPE", "group is not a subscription type")
	ErrInvalidInput               = infraerrors.BadRequest("INVALID_INPUT", "at least one of resetDaily, resetWeekly, or resetMonthly must be true")
	ErrDailyLimitExceeded         = infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily usage limit exceeded")
	ErrWeeklyLimitExceeded        = infraerrors.TooManyRequests("WEEKLY_LIMIT_EXCEEDED", "weekly usage limit exceeded")
	ErrMonthlyLimitExceeded       = infraerrors.TooManyRequests("MONTHLY_LIMIT_EXCEEDED", "monthly usage limit exceeded")
	ErrSubscriptionNilInput       = infraerrors.BadRequest("SUBSCRIPTION_NIL_INPUT", "subscription input cannot be nil")
	ErrAdjustWouldExpire          = infraerrors.BadRequest("ADJUST_WOULD_EXPIRE", "adjustment would result in expired subscription (remaining days must be > 0)")
)

const MaxValidityDays = 36500

// SubscriptionService is intentionally inert in slim builds. It preserves
// constructor signatures for core auth/redeem paths without enabling billing.
type SubscriptionService struct {
	userSubRepo UserSubscriptionRepository
}

type AssignSubscriptionInput struct {
	UserID       int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

type BulkAssignSubscriptionInput struct {
	UserIDs      []int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

type BulkAssignResult struct {
	SuccessCount  int
	CreatedCount  int
	ReusedCount   int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Statuses      map[int64]string
}

type SubscriptionProgress struct {
	DailyUsedUSD    float64
	DailyLimitUSD   float64
	WeeklyUsedUSD   float64
	WeeklyLimitUSD  float64
	MonthlyUsedUSD  float64
	MonthlyLimitUSD float64
}

func (s *SubscriptionService) AssignOrExtendSubscription(context.Context, *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	return nil, false, nil
}

func (s *SubscriptionService) InvalidateSubCache(int64, int64) {}

func (s *SubscriptionService) AssignSubscription(context.Context, *AssignSubscriptionInput) (*UserSubscription, error) {
	return nil, nil
}

func (s *SubscriptionService) BulkAssignSubscription(context.Context, *BulkAssignSubscriptionInput) (*BulkAssignResult, error) {
	return &BulkAssignResult{}, nil
}

func (s *SubscriptionService) ListUserSubscriptions(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (s *SubscriptionService) ListActiveUserSubscriptions(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (s *SubscriptionService) GetSubscriptionProgress(context.Context, int64) (*SubscriptionProgress, error) {
	return &SubscriptionProgress{}, nil
}

func (s *SubscriptionService) GetByID(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (s *SubscriptionService) RevokeSubscription(context.Context, int64) error {
	return nil
}

func (s *SubscriptionService) ExtendSubscription(context.Context, int64, int) (*UserSubscription, error) {
	return nil, nil
}

func (s *SubscriptionService) AdminResetQuota(context.Context, int64, bool, bool, bool) (*UserSubscription, error) {
	return nil, nil
}
