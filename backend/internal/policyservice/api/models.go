package api

// createGrantRequest is the request body for POST /policy/grants.
type createGrantRequest struct {
	TenantID      string   `json:"tenant_id"`
	PrincipalType string   `json:"principal_type"`
	PrincipalID   string   `json:"principal_id"`
	Scopes        []string `json:"scopes"`
}

// registerScopeRequest is the request body for POST /policy/vocabulary.
type registerScopeRequest struct {
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	PathSegments []string `json:"path"`
	Action       string   `json:"action"`
	Description  string   `json:"description"`
}
