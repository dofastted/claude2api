//go:build slim

package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type billingGateSlim struct{}

func NewBillingGate(_ *service.BillingCacheService) BillingGate {
	return &billingGateSlim{}
}

func (g *billingGateSlim) CheckEligibility(_ context.Context, _ *service.User, _ *service.APIKey, _ *service.Group, _ *service.UserSubscription, _ string) error {
	// slim: 纯 API key 鉴权，不做余额/配额拦截，恒放行。
	return nil
}
