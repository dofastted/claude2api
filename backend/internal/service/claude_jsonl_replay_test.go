package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type claudeJSONLReplayCase struct {
	name  string
	body  []byte
	turns int
}

type claudeJSONLEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeReplayMockUpstream struct {
	requests []*http.Request
	bodies   [][]byte
}

func (u *claudeReplayMockUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

func (u *claudeReplayMockUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(body, "messages").IsArray() {
		return nil, fmt.Errorf("replayed Claude request missing messages")
	}
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, append([]byte(nil), body...))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"msg_mock","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-sonnet-4-6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
	}, nil
}

func TestClaudeJSONLReplayMockEnvironmentBuildsRequests(t *testing.T) {
	cases := loadClaudeJSONLReplayCases(t, 6)
	if len(cases) == 0 {
		t.Skip("no local Claude JSONL replay data found; set CLAUDE_JSONL_REPLAY_DIR to enable")
	}

	account := &Account{
		ID:          9101,
		Name:        "claude-jsonl-replay",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 5,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       map[string]any{},
	}
	repo := &environmentProfileAccountRepo{account: account}
	upstream := &claudeReplayMockUpstream{}
	svc := &GatewayService{
		cfg:                                &config.Config{},
		accountRepo:                        repo,
		identityService:                    NewIdentityService(&identityCacheStub{}, nil),
		httpUpstream:                       upstream,
		claudeEnvironmentProfileSlotLeases: NewEnvironmentProfileSlotLeaseManager(),
	}

	for i, replay := range cases {
		t.Run(replay.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptestNewClaudeReplayRequest(replay.body)

			req, wireBody, err := svc.buildUpstreamRequest(
				context.Background(), c, account, replay.body,
				"oauth-token", "oauth", "claude-sonnet-4-6", i%2 == 1, true,
			)
			require.NoError(t, err)
			require.NotNil(t, req)
			require.NotEmpty(t, wireBody)
			require.Equal(t, "https://api.anthropic.com/v1/messages?beta=true", req.URL.String())
			require.Equal(t, "Bearer oauth-token", getHeaderRaw(req.Header, "authorization"))
			require.Equal(t, "claude-code", getHeaderRaw(req.Header, "X-App"))
			require.NotEmpty(t, getHeaderRaw(req.Header, "User-Agent"))
			require.NotEmpty(t, getHeaderRaw(req.Header, "X-Stainless-OS"))
			require.True(t, gjson.GetBytes(wireBody, "messages").IsArray())
			require.GreaterOrEqual(t, len(gjson.GetBytes(wireBody, "messages").Array()), 1)

			resp, err := upstream.DoWithTLS(req, "", account.ID, account.Concurrency, nil)
			require.NoError(t, err)
			wrapResponseBodyWithEnvironmentProfileLease(req, resp)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
		})
	}

	pool, err := DecodeClaudeEnvironmentProfilePool(repo.account.Extra[claudeEnvironmentProfilePoolKey])
	require.NoError(t, err)
	require.NotNil(t, pool)
	// v2 schema：固定 3 OS 槽位冻结（windows/macos/linux），容量与并发解耦。
	require.True(t, pool.IsV2(), "pool should be schema v2")
	require.Equal(t, 3, pool.Capacity)
	require.Len(t, pool.Slots, 3)
	require.Equal(t, 0, svc.claudeEnvironmentProfileSlotLeases.activeCount())
	require.Len(t, upstream.requests, len(cases))
}

func loadClaudeJSONLReplayCases(t *testing.T, limit int) []claudeJSONLReplayCase {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv("CLAUDE_JSONL_REPLAY_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".claude", "projects", "-mnt-x-project-claude2api")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return nil
	}
	sort.Strings(files)

	cases := make([]claudeJSONLReplayCase, 0, limit)
	for _, file := range files {
		if len(cases) >= limit {
			break
		}
		loaded := loadClaudeJSONLReplayCasesFromFile(t, file, limit-len(cases))
		cases = append(cases, loaded...)
	}
	return cases
}

func loadClaudeJSONLReplayCasesFromFile(t *testing.T, file string, limit int) []claudeJSONLReplayCase {
	t.Helper()
	fh, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer func() { _ = fh.Close() }()

	messages := make([]map[string]any, 0, 16)
	cases := make([]claudeJSONLReplayCase, 0, limit)
	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if len(cases) >= limit {
			break
		}
		var entry claudeJSONLEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil || entry.Message.Role == "" || len(entry.Message.Content) == 0 {
			continue
		}
		role := strings.TrimSpace(entry.Message.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := normalizeClaudeJSONLContent(entry.Message.Content)
		if content == nil {
			continue
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
		if role != "user" {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"model":      "claude-sonnet-4-6",
			"max_tokens": 64,
			"stream":     false,
			"system": []map[string]any{{
				"type": "text",
				"text": "Replay a locally recorded Claude JSONL conversation shape against the gateway mock.",
			}},
			"messages": tailClaudeReplayMessages(messages, 12),
		})
		if err != nil {
			continue
		}
		cases = append(cases, claudeJSONLReplayCase{
			name:  fmt.Sprintf("%s:%d", filepath.Base(file), lineNo),
			body:  body,
			turns: len(messages),
		})
	}
	return cases
}

func normalizeClaudeJSONLContent(raw json.RawMessage) any {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = trimReplayText(text)
		if text == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": text}}
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		out := make([]map[string]any, 0, len(blocks))
		for _, block := range blocks {
			kind, _ := block["type"].(string)
			switch kind {
			case "text":
				if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
					out = append(out, map[string]any{"type": "text", "text": trimReplayText(text)})
				}
			case "thinking":
				if thinking, _ := block["thinking"].(string); strings.TrimSpace(thinking) != "" {
					out = append(out, map[string]any{"type": "thinking", "thinking": trimReplayText(thinking), "signature": "mock-signature"})
				}
			case "tool_use":
				out = append(out, map[string]any{
					"type":  "tool_use",
					"id":    stringValueOrDefault(block["id"], "toolu_mock"),
					"name":  stringValueOrDefault(block["name"], "mock_tool"),
					"input": block["input"],
				})
			case "tool_result":
				out = append(out, map[string]any{
					"type":        "tool_result",
					"tool_use_id": stringValueOrDefault(block["tool_use_id"], "toolu_mock"),
					"content":     trimReplayText(fmt.Sprint(block["content"])),
				})
			}
			if len(out) >= 8 {
				break
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []map[string]any{{"type": "text", "text": trimReplayText(string(raw))}}
}

func tailClaudeReplayMessages(messages []map[string]any, limit int) []map[string]any {
	if len(messages) <= limit {
		return append([]map[string]any(nil), messages...)
	}
	return append([]map[string]any(nil), messages[len(messages)-limit:]...)
}

func trimReplayText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 2048 {
		return value
	}
	return value[:2048] + "...[truncated]"
}

func stringValueOrDefault(value any, fallback string) string {
	text, _ := value.(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback
	}
	return text
}

func httptestNewClaudeReplayRequest(body []byte) *http.Request {
	req, _ := http.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("Anthropic-Client-Type", "cli")
	req.Header.Set("X-Stainless-OS", "linux")
	req.Header.Set("X-Stainless-Arch", "x64")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Runtime-Version", "v22.0.0")
	return req
}
