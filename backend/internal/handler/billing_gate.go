package handler

import (
	"context"

	"github.com/dofastted/claude2api/internal/service"
)

// BillingGate 是 billing 拦截的抽象，供 full/slim 分版实现。
type BillingGate interface {
	CheckEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, platform string) error
}
