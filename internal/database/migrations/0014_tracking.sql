-- Visit tracking snippet: an administrator pastes or picks an analytics
-- tracker in the settings screen and the served pages carry it under a
-- nonce-based content security policy. Off by default, so an existing
-- installation serves exactly the same pages as before.
INSERT INTO settings(key, value, secret) VALUES
 ('tracking.enabled', 'false', false),
 ('tracking.provider', 'none', false),
 ('tracking.momento_url', '', false),
 ('tracking.momento_site_id', '', false),
 ('tracking.momento_environment', 'prd', false),
 ('tracking.momento_proxy', 'true', false),
 ('tracking.measurement_id', '', false),
 ('tracking.matomo_url', '', false),
 ('tracking.matomo_site_id', '', false),
 ('tracking.custom_snippet', '', false),
 ('tracking.allowed_hosts', '', false),
 ('tracking.include_admin', 'false', false),
 ('tracking.placement', 'head', false)
ON CONFLICT (key) DO NOTHING;
