-- Add local intercept markers for Claude Code warmup/mock responses.
ALTER TABLE usage_logs
    ALTER COLUMN account_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS local_intercept BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS intercept_type VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_usage_logs_local_intercept_created_at
    ON usage_logs (local_intercept, created_at);
