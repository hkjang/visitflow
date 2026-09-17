-- Mail settings take the company-wide MAIL-STANDARD names (mail.enabled,
-- mail.smtp_host, ...) so every service is configured the same way. An
-- installation that already pointed at a relay keeps its values; one that never
-- did takes the standard defaults — port 25, security auto, no authentication —
-- which is what an on-premises relay usually wants, instead of the old
-- 587/starttls defaults.
INSERT INTO settings(key, value, secret) VALUES
 ('mail.enabled', 'false', false),
 ('mail.smtp_host', '', false),
 ('mail.smtp_port', '25', false),
 ('mail.security', 'auto', false),
 ('mail.skip_tls_verify', 'false', false),
 ('mail.username', '', false),
 ('mail.password', '', true),
 ('mail.from_address', '', false),
 ('mail.from_name', '', false),
 ('mail.base_url', '', false),
 ('mail.timeout_seconds', '10', false),
 ('mail.notify_approval_pending', 'true', false),
 ('mail.notify_approval_escalated', 'true', false),
 ('mail.notify_visit_confirmed', 'true', false),
 ('mail.notify_visit_rejected', 'true', false),
 ('mail.notify_visit_cancelled', 'true', false),
 ('mail.notify_checked_in', 'true', false),
 ('mail.notify_checked_out', 'true', false)
ON CONFLICT (key) DO NOTHING;

-- Carry the old smtp.* values over only when a relay host was entered; the
-- encrypted password ciphertext is copied as-is because both rows are sealed
-- with the same installation key.
UPDATE settings m SET value=o.value
FROM settings o
WHERE EXISTS (SELECT 1 FROM settings h WHERE h.key='smtp.host' AND h.value<>'')
  AND m.key=CASE o.key
    WHEN 'smtp.enabled' THEN 'mail.enabled'
    WHEN 'smtp.host' THEN 'mail.smtp_host'
    WHEN 'smtp.port' THEN 'mail.smtp_port'
    WHEN 'smtp.security' THEN 'mail.security'
    WHEN 'smtp.skip_tls_verify' THEN 'mail.skip_tls_verify'
    WHEN 'smtp.username' THEN 'mail.username'
    WHEN 'smtp.password' THEN 'mail.password'
  END;

-- smtp.from held "Name <address>" in one value; the standard keeps them apart.
UPDATE settings m SET value=CASE m.key
    WHEN 'mail.from_address' THEN CASE WHEN o.value LIKE '%<%>%' THEN substring(o.value from '<([^>]+)>') ELSE o.value END
    WHEN 'mail.from_name' THEN CASE WHEN o.value LIKE '%<%>%' THEN btrim(split_part(o.value, '<', 1), ' "') ELSE '' END
  END
FROM settings o
WHERE o.key='smtp.from' AND o.value<>'' AND m.key IN ('mail.from_address','mail.from_name')
  AND EXISTS (SELECT 1 FROM settings h WHERE h.key='smtp.host' AND h.value<>'');

DELETE FROM settings WHERE key LIKE 'smtp.%';
