-- Login and public endpoint throttling. Persisting the counters keeps a lockout
-- effective across restarts and across every node sharing the database.
CREATE TABLE IF NOT EXISTS auth_throttle (
  key text PRIMARY KEY,
  failures integer NOT NULL DEFAULT 0,
  first_failure_at timestamptz NOT NULL DEFAULT now(),
  last_failure_at timestamptz NOT NULL DEFAULT now(),
  locked_until timestamptz
);
CREATE INDEX IF NOT EXISTS auth_throttle_locked_idx ON auth_throttle(locked_until);

-- Consent is recorded as an event with who asserted it, from where, and against
-- which policy version, instead of only a boolean on the visitor row.
CREATE TABLE IF NOT EXISTS consent_records (
  id text PRIMARY KEY,
  visitor_id text REFERENCES visitors(id) ON DELETE SET NULL,
  visit_id text REFERENCES visits(id) ON DELETE SET NULL,
  visitor_visit_id text REFERENCES visitor_visits(id) ON DELETE SET NULL,
  source text NOT NULL CHECK (source IN ('host','self','import','lobby','api','mcp')),
  policy_version text NOT NULL DEFAULT '',
  locale text NOT NULL DEFAULT '',
  actor_user_id text REFERENCES users(id) ON DELETE SET NULL,
  ip_address text,
  user_agent text,
  consented_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS consent_records_visitor_idx ON consent_records(visitor_id, consented_at DESC);
CREATE INDEX IF NOT EXISTS consent_records_visit_idx ON consent_records(visit_id);

-- Visitor self pre-registration links.
CREATE TABLE IF NOT EXISTS registration_invitations (
  id text PRIMARY KEY,
  visitor_visit_id text NOT NULL REFERENCES visitor_visits(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  expires_at timestamptz NOT NULL,
  completed_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS registration_invitations_open_idx
  ON registration_invitations(visitor_visit_id) WHERE completed_at IS NULL AND revoked_at IS NULL;

ALTER TABLE visitors ADD COLUMN IF NOT EXISTS locale text NOT NULL DEFAULT '';

-- Visit types carry the site's compliance checklist (NDA, safety briefing,
-- vehicle and equipment declarations) and can force the approval workflow.
CREATE TABLE IF NOT EXISTS visit_types (
  id text PRIMARY KEY,
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  requires_nda boolean NOT NULL DEFAULT false,
  requires_safety_briefing boolean NOT NULL DEFAULT false,
  requires_vehicle boolean NOT NULL DEFAULT false,
  requires_equipment boolean NOT NULL DEFAULT false,
  requires_approval boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS visit_types_active_idx ON visit_types(active, sort_order, name);

ALTER TABLE visits ADD COLUMN IF NOT EXISTS visit_type_id text REFERENCES visit_types(id) ON DELETE SET NULL;
ALTER TABLE visits ADD COLUMN IF NOT EXISTS checklist jsonb NOT NULL DEFAULT '{}';
ALTER TABLE visits ADD COLUMN IF NOT EXISTS escalated_at timestamptz;
CREATE INDEX IF NOT EXISTS visits_pending_escalation_idx ON visits(status, created_at) WHERE status='PENDING_APPROVAL';

INSERT INTO visit_types(id,code,name,description,requires_nda,requires_safety_briefing,requires_approval,sort_order) VALUES
 ('00000000-0000-4000-8000-000000000201','GENERAL','일반 방문','회의·상담 등 일반 업무 방문',false,false,false,10),
 ('00000000-0000-4000-8000-000000000202','CONTRACTOR','협력사 작업','현장 작업·설비 점검. 보안서약과 안전교육 확인이 필요합니다.',true,true,true,20)
ON CONFLICT (code) DO NOTHING;

-- Host delegation: an absent host can hand approvals and arrival notifications
-- to a colleague for a bounded period.
ALTER TABLE users ADD COLUMN IF NOT EXISTS delegate_user_id text REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS delegate_until timestamptz;
CREATE INDEX IF NOT EXISTS users_delegate_idx ON users(delegate_user_id) WHERE delegate_user_id IS NOT NULL;

-- Lobby kiosk devices authenticate with a long-lived device token limited to the
-- lobby endpoints of one site.
CREATE TABLE IF NOT EXISTS kiosk_devices (
  id text PRIMARY KEY,
  name text NOT NULL,
  site_id text NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  lobby_id text REFERENCES lobbies(id) ON DELETE SET NULL,
  token_hash bytea NOT NULL UNIQUE,
  prefix text NOT NULL,
  created_by text REFERENCES users(id) ON DELETE SET NULL,
  expires_at timestamptz,
  last_seen_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS kiosk_devices_site_idx ON kiosk_devices(site_id, created_at DESC);

-- Outbound integrations reuse the configurable API adapter: a 'webhook' channel
-- with the 'system' audience drives gates, guest Wi-Fi and other systems.
ALTER TABLE notification_api_configs DROP CONSTRAINT IF EXISTS notification_api_configs_channel_check;
ALTER TABLE notification_api_configs ADD CONSTRAINT notification_api_configs_channel_check
  CHECK (channel IN ('sms','mms','kakao','webhook'));
ALTER TABLE notification_rules DROP CONSTRAINT IF EXISTS notification_rules_channel_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_channel_check
  CHECK (channel IN ('sms','mms','kakao','webhook'));
ALTER TABLE notification_rules DROP CONSTRAINT IF EXISTS notification_rules_audience_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_audience_check
  CHECK (audience IN ('visitor','host','system'));
ALTER TABLE notification_rules DROP CONSTRAINT IF EXISTS notification_rules_event_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_event_check
  CHECK (event IN ('visit_confirmed','visit_start','checked_in','checked_out','visit_cancelled','approval_escalated'));
ALTER TABLE notification_rules ADD COLUMN IF NOT EXISTS locale text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS notification_rules_locale_idx ON notification_rules(event, enabled, locale);

INSERT INTO settings(key, value, secret) VALUES
 ('general.supported_locales', 'ko en', false),
 ('security.login_max_attempts', '5', false),
 ('security.login_lockout_minutes', '15', false),
 ('security.public_rate_limit_per_minute', '60', false),
 ('security.metrics_token', '', true),
 ('visit.approval_escalation_hours', '24', false),
 ('visit.self_registration_enabled', 'true', false),
 ('visit.self_registration_hours', '72', false),
 ('privacy.consent_policy_version', '1.0', false)
ON CONFLICT (key) DO NOTHING;
