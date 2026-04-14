package policygw

// CheckRequest is the body for POST /v1/policy/check.
type CheckRequest struct {
	// Token is the raw platform JWT for the principal being checked.
	Token string `json:"token"`
	// DelegationToken is the optional delegation token (RFC 8693).
	DelegationToken string `json:"delegation_token,omitempty"`
	// Resource describes the resource being accessed.
	Resource CheckResource `json:"resource"`
	// Action is the operation being attempted (e.g. "invoke", "read", "write").
	Action string `json:"action"`
}

// CheckResource describes a resource in a check request.
type CheckResource struct {
	Type         string            `json:"type"`
	PathSegments []string          `json:"path,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// CheckResponse is the body for POST /v1/policy/check.
type CheckResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason"`
}

// QueryRequest is the body for POST /v1/policy/query.
// The calling service provides its full resource catalog as candidates;
// the policy gateway returns only those the principal is permitted to access.
type QueryRequest struct {
	Token           string           `json:"token"`
	DelegationToken string           `json:"delegation_token,omitempty"`
	ResourceType    string           `json:"resource_type"`
	Action          string           `json:"action"`
	Candidates      []QueryCandidate `json:"candidates"`
}

// QueryCandidate is a single resource the caller wants to check access for.
type QueryCandidate struct {
	ID           string            `json:"id"`
	PathSegments []string          `json:"path,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// QueryResponse is the body for POST /v1/policy/query.
type QueryResponse struct {
	Allowed []string `json:"allowed"`
}

// ScopesResponse is the body for GET /v1/policy/scopes.
type ScopesResponse struct {
	Scopes      []string `json:"scopes"`
	TenantID    string   `json:"tenant_id"`
	EvaluatedAt string   `json:"evaluated_at"`
}
