package service

import (
	"fmt"

	"github.com/dofastted/claude2api/internal/config"
)

func ProvideClaudeOAuthSessionResolver(cfg *config.Config) (*ClaudeOAuthSessionResolver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build claude oauth session resolver: nil config")
	}
	return NewClaudeOAuthSessionResolver(ClaudeOAuthSessionKeys{
		CurrentSigningKeyID: cfg.ClaudeOAuthCapsule.SessionSigningKeyID,
		CurrentSigningKey:   []byte(cfg.ClaudeOAuthCapsule.SessionSigningKey),
		BindingKey:          []byte(cfg.ClaudeOAuthCapsule.SessionBindingKey),
	})
}

func ProvideClaudeOAuthPoolSelector(poolRepo OAuthPoolRepository, accountRepo AccountRepository, bindingStore ClaudeOAuthBindingStore, cfg *config.Config) (*ClaudeOAuthPoolSelector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build claude oauth selector: nil config")
	}
	selector, err := NewClaudeOAuthPoolSelector(poolRepo, accountRepo, bindingStore, []byte(cfg.ClaudeOAuthCapsule.SessionBindingKey))
	if err != nil {
		return nil, err
	}
	// AccountRepository implements UpdateExtra; ensure auto-generated capsules are persisted.
	selector.SetAccountExtraWriter(accountRepo)
	return selector, nil
}

func ProvideClaudeOAuthMigrationManager(selector *ClaudeOAuthPoolSelector, poolRepo OAuthPoolRepository, bindingStore ClaudeOAuthBindingStore) (*ClaudeOAuthMigrationManager, error) {
	return NewClaudeOAuthMigrationManager(selector, poolRepo, bindingStore)
}
