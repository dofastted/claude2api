package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var ErrClaudeOAuthBoundTransport = errors.New("claude oauth bound transport rejected request")

type ClaudeOAuthBoundTransportPolicy struct {
	PoolID         int64
	ProxyURL       string
	AllowedOrigins []string
}

type claudeOAuthBoundTransportContextKey struct{}

func NewClaudeOAuthBoundTransportPolicy(pool *OAuthPool, proxyURL string) (*ClaudeOAuthBoundTransportPolicy, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if pool == nil || pool.ID <= 0 || pool.EgressRouteID <= 0 || proxyURL == "" || len(pool.AllowedOrigins) == 0 {
		return nil, fmt.Errorf("%w: active pool, proxy and origins are required", ErrClaudeOAuthBoundTransport)
	}
	policy := &ClaudeOAuthBoundTransportPolicy{
		PoolID:         pool.ID,
		ProxyURL:       proxyURL,
		AllowedOrigins: append([]string(nil), pool.AllowedOrigins...),
	}
	for _, origin := range policy.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("%w: invalid allowed origin", ErrClaudeOAuthBoundTransport)
		}
	}
	return policy, nil
}

func WithClaudeOAuthBoundTransport(ctx context.Context, policy *ClaudeOAuthBoundTransportPolicy) context.Context {
	if policy == nil {
		return ctx
	}
	return context.WithValue(ctx, claudeOAuthBoundTransportContextKey{}, policy)
}

func ClaudeOAuthBoundTransportFromContext(ctx context.Context) (*ClaudeOAuthBoundTransportPolicy, bool) {
	if ctx == nil {
		return nil, false
	}
	policy, ok := ctx.Value(claudeOAuthBoundTransportContextKey{}).(*ClaudeOAuthBoundTransportPolicy)
	return policy, ok && policy != nil
}

func ValidateClaudeOAuthBoundTransport(req *http.Request, suppliedProxyURL string) (*ClaudeOAuthBoundTransportPolicy, bool, error) {
	if req == nil {
		return nil, false, nil
	}
	policy, ok := ClaudeOAuthBoundTransportFromContext(req.Context())
	if !ok {
		return nil, false, nil
	}
	if strings.TrimSpace(suppliedProxyURL) == "" || suppliedProxyURL != policy.ProxyURL {
		return nil, true, fmt.Errorf("%w: proxy mismatch", ErrClaudeOAuthBoundTransport)
	}
	if !policy.AllowsURL(req.URL) {
		return nil, true, fmt.Errorf("%w: upstream origin is not allowed", ErrClaudeOAuthBoundTransport)
	}
	return policy, true, nil
}

func (p *ClaudeOAuthBoundTransportPolicy) AllowsURL(target *url.URL) bool {
	if p == nil || target == nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return false
	}
	if target.RawQuery != "" && target.RawQuery != "beta=true" {
		return false
	}
	candidate := strings.ToLower(target.Scheme) + "://" + strings.ToLower(target.Host) + target.EscapedPath()
	for _, origin := range p.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil {
			continue
		}
		allowed := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + parsed.EscapedPath()
		if candidate == allowed {
			return true
		}
	}
	return false
}
