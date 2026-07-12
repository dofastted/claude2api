package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ClaudeOAuthBindingTTL = time.Hour

var (
	ErrClaudeOAuthBindingInvalid     = errors.New("invalid claude oauth binding")
	ErrClaudeOAuthBindingMissing     = errors.New("claude oauth binding missing")
	ErrClaudeOAuthBindingCASConflict = errors.New("claude oauth binding cas conflict")
)

type ClaudeOAuthBinding struct {
	PoolID              int64
	BindingHash         string
	AccountID           int64
	CapsuleSetVersion   int64
	CapsuleSlot         int
	Epoch               int64
	CreatedAtUnixMillis int64
	LastSeenUnixMillis  int64
}

type ClaudeOAuthBindingCandidate struct {
	PoolID            int64
	BindingHash       string
	AccountID         int64
	CapsuleSetVersion int64
	CapsuleSlot       int
}

type ClaudeOAuthBindingMigration struct {
	PoolID            int64
	BindingHash       string
	ExpectedAccountID int64
	ExpectedEpoch     int64
	NewAccountID      int64
}

type ClaudeOAuthBindingStore interface {
	GetOrCreateBinding(context.Context, ClaudeOAuthBindingCandidate) (*ClaudeOAuthBinding, bool, error)
	MigrateBindingCAS(context.Context, ClaudeOAuthBindingMigration) (*ClaudeOAuthBinding, error)
	ListCredentialBindingKeys(context.Context, int64) ([]string, error)
	DeleteCredentialBindings(context.Context, int64) (int64, error)
}

func ValidateClaudeOAuthBindingCandidate(candidate ClaudeOAuthBindingCandidate) error {
	if candidate.PoolID <= 0 || candidate.AccountID <= 0 || candidate.CapsuleSetVersion <= 0 {
		return fmt.Errorf("%w: pool, account and capsule version are required", ErrClaudeOAuthBindingInvalid)
	}
	candidate.BindingHash = strings.TrimSpace(candidate.BindingHash)
	if candidate.BindingHash == "" || len(candidate.BindingHash) > 128 {
		return fmt.Errorf("%w: binding hash is required and must be at most 128 bytes", ErrClaudeOAuthBindingInvalid)
	}
	if candidate.CapsuleSlot < 0 || candidate.CapsuleSlot > 2 {
		return fmt.Errorf("%w: capsule slot must be between 0 and 2", ErrClaudeOAuthBindingInvalid)
	}
	return nil
}

func ValidateClaudeOAuthBindingMigration(migration ClaudeOAuthBindingMigration) error {
	if migration.PoolID <= 0 || migration.ExpectedAccountID <= 0 || migration.NewAccountID <= 0 || migration.ExpectedEpoch < 0 {
		return fmt.Errorf("%w: pool, accounts and non-negative epoch are required", ErrClaudeOAuthBindingInvalid)
	}
	migration.BindingHash = strings.TrimSpace(migration.BindingHash)
	if migration.BindingHash == "" || len(migration.BindingHash) > 128 {
		return fmt.Errorf("%w: binding hash is required and must be at most 128 bytes", ErrClaudeOAuthBindingInvalid)
	}
	if migration.ExpectedAccountID == migration.NewAccountID {
		return fmt.Errorf("%w: migration must change account", ErrClaudeOAuthBindingInvalid)
	}
	return nil
}
