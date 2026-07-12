package service

import "context"

type claudeOAuthResolvedSessionContextKey struct{}

func WithClaudeOAuthResolvedSession(ctx context.Context, session *ClaudeOAuthResolvedSession) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, claudeOAuthResolvedSessionContextKey{}, session)
}

func ClaudeOAuthResolvedSessionFromContext(ctx context.Context) (*ClaudeOAuthResolvedSession, bool) {
	if ctx == nil {
		return nil, false
	}
	session, ok := ctx.Value(claudeOAuthResolvedSessionContextKey{}).(*ClaudeOAuthResolvedSession)
	return session, ok && session != nil
}
