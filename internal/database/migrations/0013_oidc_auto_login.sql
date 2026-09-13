-- Silent SSO: when the identity provider already holds a session, the browser
-- may sign in with prompt=none before ever showing the login screen. Off by
-- default, so an existing installation behaves exactly as before.
INSERT INTO settings(key, value, secret) VALUES
 ('oidc.auto_login', 'false', false)
ON CONFLICT (key) DO NOTHING;

-- The callback must know whether a provider error answers a silent attempt,
-- because login_required is then an ordinary outcome rather than a failure.
ALTER TABLE oidc_states ADD COLUMN IF NOT EXISTS silent boolean NOT NULL DEFAULT false;
