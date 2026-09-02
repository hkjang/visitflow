-- Hosts need to hear about rejections, and lobby staff need to record a
-- check-in that happened without a QR (dead phone, lost link) with a reason.
ALTER TABLE notification_rules DROP CONSTRAINT IF EXISTS notification_rules_event_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_event_check
  CHECK (event IN ('visit_confirmed','visit_start','checked_in','checked_out','visit_cancelled','visit_rejected','approval_escalated'));

CREATE INDEX IF NOT EXISTS visits_series_idx ON visits((recurrence->>'seriesId'), start_at) WHERE recurrence ? 'seriesId';
