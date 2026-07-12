package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	OAuthPoolProviderClaude = "claude_oauth"
	OAuthPoolStatusActive   = "active"
	OAuthPoolStatusInactive = "inactive"
	OAuthPoolModeShadow     = "shadow"
	OAuthPoolModeEnforce    = "enforce"

	OAuthPoolCredentialAvailable = "available"
	OAuthPoolCredentialCooldown  = "cooldown"
	OAuthPoolCredentialExhausted = "exhausted"
	OAuthPoolCredentialRevoked   = "revoked"
	OAuthPoolCredentialUnhealthy = "unhealthy"

	ClaudeOAuthSessionTTLSeconds = 3600
)

var (
	ErrOAuthPoolNotFound              = errors.New("oauth pool not found")
	ErrOAuthPoolInvalid               = errors.New("invalid oauth pool")
	ErrOAuthPoolCredentialConflict    = errors.New("oauth credential already belongs to a pool")
	ErrOAuthPoolCredentialInvalid     = errors.New("invalid oauth pool credential")
	ErrOAuthCapsuleSetNotFound        = errors.New("oauth capsule set not found")
	ErrOAuthCapsuleSetConflict        = errors.New("oauth capsule set version already exists")
	ErrOAuthPoolEnforceGateNotReached = errors.New("oauth pool enforce gate not reached")
)

var approvedClaudeOAuthOrigins = map[string]struct{}{
	"https://api.anthropic.com/v1/messages":              {},
	"https://api.anthropic.com/v1/messages/count_tokens": {},
}

type OAuthPool struct {
	ID                        int64      `json:"id"`
	Name                      string     `json:"name"`
	Provider                  string     `json:"provider"`
	Status                    string     `json:"status"`
	Mode                      string     `json:"mode"`
	EgressRouteID             int64      `json:"egress_route_id"`
	AllowedOrigins            []string   `json:"allowed_origins"`
	AllowedModels             []string   `json:"allowed_models"`
	ActiveCapsuleSetVersion   int64      `json:"active_capsule_set_version"`
	PreviousCapsuleSetVersion *int64     `json:"previous_capsule_set_version,omitempty"`
	CompatibilityDigest       string     `json:"compatibility_digest"`
	SessionTTLSeconds         int        `json:"session_ttl_seconds"`
	ShadowStartedAt           *time.Time `json:"shadow_started_at,omitempty"`
	ShadowQualifiedAt         *time.Time `json:"shadow_qualified_at,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type OAuthPoolCredential struct {
	ID            int64      `json:"id"`
	PoolID        int64      `json:"pool_id"`
	AccountID     int64      `json:"account_id"`
	State         string     `json:"state"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type OAuthCapsuleSet struct {
	ID                  int64          `json:"id"`
	PoolID              int64          `json:"pool_id"`
	Version             int64          `json:"version"`
	CompatibilityDigest string         `json:"compatibility_digest"`
	Payload             map[string]any `json:"payload"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type OAuthPoolRepository interface {
	Create(ctx context.Context, pool *OAuthPool) error
	Update(ctx context.Context, pool *OAuthPool) error
	GetByID(ctx context.Context, id int64) (*OAuthPool, error)
	List(ctx context.Context) ([]OAuthPool, error)
	Delete(ctx context.Context, id int64) error
	AddCredential(ctx context.Context, credential *OAuthPoolCredential) error
	UpdateCredential(ctx context.Context, credential *OAuthPoolCredential) error
	RemoveCredential(ctx context.Context, poolID, accountID int64) error
	ListCredentials(ctx context.Context, poolID int64) ([]OAuthPoolCredential, error)
	CreateCapsuleSet(ctx context.Context, set *OAuthCapsuleSet) error
	GetCapsuleSet(ctx context.Context, poolID, version int64) (*OAuthCapsuleSet, error)
	ActivateCapsuleSet(ctx context.Context, poolID, version int64, compatibilityDigest string) (*OAuthPool, error)
}

func ValidateOAuthPool(pool *OAuthPool) error {
	if pool == nil {
		return fmt.Errorf("%w: pool is nil", ErrOAuthPoolInvalid)
	}
	pool.Name = strings.TrimSpace(pool.Name)
	if pool.Name == "" {
		return fmt.Errorf("%w: name is required", ErrOAuthPoolInvalid)
	}
	if pool.Provider == "" {
		pool.Provider = OAuthPoolProviderClaude
	}
	if pool.Provider != OAuthPoolProviderClaude {
		return fmt.Errorf("%w: unsupported provider %q", ErrOAuthPoolInvalid, pool.Provider)
	}
	if pool.Status == "" {
		pool.Status = OAuthPoolStatusActive
	}
	if pool.Status != OAuthPoolStatusActive && pool.Status != OAuthPoolStatusInactive {
		return fmt.Errorf("%w: unsupported status %q", ErrOAuthPoolInvalid, pool.Status)
	}
	if pool.Mode == "" {
		pool.Mode = OAuthPoolModeShadow
	}
	if pool.Mode != OAuthPoolModeShadow && pool.Mode != OAuthPoolModeEnforce {
		return fmt.Errorf("%w: unsupported mode %q", ErrOAuthPoolInvalid, pool.Mode)
	}
	if pool.EgressRouteID <= 0 {
		return fmt.Errorf("%w: egress route is required", ErrOAuthPoolInvalid)
	}
	if pool.SessionTTLSeconds == 0 {
		pool.SessionTTLSeconds = ClaudeOAuthSessionTTLSeconds
	}
	if pool.SessionTTLSeconds != ClaudeOAuthSessionTTLSeconds {
		return fmt.Errorf("%w: session TTL must be %d seconds", ErrOAuthPoolInvalid, ClaudeOAuthSessionTTLSeconds)
	}
	origins, err := normalizeApprovedOrigins(pool.AllowedOrigins)
	if err != nil {
		return err
	}
	pool.AllowedOrigins = origins
	pool.AllowedModels = normalizeNonEmptyStrings(pool.AllowedModels)
	if len(pool.AllowedModels) == 0 {
		return fmt.Errorf("%w: at least one allowed model is required", ErrOAuthPoolInvalid)
	}
	pool.CompatibilityDigest = strings.TrimSpace(pool.CompatibilityDigest)
	if pool.Mode == OAuthPoolModeEnforce && (pool.ActiveCapsuleSetVersion <= 0 || pool.CompatibilityDigest == "") {
		return fmt.Errorf("%w: enforce mode requires an active capsule set and compatibility digest", ErrOAuthPoolInvalid)
	}
	return nil
}

func ValidateOAuthPoolAccount(pool *OAuthPool, account *Account) error {
	if pool == nil || account == nil {
		return fmt.Errorf("%w: pool and account are required", ErrOAuthPoolCredentialInvalid)
	}
	if account.Platform != PlatformAnthropic || account.Type != AccountTypeOAuth {
		return fmt.Errorf("%w: account must be anthropic oauth", ErrOAuthPoolCredentialInvalid)
	}
	if account.ProxyID != nil && *account.ProxyID != pool.EgressRouteID {
		return fmt.Errorf("%w: account proxy %d does not match pool egress %d", ErrOAuthPoolCredentialInvalid, *account.ProxyID, pool.EgressRouteID)
	}
	return nil
}

func (p *OAuthPool) SupportsModel(model string) bool {
	model = strings.TrimSpace(model)
	if p == nil || model == "" {
		return false
	}
	for _, allowed := range p.AllowedModels {
		if allowed == model || (strings.HasSuffix(allowed, "*") && strings.HasPrefix(model, strings.TrimSuffix(allowed, "*"))) {
			return true
		}
	}
	return false
}

func normalizeApprovedOrigins(values []string) ([]string, error) {
	values = normalizeNonEmptyStrings(values)
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one allowed origin is required", ErrOAuthPoolInvalid)
	}
	for _, value := range values {
		if _, ok := approvedClaudeOAuthOrigins[value]; !ok {
			return nil, fmt.Errorf("%w: origin %q is not approved", ErrOAuthPoolInvalid, value)
		}
	}
	return values, nil
}

func normalizeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
