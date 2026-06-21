package clientidentity_test

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/clientidentity"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestResolve(t *testing.T) {
	resolver := clientidentity.NewResolver()

	account := &service.Account{Extra: map[string]any{"client_family": "claude-cli"}}
	snapshot := resolver.Resolve(account)
	assert.NotNil(t, snapshot)
	assert.Equal(t, "claude-cli-default", snapshot.TLSProfileName)

	account2 := &service.Account{Extra: map[string]any{}}
	snapshot2 := resolver.Resolve(account2)
	assert.Nil(t, snapshot2)

	account3 := &service.Account{Extra: map[string]any{"client_family": "unknown"}}
	snapshot3 := resolver.Resolve(account3)
	assert.Nil(t, snapshot3)
}
