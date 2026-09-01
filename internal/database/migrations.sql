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
ALTER TABLE users ADD COLUMN IF NOT EXISTS role_override boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject text;
CREATE UNIQUE INDEX IF NOT EXISTS users_oidc_identity_idx ON users(oidc_issuer, oidc_subject) WHERE oidc_subject IS NOT NULL;

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
  name_hash bytea,
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
ALTER TABLE visitors ADD COLUMN IF NOT EXISTS name_hash bytea;
ALTER TABLE visitors ADD COLUMN IF NOT EXISTS masked_at timestamptz;
CREATE INDEX IF NOT EXISTS visitors_phone_hash_idx ON visitors(phone_hash);
CREATE INDEX IF NOT EXISTS visitors_name_hash_idx ON visitors(name_hash);
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
WITH active_qr AS (
  SELECT id,row_number() OVER (PARTITION BY visitor_visit_id ORDER BY issued_at DESC,version DESC,id DESC) AS rn
  FROM qr_tokens
  WHERE revoked_at IS NULL
)
UPDATE qr_tokens q
SET revoked_at=now()
FROM active_qr a
WHERE q.id=a.id AND a.rn>1;
CREATE UNIQUE INDEX IF NOT EXISTS qr_tokens_one_active_per_participant_idx ON qr_tokens(visitor_visit_id) WHERE revoked_at IS NULL;

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
CREATE UNIQUE INDEX IF NOT EXISTS visit_templates_owner_key ON visit_templates(id, user_id);

CREATE TABLE IF NOT EXISTS frequent_visitors (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name_encrypted text NOT NULL,
  phone_encrypted text NOT NULL,
  phone_hash bytea NOT NULL,
  email_encrypted text,
  company text,
  title text,
  vehicle_encrypted text,
  equipment jsonb NOT NULL DEFAULT '[]',
  consented_at timestamptz NOT NULL,
  last_used_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id, phone_hash)
);
ALTER TABLE frequent_visitors ADD COLUMN IF NOT EXISTS last_used_at timestamptz;
UPDATE frequent_visitors SET last_used_at=COALESCE(updated_at,created_at,now()) WHERE last_used_at IS NULL;
ALTER TABLE frequent_visitors ALTER COLUMN last_used_at SET DEFAULT now();
ALTER TABLE frequent_visitors ALTER COLUMN last_used_at SET NOT NULL;
CREATE INDEX IF NOT EXISTS frequent_visitors_user_idx ON frequent_visitors(user_id, updated_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS frequent_visitors_owner_key ON frequent_visitors(id, user_id);
CREATE INDEX IF NOT EXISTS frequent_visitors_retention_idx ON frequent_visitors(last_used_at);

CREATE TABLE IF NOT EXISTS visit_template_frequent_visitors (
  user_id text NOT NULL,
  template_id text NOT NULL,
  frequent_visitor_id text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY(template_id, frequent_visitor_id),
  CONSTRAINT vtfv_user_fk FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT vtfv_template_owner_fk FOREIGN KEY(template_id, user_id) REFERENCES visit_templates(id, user_id) ON DELETE CASCADE,
  CONSTRAINT vtfv_visitor_owner_fk FOREIGN KEY(frequent_visitor_id, user_id) REFERENCES frequent_visitors(id, user_id) ON DELETE CASCADE
);
ALTER TABLE visit_template_frequent_visitors ADD COLUMN IF NOT EXISTS user_id text;
UPDATE visit_template_frequent_visitors link SET user_id=template.user_id
FROM visit_templates template WHERE template.id=link.template_id AND link.user_id IS NULL;
ALTER TABLE visit_template_frequent_visitors ALTER COLUMN user_id SET NOT NULL;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='vtfv_user_fk' AND conrelid='visit_template_frequent_visitors'::regclass) THEN
    ALTER TABLE visit_template_frequent_visitors ADD CONSTRAINT vtfv_user_fk FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='vtfv_template_owner_fk' AND conrelid='visit_template_frequent_visitors'::regclass) THEN
    ALTER TABLE visit_template_frequent_visitors ADD CONSTRAINT vtfv_template_owner_fk FOREIGN KEY(template_id,user_id) REFERENCES visit_templates(id,user_id) ON DELETE CASCADE;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='vtfv_visitor_owner_fk' AND conrelid='visit_template_frequent_visitors'::regclass) THEN
    ALTER TABLE visit_template_frequent_visitors ADD CONSTRAINT vtfv_visitor_owner_fk FOREIGN KEY(frequent_visitor_id,user_id) REFERENCES frequent_visitors(id,user_id) ON DELETE CASCADE;
  END IF;
END $$;
CREATE INDEX IF NOT EXISTS visit_template_frequent_visitors_visitor_idx ON visit_template_frequent_visitors(frequent_visitor_id);

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

ALTER TABLE qr_tokens ADD COLUMN IF NOT EXISTS qrcode_file_seq text;
UPDATE qr_tokens
SET qrcode_file_seq=substr(md5(id || random()::text || clock_timestamp()::text),1,24)
WHERE qrcode_file_seq IS NULL OR qrcode_file_seq='';
ALTER TABLE qr_tokens ALTER COLUMN qrcode_file_seq SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS qr_tokens_file_seq_idx ON qr_tokens(qrcode_file_seq);

CREATE TABLE IF NOT EXISTS notification_api_configs (
  id text PRIMARY KEY,
  name text NOT NULL,
  channel text NOT NULL CHECK (channel IN ('sms','mms','kakao')),
  base_url text NOT NULL,
  path text NOT NULL DEFAULT '',
  method text NOT NULL DEFAULT 'POST' CHECK (method IN ('GET','POST','PUT','PATCH')),
  request_format text NOT NULL DEFAULT 'json' CHECK (request_format IN ('json','form','query')),
  headers_encrypted text NOT NULL DEFAULT '',
  parameters_encrypted text NOT NULL,
  secret_keys jsonb NOT NULL DEFAULT '[]',
  timeout_seconds integer NOT NULL DEFAULT 10 CHECK (timeout_seconds BETWEEN 1 AND 60),
  enabled boolean NOT NULL DEFAULT true,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notification_api_configs_channel_idx ON notification_api_configs(channel, enabled, name);

CREATE TABLE IF NOT EXISTS notification_rules (
  id text PRIMARY KEY,
  name text NOT NULL,
  event text NOT NULL CHECK (event IN ('visit_confirmed','visit_start','checked_in','checked_out','visit_cancelled')),
  audience text NOT NULL CHECK (audience IN ('visitor','host')),
  channel text NOT NULL DEFAULT 'sms' CHECK (channel IN ('sms','mms','kakao')),
  api_config_id text REFERENCES notification_api_configs(id) ON DELETE SET NULL,
  offset_minutes integer NOT NULL DEFAULT 0 CHECK (offset_minutes BETWEEN -10080 AND 10080),
  template_key text NOT NULL,
  body_template text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notification_rules_event_idx ON notification_rules(event, enabled);

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS visitor_visit_id text REFERENCES visitor_visits(id) ON DELETE SET NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS rule_id text REFERENCES notification_rules(id) ON DELETE SET NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS api_config_id text REFERENCES notification_api_configs(id) ON DELETE SET NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS metadata_encrypted text NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS claimed_at timestamptz;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS claim_token text;
UPDATE notification_rules rule SET enabled=false,updated_at=now()
FROM notification_api_configs api
WHERE rule.api_config_id=api.id AND rule.enabled AND NOT api.enabled;
UPDATE notifications notification SET status='cancelled',
  attempts=CASE WHEN notification.status='sending' THEN GREATEST(notification.attempts-1,0) ELSE notification.attempts END,
  claimed_at=NULL,claim_token=NULL,error=NULL
WHERE notification.status IN ('queued','failed','sending') AND (
  EXISTS (SELECT 1 FROM notification_api_configs api WHERE api.id=notification.api_config_id AND NOT api.enabled) OR
  EXISTS (SELECT 1 FROM notification_rules rule WHERE rule.id=notification.rule_id AND NOT rule.enabled)
);
CREATE INDEX IF NOT EXISTS notifications_visit_schedule_idx ON notifications(visit_id, status, next_attempt_at);

CREATE TABLE IF NOT EXISTS guide_posts (
  id text PRIMARY KEY,
  title text NOT NULL,
  category text NOT NULL DEFAULT '일반',
  content text NOT NULL,
  published boolean NOT NULL DEFAULT false,
  pinned boolean NOT NULL DEFAULT false,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  updated_by text REFERENCES users(id) ON DELETE SET NULL,
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS guide_posts_list_idx ON guide_posts(published, pinned DESC, updated_at DESC);

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
 ('security.api_key_allowed_scopes', 'read write mcp', false),
 ('security.api_key_max_active', '10', false),
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

INSERT INTO notification_rules(id,name,event,audience,channel,offset_minutes,template_key,body_template,enabled)
SELECT '00000000-0000-4000-8000-000000000101','방문 확정 시 방문증 안내','visit_confirmed','visitor','sms',0,'visitor_pass',value,true
FROM settings WHERE key='notification.visitor_template'
AND NOT EXISTS (SELECT 1 FROM schema_migrations WHERE version=4)
ON CONFLICT (id) DO NOTHING;
INSERT INTO notification_rules(id,name,event,audience,channel,offset_minutes,template_key,body_template,enabled)
SELECT '00000000-0000-4000-8000-000000000102','체크인 시 담당자 도착 안내','checked_in','host','sms',0,'host_arrival',value,true
FROM settings WHERE key='notification.arrival_template'
AND NOT EXISTS (SELECT 1 FROM schema_migrations WHERE version=4)
ON CONFLICT (id) DO NOTHING;

INSERT INTO schema_migrations(version) VALUES (3) ON CONFLICT DO NOTHING;
INSERT INTO schema_migrations(version) VALUES (4) ON CONFLICT DO NOTHING;
