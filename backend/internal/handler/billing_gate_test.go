package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type testBillingGate struct{}

func newTestBillingGate() BillingGate {
	return testBillingGate{}
}

func (testBillingGate) CheckEligibility(_ context.Context, _ *service.User, _ *service.APIKey, _ *service.Group, _ *service.UserSubscription, _ string) error {
	return nil
}
