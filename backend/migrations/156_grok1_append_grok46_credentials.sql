-- Only grok-1 / xai32131@163.com: persist grok-4.6 in credentials.model_mapping
-- and stamp the updated account email. grok-heavy / mci777686@outlook.com is excluded.

UPDATE accounts
SET
    credentials = jsonb_set(
        jsonb_set(
            jsonb_set(
                jsonb_set(
                    COALESCE(credentials, '{}'::jsonb),
                    '{email}',
                    '"xai32131@163.com"'::jsonb,
                    true
                ),
                '{model_mapping,grok-4.6}',
                '"grok-4.6"'::jsonb,
                true
            ),
            '{model_mapping,grok}',
            '"grok-4.6"'::jsonb,
            true
        ),
        '{model_mapping,grok-latest}',
        '"grok-4.6"'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE platform = 'grok'
  AND deleted_at IS NULL
  AND (
        lower(name) IN ('grok-1', 'grok1', 'grok 1')
        OR credentials->>'email' = 'xai32131@163.com'
        OR extra->>'email' = 'xai32131@163.com'
        OR notes ILIKE '%xai32131@163.com%'
      )
  AND lower(coalesce(name, '')) NOT IN ('grok-heavy', 'grokheavy', 'grok heavy')
  AND coalesce(credentials->>'email', '') <> 'mci777686@outlook.com'
  AND coalesce(extra->>'email', '') <> 'mci777686@outlook.com';
