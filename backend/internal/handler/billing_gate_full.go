//go:build !slim

package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type billingGateFull struct {
	billingCache *service.BillingCacheService
}

func NewBillingGate(billingCache *service.BillingCacheService) BillingGate {
	return &billingGateFull{billingCache: billingCache}
}

func (g *billingGateFull) CheckEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, platform string) error {
	return g.billingCache.CheckBillingEligibility(ctx, user, apiKey, group, subscription, platform)
}
