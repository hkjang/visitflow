-- MCP over SSO: /mcp may also be opened with a Keycloak access token, so an
-- MCP client needs nothing but the URL. Personal keys keep working unchanged.
-- Off by default, so an existing installation behaves exactly as before.
INSERT INTO settings(key, value, secret) VALUES
 ('mcp.oauth.enabled', 'false', false),
 ('mcp.oauth.resource', '', false),
 ('mcp.oauth.audience', '', false),
 ('mcp.oauth.scopes', 'read mcp', false)
ON CONFLICT (key) DO NOTHING;
