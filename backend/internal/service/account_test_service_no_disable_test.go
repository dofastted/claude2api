//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_Claude403ReportsErrorWithoutDisablingAccount(t *testing.T) {
	ctx, _ := newTestContext()
	resp := newJSONResponse(http.StatusForbidden, `{"error":{"message":"Your request body appears to have been tampered with."}}`)
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	account := &Account{
		ID:          44,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
	repo := &openAIAccountTestRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
	}
	svc := &AccountTestService{
		accountRepo:      repo,
		httpUpstream:     upstream,
		identityRegistry: clientidentity.NewRegistry(),
	}

	err := svc.TestAccountConnection(ctx, account.ID, "claude-sonnet-4-6", "", "")
	require.Error(t, err)
	require.Zero(t, repo.setErrorID)
	require.Empty(t, repo.setErrorMsg)
}
