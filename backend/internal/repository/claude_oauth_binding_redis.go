package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dofastted/claude2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	claudeOAuthBindingKeyPrefix            = "claude_oauth:binding:"
	claudeOAuthCredentialBindingsKeyPrefix = "claude_oauth:credential_bindings:"
)

var claudeOAuthGetOrCreateBindingScript = redis.NewScript(`
local created = 0
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HSET', KEYS[1],
    'pool_id', ARGV[1],
    'binding_hash', ARGV[2],
    'account_id', ARGV[3],
    'capsule_set_version', ARGV[4],
    'capsule_slot', ARGV[5],
    'epoch', '0',
    'created_at_ms', ARGV[6],
    'last_seen_ms', ARGV[6])
  created = 1
else
  redis.call('HSET', KEYS[1], 'last_seen_ms', ARGV[6])
end
local account_id = redis.call('HGET', KEYS[1], 'account_id')
if account_id == false then
  return {'status', 'invalid'}
end
local reverse_key = ARGV[8] .. account_id
redis.call('SADD', reverse_key, KEYS[1])
redis.call('EXPIRE', reverse_key, tonumber(ARGV[7]))
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[7]))
local result = {'status', 'ok', 'created', tostring(created)}
local binding = redis.call('HGETALL', KEYS[1])
for i = 1, #binding do
  table.insert(result, binding[i])
end
return result
`)

var claudeOAuthMigrateBindingCASScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('SREM', ARGV[6] .. ARGV[2], KEYS[1])
  return {'status', 'missing'}
end
local current_epoch = redis.call('HGET', KEYS[1], 'epoch')
local current_account = redis.call('HGET', KEYS[1], 'account_id')
if current_epoch ~= ARGV[1] or current_account ~= ARGV[2] then
  return {'status', 'conflict'}
end
local next_epoch = tostring(tonumber(current_epoch) + 1)
redis.call('SREM', ARGV[6] .. current_account, KEYS[1])
redis.call('HSET', KEYS[1],
  'account_id', ARGV[3],
  'epoch', next_epoch,
  'last_seen_ms', ARGV[4])
redis.call('SADD', ARGV[6] .. ARGV[3], KEYS[1])
redis.call('EXPIRE', ARGV[6] .. ARGV[3], tonumber(ARGV[5]))
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[5]))
local result = {'status', 'ok'}
local binding = redis.call('HGETALL', KEYS[1])
for i = 1, #binding do
  table.insert(result, binding[i])
end
return result
`)

var claudeOAuthDeleteCredentialBindingsScript = redis.NewScript(`
local binding_keys = redis.call('SMEMBERS', KEYS[1])
local deleted = 0
for _, binding_key in ipairs(binding_keys) do
  if redis.call('HGET', binding_key, 'account_id') == ARGV[1] then
    deleted = deleted + redis.call('DEL', binding_key)
  end
end
redis.call('DEL', KEYS[1])
return deleted
`)

type claudeOAuthBindingRedisStore struct {
	rdb *redis.Client
	now func() time.Time
}

func NewClaudeOAuthBindingRedisStore(rdb *redis.Client) service.ClaudeOAuthBindingStore {
	return &claudeOAuthBindingRedisStore{rdb: rdb, now: time.Now}
}

func (s *claudeOAuthBindingRedisStore) GetOrCreateBinding(ctx context.Context, candidate service.ClaudeOAuthBindingCandidate) (*service.ClaudeOAuthBinding, bool, error) {
	if err := service.ValidateClaudeOAuthBindingCandidate(candidate); err != nil {
		return nil, false, err
	}
	if s == nil || s.rdb == nil {
		return nil, false, fmt.Errorf("get or create claude oauth binding: nil redis client")
	}
	result, err := claudeOAuthGetOrCreateBindingScript.Run(ctx, s.rdb, []string{claudeOAuthBindingKey(candidate.PoolID, candidate.BindingHash)},
		candidate.PoolID,
		candidate.BindingHash,
		candidate.AccountID,
		candidate.CapsuleSetVersion,
		candidate.CapsuleSlot,
		s.now().UTC().UnixMilli(),
		int(service.ClaudeOAuthBindingTTL/time.Second),
		claudeOAuthCredentialBindingsKeyPrefix,
	).Result()
	if err != nil {
		return nil, false, fmt.Errorf("get or create claude oauth binding: %w", err)
	}
	values, err := claudeOAuthScriptMap(result)
	if err != nil {
		return nil, false, err
	}
	if values["status"] != "ok" {
		return nil, false, fmt.Errorf("%w: redis binding payload is incomplete", service.ErrClaudeOAuthBindingInvalid)
	}
	binding, err := claudeOAuthBindingFromMap(values)
	if err != nil {
		return nil, false, err
	}
	return binding, values["created"] == "1", nil
}

func (s *claudeOAuthBindingRedisStore) MigrateBindingCAS(ctx context.Context, migration service.ClaudeOAuthBindingMigration) (*service.ClaudeOAuthBinding, error) {
	if err := service.ValidateClaudeOAuthBindingMigration(migration); err != nil {
		return nil, err
	}
	if s == nil || s.rdb == nil {
		return nil, fmt.Errorf("migrate claude oauth binding: nil redis client")
	}
	result, err := claudeOAuthMigrateBindingCASScript.Run(ctx, s.rdb, []string{claudeOAuthBindingKey(migration.PoolID, migration.BindingHash)},
		migration.ExpectedEpoch,
		migration.ExpectedAccountID,
		migration.NewAccountID,
		s.now().UTC().UnixMilli(),
		int(service.ClaudeOAuthBindingTTL/time.Second),
		claudeOAuthCredentialBindingsKeyPrefix,
	).Result()
	if err != nil {
		return nil, fmt.Errorf("migrate claude oauth binding: %w", err)
	}
	values, err := claudeOAuthScriptMap(result)
	if err != nil {
		return nil, err
	}
	switch values["status"] {
	case "missing":
		return nil, service.ErrClaudeOAuthBindingMissing
	case "conflict":
		return nil, service.ErrClaudeOAuthBindingCASConflict
	case "ok":
		return claudeOAuthBindingFromMap(values)
	default:
		return nil, fmt.Errorf("%w: unexpected migration result", service.ErrClaudeOAuthBindingInvalid)
	}
}

func (s *claudeOAuthBindingRedisStore) ListCredentialBindingKeys(ctx context.Context, accountID int64) ([]string, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("%w: account is required", service.ErrClaudeOAuthBindingInvalid)
	}
	if s == nil || s.rdb == nil {
		return nil, fmt.Errorf("list claude oauth credential bindings: nil redis client")
	}
	keys, err := s.rdb.SMembers(ctx, claudeOAuthCredentialBindingsKey(accountID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("list claude oauth credential bindings: %w", err)
	}
	return keys, nil
}

func (s *claudeOAuthBindingRedisStore) DeleteCredentialBindings(ctx context.Context, accountID int64) (int64, error) {
	if accountID <= 0 {
		return 0, fmt.Errorf("%w: account is required", service.ErrClaudeOAuthBindingInvalid)
	}
	if s == nil || s.rdb == nil {
		return 0, fmt.Errorf("delete claude oauth credential bindings: nil redis client")
	}
	deleted, err := claudeOAuthDeleteCredentialBindingsScript.Run(
		ctx,
		s.rdb,
		[]string{claudeOAuthCredentialBindingsKey(accountID)},
		accountID,
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("delete claude oauth credential bindings: %w", err)
	}
	return deleted, nil
}

func claudeOAuthBindingKey(poolID int64, bindingHash string) string {
	return claudeOAuthBindingKeyPrefix + strconv.FormatInt(poolID, 10) + ":" + bindingHash
}

func claudeOAuthCredentialBindingsKey(accountID int64) string {
	return claudeOAuthCredentialBindingsKeyPrefix + strconv.FormatInt(accountID, 10)
}

func claudeOAuthScriptMap(result any) (map[string]string, error) {
	items, ok := result.([]any)
	if !ok || len(items)%2 != 0 {
		return nil, fmt.Errorf("%w: invalid redis script result", service.ErrClaudeOAuthBindingInvalid)
	}
	values := make(map[string]string, len(items)/2)
	for index := 0; index < len(items); index += 2 {
		key, keyOK := items[index].(string)
		value, valueOK := items[index+1].(string)
		if !keyOK || !valueOK {
			return nil, fmt.Errorf("%w: invalid redis script field", service.ErrClaudeOAuthBindingInvalid)
		}
		values[key] = value
	}
	return values, nil
}

func claudeOAuthBindingFromMap(values map[string]string) (*service.ClaudeOAuthBinding, error) {
	poolID, err := strconv.ParseInt(values["pool_id"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid pool id", service.ErrClaudeOAuthBindingInvalid)
	}
	accountID, err := strconv.ParseInt(values["account_id"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid account id", service.ErrClaudeOAuthBindingInvalid)
	}
	capsuleVersion, err := strconv.ParseInt(values["capsule_set_version"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid capsule version", service.ErrClaudeOAuthBindingInvalid)
	}
	capsuleSlot, err := strconv.Atoi(values["capsule_slot"])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid capsule slot", service.ErrClaudeOAuthBindingInvalid)
	}
	epoch, err := strconv.ParseInt(values["epoch"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid epoch", service.ErrClaudeOAuthBindingInvalid)
	}
	createdAt, err := strconv.ParseInt(values["created_at_ms"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid created timestamp", service.ErrClaudeOAuthBindingInvalid)
	}
	lastSeen, err := strconv.ParseInt(values["last_seen_ms"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid last seen timestamp", service.ErrClaudeOAuthBindingInvalid)
	}
	return &service.ClaudeOAuthBinding{
		PoolID:              poolID,
		BindingHash:         values["binding_hash"],
		AccountID:           accountID,
		CapsuleSetVersion:   capsuleVersion,
		CapsuleSlot:         capsuleSlot,
		Epoch:               epoch,
		CreatedAtUnixMillis: createdAt,
		LastSeenUnixMillis:  lastSeen,
	}, nil
}
