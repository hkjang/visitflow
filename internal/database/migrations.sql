CREATE TABLE IF NOT EXISTS schema_migrations (
  version integer PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS settings (
  key text PRIMARY KEY,
  value text NOT NULL DEFAULT '',
  secret boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by text
);

CREATE TABLE IF NOT EXISTS organizations (
  id text PRIMARY KEY,
  external_id text UNIQUE,
  name text NOT NULL,
  parent_id text REFERENCES organizations(id) ON DELETE SET NULL,
  color text NOT NULL DEFAULT '#2F6B5F',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS employees (
  id text PRIMARY KEY,
  employee_no text NOT NULL UNIQUE,
  name text NOT NULL,
  email text,
  organization_id text REFERENCES organizations(id) ON DELETE SET NULL,
  title text,
  position text,
  workplace text,
  profile_image_url text,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','leave','retired')),
  attributes jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id text PRIMARY KEY,
  username text NOT NULL UNIQUE,
  password_hash text,
  display_name text NOT NULL,
  email text,
  employee_id text REFERENCES employees(id) ON DELETE SET NULL,
  role text NOT NULL DEFAULT 'user',
  source text NOT NULL DEFAULT 'local' CHECK (source IN ('local','oidc')),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz
);

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
UPDATE users SET role=CASE role
  WHEN 'employee' THEN 'user'
  WHEN 'department_manager' THEN 'dept_manager'
  WHEN 'seat_manager' THEN 'lobby'
  WHEN 'system_admin' THEN 'super_admin'
  ELSE role END;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='users_role_check' AND conrelid='users'::regclass) THEN
    ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('user','lobby','dept_manager','security','auditor','admin','super_admin'));
  END IF;
END $$;

ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_encrypted text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS department_id text REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS site_scope text[] NOT NULL DEFAULT ARRAY[]::text[];

CREATE TABLE IF NOT EXISTS sessions (
  token_hash bytea PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  ip_address text,
  user_agent text
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS oidc_states (
  state_hash bytea PRIMARY KEY,
  nonce text NOT NULL,
  verifier text NOT NULL,
  return_to text NOT NULL DEFAULT '/',
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name text NOT NULL,
  prefix text NOT NULL,
  secret_hash bytea NOT NULL UNIQUE,
  scopes text[] NOT NULL DEFAULT ARRAY['read'],
  version integer NOT NULL DEFAULT 1,
  rotated_from text REFERENCES api_keys(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  grace_until timestamptz
);
CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS sites (
  id text PRIMARY KEY,
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  address text,
  map_url text,
  timezone text NOT NULL DEFAULT 'Asia/Seoul',
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lobbies (
  id text PRIMARY KEY,
  site_id text NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  code text NOT NULL,
  name text NOT NULL,
  instructions text,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id, code)
);

CREATE TABLE IF NOT EXISTS visitors (
  id text PRIMARY KEY,
  name_encrypted text NOT NULL,
  phone_encrypted text NOT NULL,
  phone_hash bytea,
  email_encrypted text,
  company text,
  title text,
  vehicle_encrypted text,
  consented_at timestamptz,
  erased_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS visitors_phone_hash_idx ON visitors(phone_hash);
CREATE INDEX IF NOT EXISTS visitors_company_idx ON visitors(lower(company));

CREATE TABLE IF NOT EXISTS visits (
  id text PRIMARY KEY,
  request_no text NOT NULL UNIQUE,
  host_user_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  department_id text REFERENCES organizations(id) ON DELETE SET NULL,
  site_id text NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
  lobby_id text REFERENCES lobbies(id) ON DELETE SET NULL,
  start_at timestamptz NOT NULL,
  end_at timestamptz NOT NULL,
  purpose text NOT NULL,
  place_detail text,
  notes text,
  status text NOT NULL CHECK (status IN ('REQUESTED','PENDING_APPROVAL','APPROVED','SCHEDULED','ARRIVED','CHECKED_IN','CHECKED_OUT','CANCELLED','REJECTED','NO_SHOW')),
  source text NOT NULL DEFAULT 'employee' CHECK (source IN ('employee','lobby','api','mcp','import')),
  approval_reason text,
  approved_by text REFERENCES users(id) ON DELETE SET NULL,
  approved_at timestamptz,
  recurrence jsonb NOT NULL DEFAULT '{}',
  policy_snapshot jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  cancelled_at timestamptz,
  CHECK (end_at > start_at)
);
CREATE INDEX IF NOT EXISTS visits_host_idx ON visits(host_user_id, start_at DESC);
CREATE INDEX IF NOT EXISTS visits_site_time_idx ON visits(site_id, start_at);
CREATE INDEX IF NOT EXISTS visits_status_idx ON visits(status, start_at);

CREATE TABLE IF NOT EXISTS visitor_visits (
  id text PRIMARY KEY,
  visit_id text NOT NULL REFERENCES visits(id) ON DELETE CASCADE,
  visitor_id text NOT NULL REFERENCES visitors(id) ON DELETE RESTRICT,
  is_primary boolean NOT NULL DEFAULT false,
  equipment jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'SCHEDULED' CHECK (status IN ('PENDING_APPROVAL','SCHEDULED','ARRIVED','CHECKED_IN','CHECKED_OUT','CANCELLED','REJECTED','NO_SHOW')),
  badge_no text,
  checked_in_at timestamptz,
  checked_out_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(visit_id, visitor_id)
);
CREATE INDEX IF NOT EXISTS visitor_visits_visit_idx ON visitor_visits(visit_id);
CREATE INDEX IF NOT EXISTS visitor_visits_status_idx ON visitor_visits(status);

CREATE TABLE IF NOT EXISTS qr_tokens (
  id text PRIMARY KEY,
  visitor_visit_id text NOT NULL REFERENCES visitor_visits(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  token_encrypted text NOT NULL,
  prefix text NOT NULL,
  version integer NOT NULL DEFAULT 1,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  issued_at timestamptz NOT NULL DEFAULT now(),
  used_at timestamptz,
  revoked_at timestamptz,
  rotated_from text REFERENCES qr_tokens(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS qr_tokens_participant_idx ON qr_tokens(visitor_visit_id, issued_at DESC);

CREATE TABLE IF NOT EXISTS visit_events (
  id bigserial PRIMARY KEY,
  visit_id text NOT NULL REFERENCES visits(id) ON DELETE CASCADE,
  visitor_visit_id text REFERENCES visitor_visits(id) ON DELETE SET NULL,
  event_type text NOT NULL,
  actor_user_id text REFERENCES users(id) ON DELETE SET NULL,
  lobby_id text REFERENCES lobbies(id) ON DELETE SET NULL,
  method text,
  details jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS visit_events_visit_idx ON visit_events(visit_id, created_at DESC);

CREATE TABLE IF NOT EXISTS notifications (
  id text PRIMARY KEY,
  visit_id text REFERENCES visits(id) ON DELETE SET NULL,
  recipient_encrypted text NOT NULL,
  channel text NOT NULL DEFAULT 'sms',
  template_key text NOT NULL,
  body_encrypted text NOT NULL,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sending','sent','logged','failed','cancelled')),
  attempts integer NOT NULL DEFAULT 0,
  error text,
  provider_message_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_queue_idx ON notifications(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS visit_templates (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name text NOT NULL,
  payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watchlist_entries (
  id text PRIMARY KEY,
  name_encrypted text,
  phone_hash bytea,
  company text,
  reason_encrypted text NOT NULL,
  starts_at timestamptz NOT NULL DEFAULT now(),
  ends_at timestamptz,
  active boolean NOT NULL DEFAULT true,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS watchlist_phone_idx ON watchlist_entries(phone_hash) WHERE active;

CREATE TABLE IF NOT EXISTS audit_logs (
  id bigserial PRIMARY KEY,
  actor_user_id text REFERENCES users(id) ON DELETE SET NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id text,
  ip_address text,
  details jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_logs_created_idx ON audit_logs(created_at DESC);

INSERT INTO sites(id, code, name, address)
VALUES ('00000000-0000-4000-8000-000000000001', 'HQ', '본사', '주소를 관리자 설정에서 입력하세요')
ON CONFLICT (code) DO NOTHING;
INSERT INTO lobbies(id, site_id, code, name, instructions)
SELECT '00000000-0000-4000-8000-000000000002', id, 'MAIN', '1층 안내데스크', 'QR 방문증을 준비해 주세요.'
FROM sites WHERE code='HQ' ON CONFLICT (site_id, code) DO NOTHING;

INSERT INTO settings(key, value, secret) VALUES
 ('general.service_name', 'VisitFlow', false),
 ('general.company_name', '', false),
 ('general.base_url', '', false),
 ('general.default_locale', 'ko-KR', false),
 ('auth.local_enabled', 'true', false),
 ('oidc.enabled', 'false', false),
 ('oidc.issuer_url', '', false),
 ('oidc.client_id', '', false),
 ('oidc.client_secret', '', true),
 ('oidc.scopes', 'openid profile email groups', false),
 ('oidc.admin_group', '/visitflow-admins', false),
 ('oidc.lobby_group', '/visitflow-lobby', false),
 ('oidc.security_group', '/visitflow-security', false),
 ('oidc.auditor_group', '/visitflow-auditors', false),
 ('oidc.department_manager_group', '/visitflow-department-managers', false),
 ('oidc.auto_provision', 'true', false),
 ('security.session_hours', '8', false),
 ('security.api_key_days', '90', false),
 ('security.rotation_grace_hours', '24', false),
 ('visit.approval_enabled', 'false', false),
 ('visit.early_checkin_minutes', '60', false),
 ('visit.late_grace_minutes', '120', false),
 ('visit.single_use_qr', 'true', false),
 ('visit.dynamic_qr_seconds', '0', false),
 ('visit.auto_checkout_hour', '23', false),
 ('visit.company_required', 'false', false),
 ('notification.provider', 'log', false),
 ('notification.webhook_url', '', false),
 ('notification.auth_header', '', true),
 ('notification.visitor_template', '[VisitFlow] {{company}} 방문: {{start}} / {{place}} / 모바일 방문증 {{passUrl}}', false),
 ('notification.arrival_template', '[방문자 도착] {{visitor}} 님이 {{lobby}}에 도착했습니다. 체크인 {{checkedIn}}', false),
 ('privacy.mask_after_days', '90', false),
 ('privacy.destroy_after_days', '365', false),
 ('privacy.audit_retention_days', '730', false)
ON CONFLICT (key) DO UPDATE SET value=CASE WHEN settings.value='SeatOn' AND EXCLUDED.key='general.service_name' THEN EXCLUDED.value ELSE settings.value END;

INSERT INTO schema_migrations(version) VALUES (2) ON CONFLICT DO NOTHING;
