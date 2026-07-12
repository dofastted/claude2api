package service

import "context"

type claudeOAuthSelectionContextKey struct{}

func WithClaudeOAuthSelection(ctx context.Context, selection *ClaudeOAuthSelection) context.Context {
	if selection == nil {
		return ctx
	}
	return context.WithValue(ctx, claudeOAuthSelectionContextKey{}, selection)
}

func ClaudeOAuthSelectionFromContext(ctx context.Context) (*ClaudeOAuthSelection, bool) {
	if ctx == nil {
		return nil, false
	}
	selection, ok := ctx.Value(claudeOAuthSelectionContextKey{}).(*ClaudeOAuthSelection)
	return selection, ok && selection != nil
}
