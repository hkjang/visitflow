-- Rule-driven e-mail: visitors and hosts receive the same event notifications
-- by mail through the configured SMTP relay, with an administrator-authored
-- subject beside the body template.
ALTER TABLE notification_rules DROP CONSTRAINT IF EXISTS notification_rules_channel_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_channel_check
  CHECK (channel IN ('sms','mms','kakao','webhook','email'));
ALTER TABLE notification_rules ADD COLUMN IF NOT EXISTS subject_template text NOT NULL DEFAULT '';
