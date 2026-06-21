package service

import (
	"context"
	"strings"
	"time"
)

// LocalInterceptUsageInput contains the minimal usage log snapshot for requests answered locally.
type LocalInterceptUsageInput struct {
	APIKey             *APIKey
	User               *User
	Model              string
	Stream             bool
	InterceptType      string
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	RequestPayloadHash string
	ChannelUsageFields ChannelUsageFields
}

func (s *GatewayService) RecordLocalInterceptUsage(ctx context.Context, input LocalInterceptUsageInput) {
	if s == nil || s.usageLogRepo == nil || input.APIKey == nil || input.User == nil {
		return
	}
	interceptType := strings.TrimSpace(input.InterceptType)
	usageLog := &UsageLog{
		UserID:            input.User.ID,
		APIKeyID:          input.APIKey.ID,
		AccountID:         0,
		RequestID:         resolveUsageBillingRequestID(ctx, ""),
		Model:             input.Model,
		RequestedModel:    firstNonEmptyLocalInterceptString(input.ChannelUsageFields.OriginalModel, input.Model),
		Stream:            input.Stream,
		RequestType:       RequestTypeFromLegacy(input.Stream, false),
		UserAgent:         optionalTrimmedStringPtr(input.UserAgent),
		IPAddress:         optionalTrimmedStringPtr(input.IPAddress),
		GroupID:           input.APIKey.GroupID,
		InboundEndpoint:   optionalTrimmedStringPtr(input.InboundEndpoint),
		UpstreamEndpoint:  optionalTrimmedStringPtr(input.UpstreamEndpoint),
		ChannelID:         optionalInt64Ptr(input.ChannelUsageFields.ChannelID),
		ModelMappingChain: optionalTrimmedStringPtr(input.ChannelUsageFields.ModelMappingChain),
		LocalIntercept:    true,
		InterceptType:     optionalTrimmedStringPtr(interceptType),
		BillingMode:       optionalTrimmedStringPtr(string(BillingModeToken)),
		RateMultiplier:    1,
		BillingType:       BillingTypeBalance,
		CreatedAt:         time.Now(),
	}
	writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway.local_intercept")
}

func firstNonEmptyLocalInterceptString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
