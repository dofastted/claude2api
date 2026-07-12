package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/dofastted/claude2api/ent"
	"github.com/dofastted/claude2api/ent/oauthcapsuleset"
	"github.com/dofastted/claude2api/ent/oauthpool"
	"github.com/dofastted/claude2api/ent/oauthpoolcredential"
	"github.com/dofastted/claude2api/internal/service"
)

type claudeOAuthPoolRepository struct {
	client *dbent.Client
}

func NewClaudeOAuthPoolRepository(client *dbent.Client) service.OAuthPoolRepository {
	return &claudeOAuthPoolRepository{client: client}
}

func (r *claudeOAuthPoolRepository) Create(ctx context.Context, pool *service.OAuthPool) error {
	if err := service.ValidateOAuthPool(pool); err != nil {
		return err
	}
	created, err := r.client.OAuthPool.Create().
		SetName(pool.Name).
		SetProvider(pool.Provider).
		SetStatus(pool.Status).
		SetMode(pool.Mode).
		SetEgressRouteID(pool.EgressRouteID).
		SetAllowedOrigins(pool.AllowedOrigins).
		SetAllowedModels(pool.AllowedModels).
		SetActiveCapsuleSetVersion(pool.ActiveCapsuleSetVersion).
		SetNillablePreviousCapsuleSetVersion(pool.PreviousCapsuleSetVersion).
		SetCompatibilityDigest(pool.CompatibilityDigest).
		SetSessionTTLSeconds(pool.SessionTTLSeconds).
		SetNillableShadowStartedAt(pool.ShadowStartedAt).
		SetNillableShadowQualifiedAt(pool.ShadowQualifiedAt).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create oauth pool: %w", err)
	}
	copyOAuthPool(pool, oauthPoolEntityToService(created))
	return nil
}

func (r *claudeOAuthPoolRepository) Update(ctx context.Context, pool *service.OAuthPool) error {
	if pool == nil || pool.ID <= 0 {
		return fmt.Errorf("%w: id is required", service.ErrOAuthPoolInvalid)
	}
	if err := service.ValidateOAuthPool(pool); err != nil {
		return err
	}
	builder := r.client.OAuthPool.UpdateOneID(pool.ID).
		SetName(pool.Name).
		SetProvider(pool.Provider).
		SetStatus(pool.Status).
		SetMode(pool.Mode).
		SetEgressRouteID(pool.EgressRouteID).
		SetAllowedOrigins(pool.AllowedOrigins).
		SetAllowedModels(pool.AllowedModels).
		SetActiveCapsuleSetVersion(pool.ActiveCapsuleSetVersion).
		SetCompatibilityDigest(pool.CompatibilityDigest).
		SetSessionTTLSeconds(pool.SessionTTLSeconds).
		SetNillableShadowStartedAt(pool.ShadowStartedAt).
		SetNillableShadowQualifiedAt(pool.ShadowQualifiedAt)
	if pool.PreviousCapsuleSetVersion == nil {
		builder = builder.ClearPreviousCapsuleSetVersion()
	} else {
		builder = builder.SetPreviousCapsuleSetVersion(*pool.PreviousCapsuleSetVersion)
	}
	updated, err := builder.Save(ctx)
	if dbent.IsNotFound(err) {
		return service.ErrOAuthPoolNotFound
	}
	if err != nil {
		return fmt.Errorf("update oauth pool: %w", err)
	}
	copyOAuthPool(pool, oauthPoolEntityToService(updated))
	return nil
}

func (r *claudeOAuthPoolRepository) GetByID(ctx context.Context, id int64) (*service.OAuthPool, error) {
	entity, err := r.client.OAuthPool.Query().Where(oauthpool.IDEQ(id)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, service.ErrOAuthPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth pool: %w", err)
	}
	return oauthPoolEntityToService(entity), nil
}

func (r *claudeOAuthPoolRepository) List(ctx context.Context) ([]service.OAuthPool, error) {
	entities, err := r.client.OAuthPool.Query().Order(dbent.Asc(oauthpool.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list oauth pools: %w", err)
	}
	out := make([]service.OAuthPool, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *oauthPoolEntityToService(entity))
	}
	return out, nil
}

func (r *claudeOAuthPoolRepository) Delete(ctx context.Context, id int64) error {
	err := r.client.OAuthPool.DeleteOneID(id).Exec(ctx)
	if dbent.IsNotFound(err) {
		return service.ErrOAuthPoolNotFound
	}
	if err != nil {
		return fmt.Errorf("delete oauth pool: %w", err)
	}
	return nil
}

func (r *claudeOAuthPoolRepository) AddCredential(ctx context.Context, credential *service.OAuthPoolCredential) error {
	if credential == nil || credential.PoolID <= 0 || credential.AccountID <= 0 {
		return fmt.Errorf("%w: pool and account are required", service.ErrOAuthPoolCredentialInvalid)
	}
	if credential.State == "" {
		credential.State = service.OAuthPoolCredentialAvailable
	}
	created, err := r.client.OAuthPoolCredential.Create().
		SetPoolID(credential.PoolID).
		SetAccountID(credential.AccountID).
		SetState(credential.State).
		SetNillableCooldownUntil(credential.CooldownUntil).
		Save(ctx)
	if dbent.IsConstraintError(err) {
		return service.ErrOAuthPoolCredentialConflict
	}
	if err != nil {
		return fmt.Errorf("add oauth pool credential: %w", err)
	}
	*credential = *oauthPoolCredentialEntityToService(created)
	return nil
}

func (r *claudeOAuthPoolRepository) UpdateCredential(ctx context.Context, credential *service.OAuthPoolCredential) error {
	if credential == nil || credential.ID <= 0 || credential.PoolID <= 0 || credential.AccountID <= 0 || credential.State == "" {
		return fmt.Errorf("%w: credential identity and state are required", service.ErrOAuthPoolCredentialInvalid)
	}
	updated, err := r.client.OAuthPoolCredential.UpdateOneID(credential.ID).
		SetState(credential.State).
		SetNillableCooldownUntil(credential.CooldownUntil).
		Save(ctx)
	if dbent.IsNotFound(err) {
		return service.ErrOAuthPoolCredentialInvalid
	}
	if err != nil {
		return fmt.Errorf("update oauth pool credential: %w", err)
	}
	*credential = *oauthPoolCredentialEntityToService(updated)
	return nil
}

func (r *claudeOAuthPoolRepository) RemoveCredential(ctx context.Context, poolID, accountID int64) error {
	deleted, err := r.client.OAuthPoolCredential.Delete().Where(
		oauthpoolcredential.PoolIDEQ(poolID),
		oauthpoolcredential.AccountIDEQ(accountID),
	).Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove oauth pool credential: %w", err)
	}
	if deleted == 0 {
		return service.ErrOAuthPoolCredentialInvalid
	}
	return nil
}

func (r *claudeOAuthPoolRepository) ListCredentials(ctx context.Context, poolID int64) ([]service.OAuthPoolCredential, error) {
	entities, err := r.client.OAuthPoolCredential.Query().
		Where(oauthpoolcredential.PoolIDEQ(poolID)).
		Order(dbent.Asc(oauthpoolcredential.FieldAccountID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list oauth pool credentials: %w", err)
	}
	out := make([]service.OAuthPoolCredential, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *oauthPoolCredentialEntityToService(entity))
	}
	return out, nil
}

func (r *claudeOAuthPoolRepository) CreateCapsuleSet(ctx context.Context, set *service.OAuthCapsuleSet) error {
	if set == nil || set.PoolID <= 0 || set.Version <= 0 || set.CompatibilityDigest == "" || len(set.Payload) == 0 {
		return fmt.Errorf("%w: pool, version, digest and payload are required", service.ErrOAuthPoolInvalid)
	}
	created, err := r.client.OAuthCapsuleSet.Create().
		SetPoolID(set.PoolID).
		SetVersion(set.Version).
		SetCompatibilityDigest(set.CompatibilityDigest).
		SetPayload(set.Payload).
		Save(ctx)
	if dbent.IsConstraintError(err) {
		return service.ErrOAuthCapsuleSetConflict
	}
	if err != nil {
		return fmt.Errorf("create oauth capsule set: %w", err)
	}
	*set = *oauthCapsuleSetEntityToService(created)
	return nil
}

func (r *claudeOAuthPoolRepository) GetCapsuleSet(ctx context.Context, poolID, version int64) (*service.OAuthCapsuleSet, error) {
	entity, err := r.client.OAuthCapsuleSet.Query().Where(
		oauthcapsuleset.PoolIDEQ(poolID),
		oauthcapsuleset.VersionEQ(version),
	).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, service.ErrOAuthCapsuleSetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth capsule set: %w", err)
	}
	return oauthCapsuleSetEntityToService(entity), nil
}

func (r *claudeOAuthPoolRepository) ActivateCapsuleSet(ctx context.Context, poolID, version int64, compatibilityDigest string) (*service.OAuthPool, error) {
	compatibilityDigest = strings.TrimSpace(compatibilityDigest)
	if poolID <= 0 || version <= 0 || compatibilityDigest == "" {
		return nil, fmt.Errorf("%w: pool, version and digest are required", service.ErrOAuthPoolInvalid)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin capsule activation: %w", err)
	}
	rollback := func(cause error) (*service.OAuthPool, error) {
		return nil, errors.Join(cause, tx.Rollback())
	}
	poolEntity, err := tx.OAuthPool.Query().Where(oauthpool.IDEQ(poolID)).Only(ctx)
	if dbent.IsNotFound(err) {
		return rollback(service.ErrOAuthPoolNotFound)
	}
	if err != nil {
		return rollback(fmt.Errorf("load oauth pool: %w", err))
	}
	setEntity, err := tx.OAuthCapsuleSet.Query().Where(
		oauthcapsuleset.PoolIDEQ(poolID),
		oauthcapsuleset.VersionEQ(version),
	).Only(ctx)
	if dbent.IsNotFound(err) {
		return rollback(service.ErrOAuthCapsuleSetNotFound)
	}
	if err != nil {
		return rollback(fmt.Errorf("load oauth capsule set: %w", err))
	}
	if setEntity.CompatibilityDigest != compatibilityDigest {
		return rollback(fmt.Errorf("%w: capsule compatibility digest mismatch", service.ErrOAuthPoolInvalid))
	}
	builder := tx.OAuthPool.UpdateOneID(poolID).
		SetActiveCapsuleSetVersion(version).
		SetCompatibilityDigest(compatibilityDigest)
	if poolEntity.ActiveCapsuleSetVersion > 0 && poolEntity.ActiveCapsuleSetVersion != version {
		builder = builder.SetPreviousCapsuleSetVersion(poolEntity.ActiveCapsuleSetVersion)
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		return rollback(fmt.Errorf("activate oauth capsule set: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit capsule activation: %w", err)
	}
	return oauthPoolEntityToService(updated), nil
}

func oauthPoolEntityToService(entity *dbent.OAuthPool) *service.OAuthPool {
	if entity == nil {
		return nil
	}
	return &service.OAuthPool{
		ID:                        entity.ID,
		Name:                      entity.Name,
		Provider:                  entity.Provider,
		Status:                    entity.Status,
		Mode:                      entity.Mode,
		EgressRouteID:             entity.EgressRouteID,
		AllowedOrigins:            entity.AllowedOrigins,
		AllowedModels:             entity.AllowedModels,
		ActiveCapsuleSetVersion:   entity.ActiveCapsuleSetVersion,
		PreviousCapsuleSetVersion: entity.PreviousCapsuleSetVersion,
		CompatibilityDigest:       entity.CompatibilityDigest,
		SessionTTLSeconds:         entity.SessionTTLSeconds,
		ShadowStartedAt:           entity.ShadowStartedAt,
		ShadowQualifiedAt:         entity.ShadowQualifiedAt,
		CreatedAt:                 entity.CreatedAt,
		UpdatedAt:                 entity.UpdatedAt,
	}
}

func oauthPoolCredentialEntityToService(entity *dbent.OAuthPoolCredential) *service.OAuthPoolCredential {
	if entity == nil {
		return nil
	}
	return &service.OAuthPoolCredential{
		ID:            entity.ID,
		PoolID:        entity.PoolID,
		AccountID:     entity.AccountID,
		State:         entity.State,
		CooldownUntil: entity.CooldownUntil,
		CreatedAt:     entity.CreatedAt,
		UpdatedAt:     entity.UpdatedAt,
	}
}

func oauthCapsuleSetEntityToService(entity *dbent.OAuthCapsuleSet) *service.OAuthCapsuleSet {
	if entity == nil {
		return nil
	}
	return &service.OAuthCapsuleSet{
		ID:                  entity.ID,
		PoolID:              entity.PoolID,
		Version:             entity.Version,
		CompatibilityDigest: entity.CompatibilityDigest,
		Payload:             entity.Payload,
		CreatedAt:           entity.CreatedAt,
		UpdatedAt:           entity.UpdatedAt,
	}
}

func copyOAuthPool(dst, src *service.OAuthPool) {
	if dst == nil || src == nil {
		return
	}
	*dst = *src
}
