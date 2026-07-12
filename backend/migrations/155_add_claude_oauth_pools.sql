-- Add strict Anthropic OAuth pool policy, credential membership, capsule versions, and Group binding.
CREATE TABLE IF NOT EXISTS oauth_pools (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'claude_oauth',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    mode VARCHAR(20) NOT NULL DEFAULT 'shadow',
    egress_route_id BIGINT NOT NULL REFERENCES proxies(id) ON DELETE RESTRICT,
    allowed_origins JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    active_capsule_set_version BIGINT NOT NULL DEFAULT 0,
    previous_capsule_set_version BIGINT,
    compatibility_digest VARCHAR(128) NOT NULL DEFAULT '',
    session_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    shadow_started_at TIMESTAMPTZ,
    shadow_qualified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT oauth_pools_provider_check CHECK (provider = 'claude_oauth'),
    CONSTRAINT oauth_pools_status_check CHECK (status IN ('active', 'inactive')),
    CONSTRAINT oauth_pools_mode_check CHECK (mode IN ('shadow', 'enforce')),
    CONSTRAINT oauth_pools_session_ttl_check CHECK (session_ttl_seconds = 3600)
);

CREATE INDEX IF NOT EXISTS idx_oauth_pools_status ON oauth_pools(status);
CREATE INDEX IF NOT EXISTS idx_oauth_pools_mode ON oauth_pools(mode);
CREATE INDEX IF NOT EXISTS idx_oauth_pools_egress_route_id ON oauth_pools(egress_route_id);
CREATE INDEX IF NOT EXISTS idx_oauth_pools_deleted_at ON oauth_pools(deleted_at);

CREATE TABLE IF NOT EXISTS oauth_pool_credentials (
    id BIGSERIAL PRIMARY KEY,
    pool_id BIGINT NOT NULL REFERENCES oauth_pools(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    cooldown_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT oauth_pool_credentials_account_unique UNIQUE(account_id),
    CONSTRAINT oauth_pool_credentials_state_check CHECK (state IN ('available', 'cooldown', 'exhausted', 'revoked', 'unhealthy'))
);

CREATE INDEX IF NOT EXISTS idx_oauth_pool_credentials_pool_id ON oauth_pool_credentials(pool_id);
CREATE INDEX IF NOT EXISTS idx_oauth_pool_credentials_state ON oauth_pool_credentials(state);
CREATE INDEX IF NOT EXISTS idx_oauth_pool_credentials_pool_state ON oauth_pool_credentials(pool_id, state);

CREATE TABLE IF NOT EXISTS oauth_capsule_sets (
    id BIGSERIAL PRIMARY KEY,
    pool_id BIGINT NOT NULL REFERENCES oauth_pools(id) ON DELETE CASCADE,
    version BIGINT NOT NULL,
    compatibility_digest VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT oauth_capsule_sets_pool_version_unique UNIQUE(pool_id, version),
    CONSTRAINT oauth_capsule_sets_version_positive CHECK (version > 0)
);

CREATE INDEX IF NOT EXISTS idx_oauth_capsule_sets_digest ON oauth_capsule_sets(compatibility_digest);

ALTER TABLE groups ADD COLUMN IF NOT EXISTS oauth_pool_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'groups_oauth_pool_id_fkey'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_oauth_pool_id_fkey
            FOREIGN KEY (oauth_pool_id) REFERENCES oauth_pools(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_groups_oauth_pool_id ON groups(oauth_pool_id);
