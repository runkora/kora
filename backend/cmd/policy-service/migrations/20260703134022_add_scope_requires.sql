-- requires lists other scopes a scope depends on. Holding the scope in a grant
-- additionally confers the required scopes in the compiled effective set (not in
-- the stored grant). Used by first-party platform MCP tools whose
-- mcp:<server>:<tool>:use scope needs the platform scope the tool calls upstream
-- (e.g. mcp:orbit:agents:list:use requires agents:types:read). Default empty.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE policy_scope_vocabulary ADD COLUMN requires TEXT[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE policy_scope_vocabulary DROP COLUMN requires;
-- +goose StatementEnd
