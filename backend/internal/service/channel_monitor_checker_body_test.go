//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/pkg/claude"
)

// swapMonitorHTTPClient 临时替换 monitorHTTPClient 为不带 SSRF 校验的普通 client，
// 让 httptest (127.0.0.1) 能连通。测试结束后恢复。
func swapMonitorHTTPClient(t *testing.T) {
	t.Helper()
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })
}

// captureHandler 把每次收到的请求 body 和 headers 存起来，测试断言用。
type captureHandler struct {
	lastBody              map[string]any
	lastHeaders           http.Header
	respondText           string // 写到 Anthropic content[0].text 里（校验用）
	deriveChallengeAnswer bool
	status                int
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	defer func() { _ = r.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	h.lastBody = parsed

	if h.status == 0 {
		h.status = 200
	}
	text := h.respondText
	if h.deriveChallengeAnswer {
		text = answerFromAnthropicRequest(parsed)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)
	// 构造 Anthropic 格式的响应：content[0].text = text
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	})
}

func setupFakeAnthropic(t *testing.T, handler *captureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

type openAICaptureHandler struct {
	lastBody                  map[string]any
	lastHeaders               http.Header
	lastPath                  string
	status                    int
	responsesLeadingReasoning bool
}

func (h *openAICaptureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	h.lastPath = r.URL.Path
	defer func() { _ = r.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	h.lastBody = parsed

	if h.status == 0 {
		h.status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)

	answer := answerFromOpenAIRequest(parsed)
	if h.lastPath == providerOpenAIResponsesPath {
		output := []map[string]any{}
		if h.responsesLeadingReasoning {
			output = append(output, map[string]any{
				"type":    "reasoning",
				"summary": []any{},
			})
		}
		output = append(output, map[string]any{
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": answer},
			},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": output,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": answer}}},
	})
}

func setupFakeOpenAI(t *testing.T, handler *openAICaptureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func answerFromOpenAIRequest(body map[string]any) string {
	prompt, _ := body["input"].(string)
	if prompt == "" {
		if messages, ok := body["messages"].([]any); ok && len(messages) > 0 {
			if msg, ok := messages[0].(map[string]any); ok {
				prompt, _ = msg["content"].(string)
			}
		}
	}
	return answerFromChallengePrompt(prompt)
}

func answerFromAnthropicRequest(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return "0"
	}
	msg, _ := messages[0].(map[string]any)
	content := msg["content"]
	if text, ok := content.(string); ok {
		return answerFromChallengePrompt(text)
	}
	blocks, _ := content.([]any)
	if len(blocks) == 0 {
		return "0"
	}
	block, _ := blocks[0].(map[string]any)
	text, _ := block["text"].(string)
	return answerFromChallengePrompt(text)
}

var challengeQuestionRegex = regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?\nA:$`)

func answerFromChallengePrompt(prompt string) string {
	m := challengeQuestionRegex.FindStringSubmatch(prompt)
	if len(m) != 4 {
		return "0"
	}
	left, _ := strconv.Atoi(m[1])
	right, _ := strconv.Atoi(m[3])
	if m[2] == "+" {
		return strconv.Itoa(left + right)
	}
	return strconv.Itoa(left - right)
}

func TestRunCheckForModel_OffMode_PreservesDefaultBody(t *testing.T) {
	h := &captureHandler{respondText: "the answer is 42"}
	endpoint := setupFakeAnthropic(t, h)

	// 跑一次 off 模式（opts=nil），确认默认 body 行为未变
	_ = runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", nil)

	if h.lastBody["model"] != "claude-x" {
		t.Errorf("default body should contain model=claude-x, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("default body should contain messages")
	}
	if h.lastHeaders.Get("x-api-key") != "sk-fake" {
		t.Errorf("expected adapter's x-api-key header, got %q", h.lastHeaders.Get("x-api-key"))
	}
}

func TestRunCheckForModel_AnthropicDefaultUsesClaudeCodeSignedBodyAndHeaders(t *testing.T) {
	h := &captureHandler{deriveChallengeAnswer: true}
	endpoint := setupFakeAnthropic(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-sonnet-4-6", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("Anthropic Claude Code monitor request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastBody["model"] != "claude-sonnet-4-6" {
		t.Fatalf("expected model to be preserved, got %v", h.lastBody["model"])
	}
	system, _ := h.lastBody["system"].([]any)
	if len(system) < 3 {
		t.Fatalf("expected Claude Code system blocks, got %#v", h.lastBody["system"])
	}
	billingBlock, _ := system[0].(map[string]any)
	billingText, _ := billingBlock["text"].(string)
	if !strings.Contains(billingText, "x-anthropic-billing-header:") {
		t.Fatalf("expected billing header block, got %q", billingText)
	}
	if strings.Contains(billingText, "cch=00000") {
		t.Fatalf("expected signed cch, got %q", billingText)
	}
	if !regexp.MustCompile(`cch=[0-9a-f]{5};`).MatchString(billingText) {
		t.Fatalf("expected 5-hex cch, got %q", billingText)
	}
	metadata, _ := h.lastBody["metadata"].(map[string]any)
	if metadata["user_id"] == "" {
		t.Fatalf("expected metadata.user_id")
	}
	messages, _ := h.lastBody["messages"].([]any)
	msg, _ := messages[0].(map[string]any)
	blocks, _ := msg["content"].([]any)
	if len(blocks) == 0 {
		t.Fatalf("expected Claude Code text content block")
	}
	if h.lastHeaders.Get("User-Agent") == "" || !strings.Contains(h.lastHeaders.Get("User-Agent"), "claude-cli/") {
		t.Fatalf("expected Claude CLI User-Agent, got %q", h.lastHeaders.Get("User-Agent"))
	}
	if h.lastHeaders.Get("X-App") != "cli" {
		t.Fatalf("expected x-app=cli, got %q", h.lastHeaders.Get("X-App"))
	}
	if h.lastHeaders.Get("X-Stainless-Package-Version") != claude.DefaultHeaders["X-Stainless-Package-Version"] {
		t.Fatalf("expected SDK package version, got %q", h.lastHeaders.Get("X-Stainless-Package-Version"))
	}
	if h.lastHeaders.Get("anthropic-beta") != claude.APIKeyBetaHeader {
		t.Fatalf("expected API-key Claude beta header, got %q", h.lastHeaders.Get("anthropic-beta"))
	}
	if h.lastHeaders.Get("anthropic-version") != monitorAnthropicAPIVersion {
		t.Fatalf("expected anthropic-version, got %q", h.lastHeaders.Get("anthropic-version"))
	}
	if h.lastHeaders.Get("x-client-request-id") == "" {
		t.Fatalf("expected x-client-request-id")
	}
	parsed := ParseMetadataUserID(metadata["user_id"].(string))
	if parsed == nil || h.lastHeaders.Get("X-Claude-Code-Session-Id") != parsed.SessionID {
		t.Fatalf("expected X-Claude-Code-Session-Id to match metadata session, got header=%q metadata=%v", h.lastHeaders.Get("X-Claude-Code-Session-Id"), parsed)
	}
	if h.lastHeaders.Get("x-api-key") != "sk-fake" {
		t.Fatalf("expected x-api-key auth, got %q", h.lastHeaders.Get("x-api-key"))
	}
}

type anthropicRetryMessageProbeHandler struct {
	paths   []string
	headers []http.Header
}

func (h *anthropicRetryMessageProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.paths = append(h.paths, r.URL.Path)
	h.headers = append(h.headers, r.Header.Clone())
	defer func() { _ = r.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer sk-fake" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Your request body appears to have been tampered with. Please use our official distribution platform."}}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": []map[string]any{{"type": "text", "text": answerFromAnthropicRequest(parsed)}},
	})
}

func TestRunCheckForModel_AnthropicRetriesBearerMessageProbe(t *testing.T) {
	swapMonitorHTTPClient(t)
	h := &anthropicRetryMessageProbeHandler{}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, srv.URL, "sk-fake", "claude-sonnet-4-6", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("expected bearer message retry to pass, got status=%s message=%q", res.Status, res.Message)
	}
	if len(h.paths) != 2 {
		t.Fatalf("expected x-api-key then bearer message probe, got paths=%v", h.paths)
	}
	if h.headers[0].Get("x-api-key") != "sk-fake" {
		t.Fatalf("first probe should use x-api-key, got %q", h.headers[0].Get("x-api-key"))
	}
	if h.headers[1].Get("Authorization") != "Bearer sk-fake" {
		t.Fatalf("second probe should use bearer, got %q", h.headers[1].Get("Authorization"))
	}
	if h.headers[0].Get("anthropic-beta") != claude.APIKeyBetaHeader {
		t.Fatalf("first probe should use API-key beta, got %q", h.headers[0].Get("anthropic-beta"))
	}
	if h.headers[1].Get("anthropic-beta") != claude.DefaultBetaHeader {
		t.Fatalf("second probe should use Claude Code OAuth beta, got %q", h.headers[1].Get("anthropic-beta"))
	}
}

type anthropicGuardThenModelsHandler struct {
	paths   []string
	headers []http.Header
}

func (h *anthropicGuardThenModelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.paths = append(h.paths, r.URL.Path)
	h.headers = append(h.headers, r.Header.Clone())
	defer func() { _ = r.Body.Close() }()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/models" {
		if r.Header.Get("Authorization") != "Bearer sk-fake" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"missing bearer"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-6"}]}`))
		return
	}
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":{"message":"Your request body appears to have been tampered with. Please use our official distribution platform."}}`))
}

func TestRunCheckForModel_AnthropicGuardFallsBackToModelsEndpoint(t *testing.T) {
	swapMonitorHTTPClient(t)
	h := &anthropicGuardThenModelsHandler{}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, srv.URL, "sk-fake", "claude-sonnet-4-6", nil)

	if res.Status != MonitorStatusDegraded {
		t.Fatalf("expected degraded model-list fallback, got status=%s message=%q", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "/v1/models verified") {
		t.Fatalf("expected fallback message, got %q", res.Message)
	}
	if len(h.paths) != 4 {
		t.Fatalf("expected two message probes + two model-list auth attempts, got paths=%v", h.paths)
	}
	if h.paths[0] != providerAnthropicPath || h.paths[1] != providerAnthropicPath || h.paths[2] != "/v1/models" || h.paths[3] != "/v1/models" {
		t.Fatalf("unexpected paths: %v", h.paths)
	}
	if h.headers[0].Get("x-api-key") != "sk-fake" {
		t.Fatalf("first message probe should try x-api-key, got %q", h.headers[0].Get("x-api-key"))
	}
	if h.headers[1].Get("Authorization") != "Bearer sk-fake" {
		t.Fatalf("second message probe should try bearer, got %q", h.headers[1].Get("Authorization"))
	}
	if h.headers[2].Get("x-api-key") != "sk-fake" {
		t.Fatalf("first model-list fallback should try x-api-key, got %q", h.headers[2].Get("x-api-key"))
	}
	if h.headers[3].Get("Authorization") != "Bearer sk-fake" {
		t.Fatalf("second model-list fallback should try bearer, got %q", h.headers[3].Get("Authorization"))
	}
}

func TestBuildRequestBody_AnthropicMergeModeResignsBillingHeaderAfterOverride(t *testing.T) {
	body, err := buildRequestBody(providerAdapters[MonitorProviderAnthropic], MonitorProviderAnthropic, MonitorAPIModeChatCompletions, "claude-x", "Q: 1 + 1 = ?\nA:", &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]any{
			"max_tokens": float64(999),
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal body error = %v", err)
	}
	if parsed["max_tokens"] != float64(999) {
		t.Fatalf("expected merged max_tokens=999, got %v", parsed["max_tokens"])
	}
	got := regexp.MustCompile(`cch=([0-9a-f]{5});`).FindStringSubmatch(string(body))
	if len(got) != 2 {
		t.Fatalf("expected signed cch in body: %s", string(body))
	}
	restored := regexp.MustCompile(`cch=[0-9a-f]{5};`).ReplaceAllString(string(body), "cch=00000;")
	want := regexp.MustCompile(`cch=([0-9a-f]{5});`).FindStringSubmatch(string(signBillingHeaderCCH([]byte(restored))))
	if len(want) != 2 || got[1] != want[1] {
		t.Fatalf("cch not signed against final merged body: got %v want %v", got, want)
	}
}

func TestRunCheckForModel_OpenAI_DefaultChatRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("default chat request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIPath {
		t.Fatalf("expected chat completions path %q, got %q", providerOpenAIPath, h.lastPath)
	}
	if h.lastBody["model"] != "gpt-test" {
		t.Errorf("chat body should contain model=gpt-test, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("chat body should contain messages")
	}
	if _, ok := h.lastBody["instructions"]; ok {
		t.Error("chat body must not contain top-level instructions")
	}
	if h.lastBody["stream"] != false {
		t.Errorf("chat body should set stream=false, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer sk-openai" {
		t.Errorf("expected bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_OpenAIResponses_DefaultRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("default responses request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIResponsesPath {
		t.Fatalf("expected responses path %q, got %q", providerOpenAIResponsesPath, h.lastPath)
	}
	if h.lastBody["model"] != "gpt-test" {
		t.Errorf("responses body should contain model=gpt-test, got %v", h.lastBody["model"])
	}
	instructions, _ := h.lastBody["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		t.Error("responses body should contain non-empty instructions")
	}
	input, _ := h.lastBody["input"].(string)
	if strings.TrimSpace(input) == "" {
		t.Error("responses body should contain non-empty input")
	}
	if _, ok := h.lastBody["messages"]; ok {
		t.Error("responses body must not contain chat messages")
	}
	if h.lastBody["stream"] != false {
		t.Errorf("responses body should set stream=false, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer sk-openai" {
		t.Errorf("expected bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_OpenAIResponses_SkipsLeadingReasoningItem(t *testing.T) {
	h := &openAICaptureHandler{responsesLeadingReasoning: true}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-5.5", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("responses request should find text after leading reasoning item, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIResponsesPath {
		t.Fatalf("expected responses path %q, got %q", providerOpenAIResponsesPath, h.lastPath)
	}
}

func TestRunCheckForModel_OpenAIResponsesReplaceMissingInstructionsFailsLocally(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", &CheckOptions{
		APIMode:          MonitorAPIModeResponses,
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride: map[string]any{
			"model": "gpt-test",
			"input": "hello",
		},
	})

	if res.Status != MonitorStatusError {
		t.Fatalf("invalid responses replace body should fail locally as error, got status=%s", res.Status)
	}
	if !strings.Contains(res.Message, "instructions and input are required") {
		t.Errorf("expected local validation message about instructions/input, got %q", res.Message)
	}
	if h.lastPath != "" {
		t.Errorf("invalid replace body should fail before HTTP request, got path %q", h.lastPath)
	}
}

func TestRunCheckForModel_MergeMode_UserFieldsWinButDenyListProtects(t *testing.T) {
	h := &captureHandler{respondText: "the answer is 42"}
	endpoint := setupFakeAnthropic(t, h)

	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]any{
			"system":     "You are Claude Code...",
			"max_tokens": float64(999),   // 应该覆盖默认 50
			"model":      "hacked-model", // 应该被黑名单挡住，保留原 model
			"messages":   []any{},        // 同上，被挡
		},
		ExtraHeaders: map[string]string{
			"User-Agent":     "claude-cli/1.0",
			"Content-Length": "999", // 黑名单
			"x-custom":       "ok",
		},
	}
	_ = runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	if h.lastBody["system"] != "You are Claude Code..." {
		t.Errorf("merge mode should inject system, got %v", h.lastBody["system"])
	}
	// max_tokens 覆盖生效
	if mt, ok := h.lastBody["max_tokens"].(float64); !ok || mt != 999 {
		t.Errorf("merge mode should override max_tokens to 999, got %v", h.lastBody["max_tokens"])
	}
	// model 在黑名单 — 应该保留默认值
	if h.lastBody["model"] != "claude-x" {
		t.Errorf("model should be protected by deny list, got %v", h.lastBody["model"])
	}
	// messages 在黑名单 — 应该保留默认值（非空）
	msgs, _ := h.lastBody["messages"].([]any)
	if len(msgs) == 0 {
		t.Error("messages should be protected by deny list (kept default, non-empty)")
	}
	// header 合并
	if h.lastHeaders.Get("User-Agent") != "claude-cli/1.0" {
		t.Errorf("extra User-Agent should override, got %q", h.lastHeaders.Get("User-Agent"))
	}
	if h.lastHeaders.Get("x-custom") != "ok" {
		t.Errorf("extra custom header should be present, got %q", h.lastHeaders.Get("x-custom"))
	}
	// Content-Length 黑名单：会被 net/http 自动重算，但不应由用户的 "999" 决定。
	// 我们无法直接断言丢弃（http.Client 总会填上），只断言请求成功即可。
}

func TestRunCheckForModel_ReplaceMode_FullBodyUsedAndChallengeSkipped(t *testing.T) {
	// replace 模式下我们的 body 完全自定义，challenge 数学题不会出现在请求里，
	// 上游也不会回正确答案 — 但只要 2xx + 响应文本非空，就算 operational
	h := &captureHandler{respondText: "any non-empty text"}
	endpoint := setupFakeAnthropic(t, h)

	userBody := map[string]any{
		"model":      "user-forced-model",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens": float64(10),
		"system":     "You are someone else",
	}
	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride:     userBody,
	}
	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	// 请求 body = 用户提供的原样
	if h.lastBody["model"] != "user-forced-model" {
		t.Errorf("replace mode should use user's model, got %v", h.lastBody["model"])
	}
	if h.lastBody["system"] != "You are someone else" {
		t.Errorf("replace mode should use user's system, got %v", h.lastBody["system"])
	}
	// challenge 虽然没命中，但由于 replace 模式跳过 challenge 校验 + 响应非空 → operational
	if res.Status != MonitorStatusOperational {
		t.Errorf("replace mode with 2xx + non-empty text should be operational, got status=%s message=%q",
			res.Status, res.Message)
	}
}

func TestRunCheckForModel_ReplaceMode_EmptyResponseIsFailed(t *testing.T) {
	h := &captureHandler{respondText: ""} // 上游 200 但 content[0].text 为空
	endpoint := setupFakeAnthropic(t, h)

	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride:     map[string]any{"model": "x", "messages": []any{}},
	}
	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	if res.Status != MonitorStatusFailed {
		t.Errorf("replace mode with empty text should be failed, got status=%s", res.Status)
	}
	if !strings.Contains(res.Message, "replace-mode") {
		t.Errorf("failure message should hint replace-mode, got %q", res.Message)
	}
}
