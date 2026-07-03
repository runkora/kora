-- mcp_tools records the governed MCP tools an agent type may call through the
-- tool-gateway, as mcp:<server>:<tool>:use scopes. A first-class field (like
-- connectors) the backend composes into EffectiveGrantScopes; the coarse
-- mcp:invoke gate is derived at composition time. Default empty so existing rows
-- have no MCP tool access.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE agent_types ADD COLUMN mcp_tools TEXT[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agent_types DROP COLUMN mcp_tools;
-- +goose StatementEnd
