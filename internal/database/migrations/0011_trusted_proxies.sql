-- Forwarded client addresses are only believed when the request arrives from a
-- listed reverse proxy. An empty value keeps the peer address, which is the safe
-- default for a service exposed directly.
INSERT INTO settings(key, value, secret) VALUES
 ('security.trusted_proxies', '', false)
ON CONFLICT (key) DO NOTHING;
