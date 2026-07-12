package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ClaudeOAuthMigrationReason string

const (
	ClaudeOAuthMigrationCredentialExhausted ClaudeOAuthMigrationReason = "credential_exhausted"
	ClaudeOAuthMigrationCredentialRevoked   ClaudeOAuthMigrationReason = "credential_revoked"
)

type ClaudeOAuthMigrationClassification struct {
	Reason          ClaudeOAuthMigrationReason
	CredentialState string
	UpstreamCode    string
}

type ClaudeOAuthMigrationResult struct {
	Classification ClaudeOAuthMigrationClassification
	Binding        *ClaudeOAuthBinding
	Migrated       bool
}

type ClaudeOAuthMigrationManager struct {
	selector     *ClaudeOAuthPoolSelector
	poolRepo     OAuthPoolRepository
	bindingStore ClaudeOAuthBindingStore
}

var claudeOAuthRevokedCodes = map[string]struct{}{
	"invalid_grant":       {},
	"oauth_token_revoked": {},
	"token_revoked":       {},
	"credential_revoked":  {},
	"account_disabled":    {},
}

var claudeOAuthExhaustedCodes = map[string]struct{}{
	"credit_balance_too_low":      {},
	"organization_quota_exceeded": {},
	"quota_exhausted":             {},
	"usage_limit_exceeded":        {},
}

func NewClaudeOAuthMigrationManager(selector *ClaudeOAuthPoolSelector, poolRepo OAuthPoolRepository, bindingStore ClaudeOAuthBindingStore) (*ClaudeOAuthMigrationManager, error) {
	if selector == nil || poolRepo == nil || bindingStore == nil {
		return nil, fmt.Errorf("build claude oauth migration manager: selector, pool repository and binding store are required")
	}
	return &ClaudeOAuthMigrationManager{selector: selector, poolRepo: poolRepo, bindingStore: bindingStore}, nil
}

func ClassifyClaudeOAuthCredentialFailure(failure *UpstreamFailoverError) (ClaudeOAuthMigrationClassification, bool) {
	if failure == nil || len(failure.ResponseBody) == 0 {
		return ClaudeOAuthMigrationClassification{}, false
	}
	codes := claudeOAuthStructuredErrorCodes(failure.ResponseBody)
	switch failure.StatusCode {
	case 400, 401, 403:
		for _, code := range codes {
			if _, ok := claudeOAuthRevokedCodes[code]; ok {
				return ClaudeOAuthMigrationClassification{
					Reason: ClaudeOAuthMigrationCredentialRevoked, CredentialState: OAuthPoolCredentialRevoked, UpstreamCode: code,
				}, true
			}
		}
	case 402, 429:
		for _, code := range codes {
			if _, ok := claudeOAuthExhaustedCodes[code]; ok {
				return ClaudeOAuthMigrationClassification{
					Reason: ClaudeOAuthMigrationCredentialExhausted, CredentialState: OAuthPoolCredentialExhausted, UpstreamCode: code,
				}, true
			}
		}
	}
	return ClaudeOAuthMigrationClassification{}, false
}

func (m *ClaudeOAuthMigrationManager) MigrateOnFailure(ctx context.Context, selection *ClaudeOAuthSelection, failure *UpstreamFailoverError) (*ClaudeOAuthMigrationResult, error) {
	classification, shouldMigrate := ClassifyClaudeOAuthCredentialFailure(failure)
	if !shouldMigrate {
		return &ClaudeOAuthMigrationResult{}, nil
	}
	if selection == nil || selection.Pool == nil || selection.Binding == nil || selection.Account == nil {
		return nil, fmt.Errorf("migrate claude oauth binding: selection is incomplete")
	}
	memberships, err := m.poolRepo.ListCredentials(ctx, selection.Pool.ID)
	if err != nil {
		return nil, err
	}
	var failedMembership *OAuthPoolCredential
	for index := range memberships {
		if memberships[index].AccountID == selection.Account.ID {
			membership := memberships[index]
			failedMembership = &membership
			break
		}
	}
	if failedMembership == nil {
		return nil, fmt.Errorf("%w: failed account is not a pool member", ErrOAuthPoolCredentialInvalid)
	}
	failedMembership.State = classification.CredentialState
	failedMembership.CooldownUntil = nil
	if err := m.poolRepo.UpdateCredential(ctx, failedMembership); err != nil {
		return nil, err
	}

	ranked, err := m.selector.RankCompatibleCredentials(ctx, selection.Pool, selection.Binding.BindingHash)
	if err != nil {
		return nil, err
	}
	var nextAccountID int64
	for _, candidate := range ranked {
		if candidate.Account.ID != selection.Account.ID {
			nextAccountID = candidate.Account.ID
			break
		}
	}
	if nextAccountID == 0 {
		return nil, ErrClaudeOAuthNoCompatibleCredential
	}
	binding, err := m.bindingStore.MigrateBindingCAS(ctx, ClaudeOAuthBindingMigration{
		PoolID:            selection.Binding.PoolID,
		BindingHash:       selection.Binding.BindingHash,
		ExpectedAccountID: selection.Binding.AccountID,
		ExpectedEpoch:     selection.Binding.Epoch,
		NewAccountID:      nextAccountID,
	})
	if err != nil {
		return nil, err
	}
	return &ClaudeOAuthMigrationResult{Classification: classification, Binding: binding, Migrated: true}, nil
}

func claudeOAuthStructuredErrorCodes(body []byte) []string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var codes []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				normalizedKey := strings.ToLower(strings.TrimSpace(key))
				if normalizedKey == "code" || normalizedKey == "type" || normalizedKey == "error_code" {
					if text, ok := child.(string); ok {
						code := strings.ToLower(strings.TrimSpace(text))
						if code != "" {
							if _, exists := seen[code]; !exists {
								seen[code] = struct{}{}
								codes = append(codes, code)
							}
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(payload)
	return codes
}
