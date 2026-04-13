package api

// createGrantRequest is the request body for POST /policy/grants.
type createGrantRequest struct {
	TenantID      string   `json:"tenant_id"`
	PrincipalType string   `json:"principal_type"`
	PrincipalID   string   `json:"principal_id"`
	Scopes        []string `json:"scopes"`
}

// upsertRoleRequest is the request body for PUT /policy/roles.
type upsertRoleRequest struct {
	TenantID string   `json:"tenant_id"`
	Role     string   `json:"role"`
	Scopes   []string `json:"scopes"`
}
