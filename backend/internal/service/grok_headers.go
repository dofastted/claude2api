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
	headers.Set("User-Agent", xai.DefaultCLIUserAgent)
	headers.Set("X-XAI-Token-Auth", xai.DefaultCLITokenAuth)
	headers.Set("x-grok-client-identifier", xai.DefaultCLIClientIdentifier)
	headers.Set("x-grok-client-version", xai.DefaultCLIClientVersion)
}
