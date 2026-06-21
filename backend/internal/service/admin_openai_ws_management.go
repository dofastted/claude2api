package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var openAIWSAllExtraUpdates = map[string]any{
	"openai_oauth_responses_websockets_v2_enabled":  true,
	"openai_oauth_responses_websockets_v2_mode":     OpenAIWSIngressModeCtxPool,
	"openai_apikey_responses_websockets_v2_enabled": true,
	"openai_apikey_responses_websockets_v2_mode":    OpenAIWSIngressModeCtxPool,
	"responses_websockets_v2_enabled":               true,
	"openai_ws_enabled":                             true,
	"openai_ws_allow_store_recovery":                true,
	"openai_ws_force_http":                          false,
}

var openAIWSAllExtraKeys = []string{
	"openai_oauth_responses_websockets_v2_enabled",
	"openai_oauth_responses_websockets_v2_mode",
	"openai_apikey_responses_websockets_v2_enabled",
	"openai_apikey_responses_websockets_v2_mode",
	"responses_websockets_v2_enabled",
	"openai_ws_enabled",
	"openai_ws_allow_store_recovery",
	"openai_ws_force_http",
}

type accountExtraKeyDeleter interface {
	DeleteExtraKeys(ctx context.Context, id int64, keys []string) error
}

// EnableAllOpenAIWS writes the account-level OpenAI WS fields to the target enabled state.
func (s *adminServiceImpl) EnableAllOpenAIWS(ctx context.Context, accountID int64) error {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil || !account.IsOpenAI() {
		return infraerrors.BadRequest("ACCOUNT_NOT_OPENAI", "account is not OpenAI platform")
	}
	if !isGlobalOpenAIResponsesWebSocketV2Enabled(s.cfg) {
		return infraerrors.BadRequest("OPENAI_WS_GLOBAL_DISABLED", "global gateway.openai_ws.responses_websockets_v2 is disabled, cannot enable for account")
	}

	updates := make(map[string]any, len(openAIWSAllExtraUpdates))
	for key, value := range openAIWSAllExtraUpdates {
		updates[key] = value
	}
	return s.accountRepo.UpdateExtra(ctx, accountID, updates)
}

// ResetOpenAIWS deletes the account-level OpenAI WS override fields so defaults apply again.
func (s *adminServiceImpl) ResetOpenAIWS(ctx context.Context, accountID int64) error {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil || !account.IsOpenAI() {
		return infraerrors.BadRequest("ACCOUNT_NOT_OPENAI", "account is not OpenAI platform")
	}
	deleter, ok := s.accountRepo.(accountExtraKeyDeleter)
	if !ok {
		return infraerrors.InternalServer("ACCOUNT_EXTRA_DELETE_UNSUPPORTED", "account repository does not support deleting extra keys")
	}
	return deleter.DeleteExtraKeys(ctx, accountID, openAIWSAllExtraKeys)
}

func isGlobalOpenAIResponsesWebSocketV2Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2
}
