package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dofastted/claude2api/internal/config"
)

const (
	claudeCLIRuntimeProbePrompt = "hi"
	claudeCLIRuntimeProbeOKText = "Claude Code CLI runtime probe completed."
)

type claudeCLIRuntimeProbeResult struct {
	OK      bool
	Message string
}

type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 || b.buf.Len() >= b.max {
		return len(p), nil
	}
	remaining := b.max - b.buf.Len()
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func shouldUseClaudeCLIRuntimeProbe(account *Account, cfg *config.Config) bool {
	if account == nil || cfg == nil || !cfg.ClaudeCLIRuntimeProbe.Enabled {
		return false
	}
	if account.IsAnthropicAPIKeyPassthroughEnabled() {
		return true
	}
	return isPackyAPIAnthropicAccount(account)
}

func isPackyAPIAnthropicAccount(account *Account) bool {
	if account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return false
	}
	baseURL, _ := account.Credentials["base_url"].(string)
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return false
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return hostname == "packyapi.com" || strings.HasSuffix(hostname, ".packyapi.com")
}

func (s *AccountTestService) runClaudeCLIRuntimeProbe(ctx context.Context, account *Account, baseURL string, apiKey string, modelID string, proxyURL string) claudeCLIRuntimeProbeResult {
	if !shouldUseClaudeCLIRuntimeProbe(account, s.cfg) {
		return claudeCLIRuntimeProbeResult{OK: false, Message: "Claude Code CLI runtime probe is disabled"}
	}
	cfg := s.cfg.ClaudeCLIRuntimeProbe
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	maxOutputBytes := cfg.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = 4096
	}
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		binaryPath = "claude"
	}
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	modelID = strings.TrimSpace(modelID)
	if baseURL == "" || apiKey == "" || modelID == "" {
		return claudeCLIRuntimeProbeResult{OK: false, Message: "Claude Code CLI runtime probe missing required input"}
	}

	tmpHome, err := os.MkdirTemp("", "claude2api-cli-probe-*")
	if err != nil {
		return claudeCLIRuntimeProbeResult{OK: false, Message: "create temporary CLI home: " + err.Error()}
	}
	defer func() { _ = os.RemoveAll(tmpHome) }()

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binaryPath, "-p", claudeCLIRuntimeProbePrompt, "--model", modelID, "--max-turns", "1")
	cmd.Dir = tmpHome
	cmd.Stdin = strings.NewReader("")
	cmd.Env = claudeCLIRuntimeProbeEnv(os.Environ(), tmpHome, baseURL, apiKey, cfg.AttributionMode, proxyURL)
	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.max = maxOutputBytes
	stderr.max = maxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := formatClaudeCLIRuntimeProbeError(err, stdout.String(), stderr.String(), apiKey)
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			message = fmt.Sprintf("Claude Code CLI runtime probe timed out after %s", timeout)
		}
		return claudeCLIRuntimeProbeResult{OK: false, Message: message}
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return claudeCLIRuntimeProbeResult{OK: false, Message: "Claude Code CLI runtime probe returned empty output"}
	}
	return claudeCLIRuntimeProbeResult{OK: true, Message: claudeCLIRuntimeProbeOKText}
}

func claudeCLIRuntimeProbeEnv(base []string, home string, baseURL string, apiKey string, attributionMode string, proxyURL string) []string {
	out := make([]string, 0, len(base)+11)
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok || shouldStripClaudeCLIRuntimeProbeEnv(key) {
			continue
		}
		out = append(out, item)
	}
	out = append(out,
		"HOME="+home,
		"ANTHROPIC_BASE_URL="+baseURL,
		"ANTHROPIC_AUTH_TOKEN="+apiKey,
		"ANTHROPIC_API_KEY="+apiKey,
		"NO_COLOR=1",
	)
	if proxyURL = strings.TrimSpace(proxyURL); proxyURL != "" {
		out = append(out,
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
			"ALL_PROXY="+proxyURL,
		)
	}
	if strings.TrimSpace(attributionMode) == "" || strings.EqualFold(attributionMode, "disabled") {
		out = append(out, "CLAUDE_CODE_ATTRIBUTION_HEADER=0")
	}
	return out
}

func shouldStripClaudeCLIRuntimeProbeEnv(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") {
		return true
	}
	switch key {
	case "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME",
		"NODE_OPTIONS", "NODE_PATH", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return true
	default:
		return false
	}
}

func formatClaudeCLIRuntimeProbeError(err error, stdout string, stderr string, secret string) string {
	parts := []string{"Claude Code CLI runtime probe failed: " + err.Error()}
	if text := strings.TrimSpace(stdout); text != "" {
		parts = append(parts, "stdout: "+text)
	}
	if text := strings.TrimSpace(stderr); text != "" {
		parts = append(parts, "stderr: "+text)
	}
	return redactClaudeCLIRuntimeProbeSecret(strings.Join(parts, "; "), secret)
}

func redactClaudeCLIRuntimeProbeSecret(text string, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[REDACTED]")
}

var _ io.Writer = (*limitedBuffer)(nil)
