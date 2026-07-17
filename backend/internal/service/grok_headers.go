package service

import (
	"net/http"
	"strings"

	"github.com/dofastted/claude2api/internal/pkg/xai"
)

func applyGrokUpstreamIdentityHeaders(headers http.Header, account *Account) {
	if headers == nil || account == nil {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(account.GetGrokBaseURL()), "/")
	if !strings.EqualFold(baseURL, xai.DefaultCLIBaseURL) {
		return
	}
	credentialHeaders, _ := account.Credentials["headers"].(map[string]any)
	setGrokHeaderWithFallback(headers, credentialHeaders, "User-Agent", xai.DefaultCLIUserAgent)
	setGrokHeaderWithFallback(headers, credentialHeaders, "X-XAI-Token-Auth", xai.DefaultCLITokenAuth)
	setGrokHeaderWithFallback(headers, credentialHeaders, "x-grok-client-identifier", xai.DefaultCLIClientIdentifier)
	setGrokHeaderWithFallback(headers, credentialHeaders, "x-grok-client-version", xai.DefaultCLIClientVersion)
}

func setGrokHeaderWithFallback(headers http.Header, credentialHeaders map[string]any, key, fallback string) {
	for storedKey, value := range credentialHeaders {
		if !strings.EqualFold(strings.TrimSpace(storedKey), key) {
			continue
		}
		if storedValue, ok := value.(string); ok && strings.TrimSpace(storedValue) != "" {
			headers.Set(key, strings.TrimSpace(storedValue))
			return
		}
	}
	headers.Set(key, fallback)
}
