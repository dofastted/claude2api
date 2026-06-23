package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type environmentProfileAccountRepo struct {
	AccountRepository
	mu      sync.Mutex
	account *Account
	updates []map[string]any
}

func (r *environmentProfileAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneEnvironmentProfileAccount(r.account), nil
}

func (r *environmentProfileAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := cloneEnvironmentProfileExtra(updates)
	r.updates = append(r.updates, copied)
	mergeAccountExtra(r.account, copied)
	return nil
}

func (r *environmentProfileAccountRepo) DeleteExtraKeys(_ context.Context, _ int64, keys []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range keys {
		delete(r.account.Extra, key)
	}
	return nil
}

func (r *environmentProfileAccountRepo) updateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updates)
}

func cloneEnvironmentProfileAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Extra = cloneEnvironmentProfileExtra(account.Extra)
	return &clone
}

func cloneEnvironmentProfileExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	data, err := json.Marshal(extra)
	if err != nil {
		panic(err)
	}
	out := make(map[string]any)
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}
func runConcurrentProfileRequests(t *testing.T, total int, fn func(int) string) map[string]struct{} {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan string, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results <- fn(index)
		}(i)
	}
	wg.Wait()
	close(results)
	unique := make(map[string]struct{})
	for value := range results {
		unique[value] = struct{}{}
	}
	return unique
}

func TestClaudeEnvironmentProfileCreatesDefaultOnce(t *testing.T) {
	account := &Account{
		ID:       101,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{accountRepo: repo}

	profile, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), account, http.Header{}, nil)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, ClaudeClientFamilyCodeCLI, profile.Family)
	require.Equal(t, claudeEnvironmentProfileSourceAutoDefault, profile.Source)
	require.NotEmpty(t, profile.ClientID)
	require.NotEmpty(t, profile.DeviceID)
	require.NotEmpty(t, profile.SessionSeed)
	stored, ok := repo.account.GetClaudeEnvironmentProfile()
	require.True(t, ok)
	require.Equal(t, profile.ClientID, stored.ClientID)
	require.True(t, repo.account.Extra[claudeEnvironmentProfileLockedKey].(bool))
	require.Equal(t, 1, repo.updateCount())

	again, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), account, http.Header{"User-Agent": []string{"Claude Desktop"}}, nil)
	require.NoError(t, err)
	require.Equal(t, profile.ClientID, again.ClientID)
	require.Equal(t, 1, repo.updateCount(), "existing profile must not be relearned from later requests")
}

func TestClaudeEnvironmentProfileLearnsDesktopFirstRequest(t *testing.T) {
	account := &Account{
		ID:       102,
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra: map[string]any{
			claudeEnvironmentAllowDesktopLearnKey: true,
		},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{accountRepo: repo}
	headers := http.Header{
		"User-Agent":                 []string{"Claude Desktop/1.0 Electron"},
		"X-App":                      []string{"claude-desktop"},
		"Anthropic-Client-Type":      []string{"desktop"},
		"Anthropic-Client-Id":        []string{"client-fixed"},
		"Anthropic-Client-Device-Id": []string{"device-fixed"},
		"Authorization":              []string{"Bearer secret"},
	}

	profile, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), account, headers, nil)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, ClaudeClientFamilyDesktop, profile.Family)
	require.Equal(t, "client-fixed", profile.ClientID)
	require.Equal(t, "device-fixed", profile.DeviceID)
	require.NotContains(t, profile.Headers, "authorization")
	require.Equal(t, 1, repo.updateCount())
}

func TestClaudeEnvironmentProfileDefaultsToFixedCodeCLI(t *testing.T) {
	account := &Account{
		ID:       104,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{accountRepo: repo}
	headers := http.Header{
		"User-Agent":                 []string{"Claude Desktop/1.0 Electron"},
		"X-App":                      []string{"claude-desktop"},
		"Anthropic-Client-Type":      []string{"desktop"},
		"Anthropic-Client-Id":        []string{"client-fixed"},
		"Anthropic-Client-Device-Id": []string{"device-fixed"},
	}

	profile, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), account, headers, nil)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, ClaudeClientFamilyCodeCLI, profile.Family)
	require.Equal(t, claudeEnvironmentProfileSourceAutoDefault, profile.Source)
	require.NotEqual(t, "client-fixed", profile.ClientID)
}

func TestCodexEnvironmentProfileDefaultsToFixedCLI(t *testing.T) {
	account := &Account{
		ID:       202,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &OpenAIGatewayService{accountRepo: repo}
	headers := http.Header{
		"User-Agent": []string{"Codex Desktop/1.0"},
		"originator": []string{"codex_chatgpt_desktop"},
	}

	profile, err := svc.getOrCreateCodexEnvironmentProfile(context.Background(), account, headers)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, CodexClientFamilyCLI, profile.Family)
	require.Equal(t, "auto_default", profile.Source)
}

func TestClaudeEnvironmentProfileSkipsGenericDesktopLearning(t *testing.T) {
	account := &Account{
		ID:       103,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			claudeEnvironmentProfileFamilyPreferenceKey: string(ClaudeClientFamilyDesktop),
		},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{accountRepo: repo}
	headers := http.Header{
		"User-Agent":            []string{"Go-http-client/1.1"},
		"X-App":                 []string{"claude-desktop"},
		"Anthropic-Client-Type": []string{"desktop"},
	}

	profile, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), account, headers, nil)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, ClaudeClientFamilyCodeCLI, profile.Family)
	require.Equal(t, claudeEnvironmentProfileSourceAutoDefault, profile.Source)
	require.NotContains(t, profile.UserAgent, "Go-http-client")
	require.Equal(t, 1, repo.updateCount())
}

func TestCodexEnvironmentProfileRejectsGenericUserAgentLearning(t *testing.T) {
	profile, learned, err := LearnCodexEnvironmentProfileFromHeaders(http.Header{
		"User-Agent": []string{"Go-http-client/1.1"},
		"originator": []string{"codex_cli_rs"},
	}, nil)
	require.NoError(t, err)
	require.False(t, learned)
	require.Nil(t, profile)
}

func TestCodexEnvironmentProfileLearnsCodexTUI(t *testing.T) {
	profile, learned, err := LearnCodexEnvironmentProfileFromHeaders(http.Header{
		"User-Agent": []string{"codex-tui/0.142.0 (Ubuntu 22.4.0; x86_64) xterm (codex-tui; 0.142.0)"},
		"originator": []string{"codex-tui"},
		"version":    []string{"0.142.0"},
	}, nil)
	require.NoError(t, err)
	require.True(t, learned)
	require.NotNil(t, profile)
	require.Equal(t, CodexClientFamilyCLI, profile.Family)
	require.Equal(t, "learned_verified_cli", profile.Source)
	require.Contains(t, profile.UserAgent, "codex-tui/0.142.0")
	require.Equal(t, "codex-tui", profile.Originator)
	require.Equal(t, "0.142.0", profile.Version)
}

func TestCodexEnvironmentProfileCreatesDefaultOnce(t *testing.T) {
	account := &Account{
		ID:       201,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &OpenAIGatewayService{accountRepo: repo}

	profile, err := svc.getOrCreateCodexEnvironmentProfile(context.Background(), account, http.Header{})
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, CodexClientFamilyCLI, profile.Family)
	require.Equal(t, "auto_default", profile.Source)
	require.NotEmpty(t, profile.SessionSeed)
	require.NotEmpty(t, profile.ConversationSeed)
	storedCodex, ok := repo.account.GetCodexEnvironmentProfile()
	require.True(t, ok)
	require.Equal(t, profile.SessionSeed, storedCodex.SessionSeed)
	require.True(t, repo.account.Extra[codexEnvironmentProfileLockedKey].(bool))
	require.Equal(t, 1, repo.updateCount())

	again, err := svc.getOrCreateCodexEnvironmentProfile(context.Background(), account, http.Header{"originator": []string{"codex_chatgpt_desktop"}})
	require.NoError(t, err)
	require.Equal(t, profile.SessionSeed, again.SessionSeed)
	require.Equal(t, 1, repo.updateCount(), "existing profile must not be relearned from later requests")
}

func TestClaudeEnvironmentProfileConcurrentDifferentEnvironmentsSingleAccount(t *testing.T) {
	account := &Account{ID: 301, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{}}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{accountRepo: repo}
	uniqueClients := runConcurrentProfileRequests(t, 100, func(index int) string {
		headers := http.Header{
			"User-Agent":            []string{"claude-cli/2.1.0"},
			"X-App":                 []string{"claude-code"},
			"Anthropic-Client-Type": []string{"cli"},
		}
		if index%2 == 1 {
			headers = http.Header{
				"User-Agent":                 []string{"Claude Desktop/1.0 Electron"},
				"X-App":                      []string{"claude-desktop"},
				"Anthropic-Client-Type":      []string{"desktop"},
				"Anthropic-Client-Id":        []string{"desktop-client"},
				"Anthropic-Client-Device-Id": []string{"desktop-device"},
			}
		}
		requestAccount := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type, Extra: map[string]any{}}
		profile, err := svc.getOrCreateClaudeEnvironmentProfile(context.Background(), requestAccount, headers, nil)
		require.NoError(t, err)
		require.NotNil(t, profile)
		return profile.ClientID
	})
	require.Len(t, uniqueClients, 1)
	require.Equal(t, 1, repo.updateCount(), "singleflight should persist one profile for one account under concurrent mixed clients")
}

func TestClaudeEnvironmentProfileConcurrentUsersShareTwentyCredentials(t *testing.T) {
	const credentialCount = 20
	accounts := make([]*Account, 0, credentialCount)
	repos := make([]*environmentProfileAccountRepo, 0, credentialCount)
	services := make([]*GatewayService, 0, credentialCount)
	for i := 0; i < credentialCount; i++ {
		account := &Account{ID: int64(500 + i), Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{}}
		repo := &environmentProfileAccountRepo{account: account}
		accounts = append(accounts, account)
		repos = append(repos, repo)
		services = append(services, &GatewayService{accountRepo: repo})
	}
	uniqueClients := runConcurrentProfileRequests(t, 100, func(index int) string {
		credentialIndex := index % credentialCount
		headers := http.Header{"User-Agent": []string{"claude-cli/2.1.0"}, "X-App": []string{"claude-code"}, "Anthropic-Client-Type": []string{"cli"}}
		if index%3 == 1 {
			headers = http.Header{"User-Agent": []string{"Claude Desktop/1.0 Electron"}, "X-App": []string{"claude-desktop"}, "Anthropic-Client-Type": []string{"desktop"}}
		}
		baseAccount := accounts[credentialIndex]
		requestAccount := &Account{ID: baseAccount.ID, Platform: baseAccount.Platform, Type: baseAccount.Type, Extra: map[string]any{}}
		profile, err := services[credentialIndex].getOrCreateClaudeEnvironmentProfile(context.Background(), requestAccount, headers, nil)
		require.NoError(t, err)
		require.NotNil(t, profile)
		return profile.ClientID
	})
	require.Len(t, uniqueClients, credentialCount)
	for _, repo := range repos {
		require.Equal(t, 1, repo.updateCount(), "each credential should persist one Claude profile despite concurrent users")
	}
}

func TestCodexEnvironmentProfileConcurrentUsersShareTwentyCredentials(t *testing.T) {
	const credentialCount = 20
	accounts := make([]*Account, 0, credentialCount)
	repos := make([]*environmentProfileAccountRepo, 0, credentialCount)
	services := make([]*OpenAIGatewayService, 0, credentialCount)
	for i := 0; i < credentialCount; i++ {
		account := &Account{ID: int64(400 + i), Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{}}
		repo := &environmentProfileAccountRepo{account: account}
		accounts = append(accounts, account)
		repos = append(repos, repo)
		services = append(services, &OpenAIGatewayService{accountRepo: repo})
	}
	uniqueSessions := runConcurrentProfileRequests(t, 100, func(index int) string {
		credentialIndex := index % credentialCount
		headers := http.Header{"originator": []string{"codex_cli_rs"}, "User-Agent": []string{"codex-cli/1.0.0"}}
		if index%3 == 1 {
			headers = http.Header{"originator": []string{"codex_chatgpt_desktop"}, "User-Agent": []string{"Codex Desktop/1.0"}}
		}
		baseAccount := accounts[credentialIndex]
		requestAccount := &Account{ID: baseAccount.ID, Platform: baseAccount.Platform, Type: baseAccount.Type, Extra: map[string]any{}}
		profile, err := services[credentialIndex].getOrCreateCodexEnvironmentProfile(context.Background(), requestAccount, headers)
		require.NoError(t, err)
		require.NotNil(t, profile)
		return profile.SessionSeed
	})
	require.Len(t, uniqueSessions, credentialCount)
	for _, repo := range repos {
		require.Equal(t, 1, repo.updateCount(), "each credential should persist one profile despite concurrent users")
	}
}

func TestAdminResetEnvironmentProfileDeletesPoolKeys(t *testing.T) {
	claudeAccount := &Account{
		ID:       3010,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			claudeEnvironmentProfileKey:     map[string]any{"family": "code_cli"},
			claudeEnvironmentProfilePoolKey: map[string]any{"version": 1, "capacity": 1},
		},
	}
	claudeRepo := &environmentProfileAccountRepo{account: claudeAccount}
	claudeSvc := &adminServiceImpl{accountRepo: claudeRepo}
	updatedClaude, err := claudeSvc.ResetClaudeEnvironmentProfile(context.Background(), claudeAccount.ID)
	require.NoError(t, err)
	require.NotContains(t, updatedClaude.Extra, claudeEnvironmentProfileKey)
	require.NotContains(t, updatedClaude.Extra, claudeEnvironmentProfilePoolKey)

	codexAccount := &Account{
		ID:       3011,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			codexEnvironmentProfileKey:     map[string]any{"family": "cli"},
			codexEnvironmentProfilePoolKey: map[string]any{"version": 1, "capacity": 1},
		},
	}
	codexRepo := &environmentProfileAccountRepo{account: codexAccount}
	codexSvc := &adminServiceImpl{accountRepo: codexRepo}
	updatedCodex, err := codexSvc.ResetCodexEnvironmentProfile(context.Background(), codexAccount.ID)
	require.NoError(t, err)
	require.NotContains(t, updatedCodex.Extra, codexEnvironmentProfileKey)
	require.NotContains(t, updatedCodex.Extra, codexEnvironmentProfilePoolKey)
}

func TestCodexEnvironmentProfileApplyPreservesCompatBridgeBetaAndOriginatorRemoval(t *testing.T) {
	account := &Account{ID: 202, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	profile := &CodexEnvironmentProfile{
		Family:           CodexClientFamilyCLI,
		Source:           "admin",
		UserAgent:        "codex-cli/1.2.3",
		Originator:       "codex_cli_rs",
		SessionSeed:      "session-seed",
		ConversationSeed: "conversation-seed",
		TLSProfile:       "codex-cli-default",
	}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "client")

	applyCodexEnvironmentProfile(req, account, profile, CodexProfileApplyOptions{APIKeyID: 7, PromptCacheKey: "prompt", CompatMessagesBridge: true})

	require.Equal(t, "codex-cli/1.2.3", req.Header.Get("User-Agent"))
	require.Empty(t, req.Header.Get("OpenAI-Beta"))
	require.Empty(t, req.Header.Get("originator"))
	require.NotEmpty(t, req.Header.Get("session_id"))
	require.NotEmpty(t, req.Header.Get("conversation_id"))
}
