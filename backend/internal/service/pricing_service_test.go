package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/stretchr/testify/require"
)

type pricingRemoteClientStub struct {
	hash           string
	body           []byte
	fetchJSONCalls int
}

func (c *pricingRemoteClientStub) FetchPricingJSON(_ context.Context, _ string) ([]byte, error) {
	c.fetchJSONCalls++
	return c.body, nil
}

func (c *pricingRemoteClientStub) FetchHashText(_ context.Context, _ string) (string, error) {
	return c.hash, nil
}

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0.0000025,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 3e-5, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 5e-7, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &LiteLLMModelPricing{InputCostPerToken: 1}
	gpt53Pricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Same(t, sparkPricing, got)
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)

	require.True(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_Gpt54UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": &LiteLLMModelPricing{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2.5e-7, got.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, got.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.LongContextOutputCostMultiplier, 1e-12)
}

func TestGetModelPricing_OpenAICompactAliasUsesStaticFallback(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("openai/gpt5.5")
	require.NotNil(t, got)
	require.InDelta(t, 5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 3e-5, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 12.5e-6, got.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 75e-6, got.OutputCostPerTokenPriority, 1e-12)
}

func TestApplyOfficialPricingOverridesAddsNewClaudeModelsAndCorrectsGPT55Priority(t *testing.T) {
	svc := &PricingService{}
	pricingData := map[string]*LiteLLMModelPricing{
		"gpt-5.5": {
			InputCostPerToken:               5e-6,
			OutputCostPerToken:              30e-6,
			CacheReadInputTokenCost:         0.5e-6,
			InputCostPerTokenPriority:       10e-6,
			OutputCostPerTokenPriority:      60e-6,
			CacheReadInputTokenCostPriority: 1e-6,
			LiteLLMProvider:                 "openai",
			Mode:                            "chat",
		},
	}

	changed := svc.applyOfficialPricingOverrides(pricingData)
	require.GreaterOrEqual(t, changed, 2)

	fable5 := pricingData["claude-fable-5"]
	require.NotNil(t, fable5)
	require.InDelta(t, 10e-6, fable5.InputCostPerToken, 1e-12)
	require.InDelta(t, 50e-6, fable5.OutputCostPerToken, 1e-12)
	require.InDelta(t, 12.5e-6, fable5.CacheCreationInputTokenCost, 1e-12)
	require.InDelta(t, 20e-6, fable5.CacheCreationInputTokenCostAbove1hr, 1e-12)
	require.InDelta(t, 1e-6, fable5.CacheReadInputTokenCost, 1e-12)

	gpt55 := pricingData["gpt-5.5"]
	require.InDelta(t, 12.5e-6, gpt55.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 75e-6, gpt55.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 1.25e-6, gpt55.CacheReadInputTokenCostPriority, 1e-12)
}

func TestSyncWithRemoteRevalidatesOfficialPricesWhenHashUnchanged(t *testing.T) {
	remote := &pricingRemoteClientStub{hash: "same"}
	svc := NewPricingService(&config.Config{
		Pricing: config.PricingConfig{HashURL: "https://example.com/model_prices.sha256"},
	}, remote)
	svc.localHash = "same"
	svc.pricingData = map[string]*LiteLLMModelPricing{
		"gpt-5.5": {
			InputCostPerToken:               5e-6,
			OutputCostPerToken:              30e-6,
			CacheReadInputTokenCost:         0.5e-6,
			InputCostPerTokenPriority:       10e-6,
			OutputCostPerTokenPriority:      60e-6,
			CacheReadInputTokenCostPriority: 1e-6,
			LiteLLMProvider:                 "openai",
			Mode:                            "chat",
		},
	}

	require.NoError(t, svc.syncWithRemote())
	require.Equal(t, 0, remote.fetchJSONCalls)

	got := svc.GetModelPricing("gpt-5.5")
	require.NotNil(t, got)
	require.InDelta(t, 12.5e-6, got.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 75e-6, got.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 1.25e-6, got.CacheReadInputTokenCostPriority, 1e-12)
}

func TestCurrentOfficialModelPricingOverrides_Sonnet5IntroWindow(t *testing.T) {
	before := currentOfficialModelPricingOverrides(time.Date(2026, 8, 31, 23, 59, 0, 0, time.UTC))["claude-sonnet-5"]
	require.NotNil(t, before)
	require.InDelta(t, 2e-6, before.InputCostPerToken, 1e-12)
	require.InDelta(t, 10e-6, before.OutputCostPerToken, 1e-12)

	after := currentOfficialModelPricingOverrides(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))["claude-sonnet-5"]
	require.NotNil(t, after)
	require.InDelta(t, 3e-6, after.InputCostPerToken, 1e-12)
	require.InDelta(t, 15e-6, after.OutputCostPerToken, 1e-12)
}

func TestDefaultPricingIncludesManualFallbackModels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	svc := &PricingService{}
	pricingData, err := svc.parsePricingData(data)
	require.NoError(t, err)
	svc.pricingData = pricingData

	codexAutoReview := svc.GetModelPricing("codex-auto-review")
	require.NotNil(t, codexAutoReview)
	require.InDelta(t, 5e-6, codexAutoReview.InputCostPerToken, 1e-12)
	require.InDelta(t, 3e-5, codexAutoReview.OutputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-5, codexAutoReview.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 7.5e-5, codexAutoReview.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 1.25e-6, codexAutoReview.CacheReadInputTokenCostPriority, 1e-12)

	sonnet5 := svc.GetModelPricing("claude-sonnet-5")
	require.NotNil(t, sonnet5)
	require.InDelta(t, 2e-6, sonnet5.InputCostPerToken, 1e-12)
	require.InDelta(t, 10e-6, sonnet5.OutputCostPerToken, 1e-12)

	fable5 := svc.GetModelPricing("claude-fable-5")
	require.NotNil(t, fable5)
	require.InDelta(t, 10e-6, fable5.InputCostPerToken, 1e-12)
	require.InDelta(t, 50e-6, fable5.OutputCostPerToken, 1e-12)
}

func TestGetModelPricing_Gpt54MiniUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-mini")
	require.NotNil(t, got)
	require.InDelta(t, 7.5e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.5e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 7.5e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54NanoUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-nano")
	require.NotNil(t, got)
	require.InDelta(t, 2e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &LiteLLMModelPricing{InputCostPerToken: 3}
	textPricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	}

	got := svc.GetModelPricing("gpt-image-3")
	require.Same(t, imagePricing, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                 2.5e-6,
			"input_cost_per_token_priority":        5e-6,
			"output_cost_per_token":                15e-6,
			"output_cost_per_token_priority":       30e-6,
			"cache_read_input_token_cost":          0.25e-6,
			"cache_read_input_token_cost_priority": 0.5e-6,
			"supports_service_tier":                true,
			"supports_prompt_caching":              true,
			"litellm_provider":                     "openai",
			"mode":                                 "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	svc := &PricingService{}
	pricingMap, err := svc.parsePricingData(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 2.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 15e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000025, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000005, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000015, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00003, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000025, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000005, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

// ---------------------------------------------------------------------------
// ListModelNamesByProvider
// ---------------------------------------------------------------------------

func TestListModelNamesByProvider_ReturnsMatchingModels(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"claude-opus-4-5-20251101": {LiteLLMProvider: "anthropic", InputCostPerToken: 1.5e-5},
			"claude-sonnet-4-5":        {LiteLLMProvider: "anthropic", InputCostPerToken: 3e-6},
			"gpt-4o":                   {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
			"gemini-2.5-pro":           {LiteLLMProvider: "google", InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.ElementsMatch(t, []string{"claude-opus-4-5-20251101", "claude-sonnet-4-5"}, got)
	// Must be sorted
	require.Equal(t, "claude-opus-4-5-20251101", got[0])
	require.Equal(t, "claude-sonnet-4-5", got[1])
}

func TestListModelNamesByProvider_CaseInsensitive(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "OpenAI", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.Equal(t, []string{"gpt-4o"}, got)

	got2 := svc.ListModelNamesByProvider("OPENAI")
	require.Equal(t, []string{"gpt-4o"}, got2)
}

func TestListModelNamesByProvider_NoMatch(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestListModelNamesByProvider_EmptyCatalog(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.NotNil(t, got)
	require.Empty(t, got)
}
