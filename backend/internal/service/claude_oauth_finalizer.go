package service

import (
	"fmt"
	"net/http"
)

func (s *GatewayService) finalizeClaudeOAuthCapsuleRequest(req *http.Request, account *Account, profile *ClaudeEnvironmentProfile, reqStream bool, betaHeader string, betaShouldSet bool) error {
	if req == nil || profile == nil || !isV2ClaudeEnvironmentProfile(profile) {
		return fmt.Errorf("finalize claude oauth capsule request: frozen profile is required")
	}
	authorization := getHeaderRaw(req.Header, "authorization")
	if authorization == "" {
		return fmt.Errorf("finalize claude oauth capsule request: authorization is required")
	}

	req.Header = make(http.Header, 24)
	setHeaderRaw(req.Header, "authorization", authorization)
	setHeaderRaw(req.Header, "content-type", "application/json")
	setHeaderRaw(req.Header, "anthropic-version", "2023-06-01")
	applyClaudeCodeMimicHeaders(req, reqStream, s.identityRegistry)
	s.applyClaudeEnvironmentProfile(req, account, profile)
	deleteHeaderAllForms(req.Header, "anthropic-beta")
	if betaShouldSet {
		setHeaderRaw(req.Header, "anthropic-beta", betaHeader)
	}
	deleteHeaderAllForms(req.Header, ClaudeOAuthSignedSessionHeader)
	deleteHeaderAllForms(req.Header, "traceparent")
	return nil
}
