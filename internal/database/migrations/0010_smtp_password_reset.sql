-- On-premises SMTP relay and e-mail based password reset for local accounts.
INSERT INTO settings(key, value, secret) VALUES
 ('smtp.enabled', 'false', false),
 ('smtp.host', '', false),
 ('smtp.port', '587', false),
 ('smtp.security', 'starttls', false),
 ('smtp.username', '', false),
 ('smtp.password', '', true),
 ('smtp.from', '', false),
 ('smtp.skip_tls_verify', 'false', false),
 ('auth.password_reset_enabled', 'true', false),
 ('auth.password_reset_minutes', '30', false)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS password_resets (
  token_hash bytea PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  requested_by text REFERENCES users(id) ON DELETE SET NULL,
  ip_address text,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS password_resets_user_idx ON password_resets(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS password_resets_expiry_idx ON password_resets(expires_at);

-- Per-user e-mail alert preferences; an empty object means the defaults apply.
ALTER TABLE users ADD COLUMN IF NOT EXISTS mail_preferences jsonb NOT NULL DEFAULT '{}';
