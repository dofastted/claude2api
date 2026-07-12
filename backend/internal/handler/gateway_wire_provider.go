package handler

import (
	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/service"
)

func ProvideGatewayHandler(
	gatewayService *service.GatewayService,
	geminiCompatService *service.GeminiMessagesCompatService,
	antigravityGatewayService *service.AntigravityGatewayService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	billingGate BillingGate,
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	userMsgQueueService *service.UserMessageQueueService,
	cfg *config.Config,
	settingService *service.SettingService,
	resolver *service.ClaudeOAuthSessionResolver,
) *GatewayHandler {
	h := NewGatewayHandler(
		gatewayService,
		geminiCompatService,
		antigravityGatewayService,
		userService,
		concurrencyService,
		billingCacheService,
		billingGate,
		usageService,
		apiKeyService,
		usageRecordWorkerPool,
		errorPassthroughService,
		contentModerationService,
		userMsgQueueService,
		cfg,
		settingService,
	)
	h.SetClaudeOAuthSessionResolver(resolver)
	return h
}
