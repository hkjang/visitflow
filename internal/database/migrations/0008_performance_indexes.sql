-- Indexes for the access paths the application actually uses: keyset paging of
-- the visit directory, per-participant notification maintenance, visitor
-- history aggregation, department-scoped approval lists and audit prefix search.
CREATE INDEX IF NOT EXISTS visits_start_keyset_idx ON visits(start_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS visits_department_idx ON visits(department_id, start_at DESC) WHERE department_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS visitor_visits_visitor_idx ON visitor_visits(visitor_id);
CREATE INDEX IF NOT EXISTS notifications_participant_idx ON notifications(visitor_visit_id) WHERE visitor_visit_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notifications_rule_idx ON notifications(rule_id) WHERE rule_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notifications_api_idx ON notifications(api_config_id) WHERE api_config_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS consent_records_participant_idx ON consent_records(visitor_visit_id, consented_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs(action text_pattern_ops, id DESC);
CREATE INDEX IF NOT EXISTS qr_tokens_active_lookup_idx ON qr_tokens(visitor_visit_id, issued_at DESC) WHERE revoked_at IS NULL;
