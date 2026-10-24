package declarative

import (
	"context"
	"encoding/json"
)

// Kinds, as they appear in the endpoint paths. Each is the CRD's plural name.
const (
	KindVirtualModels  = "virtualmodels"
	KindEnvironments   = "environments"
	KindAgents         = "agents"
	KindTriggerSources = "triggersources"
	KindTriggers       = "triggers"
	KindPolicyRoles    = "policyroles"
)

// Outcomes a service reports for an applied item.
const (
	// OutcomeProjected means the current spec is applied. Ready says whether
	// the item is live.
	OutcomeProjected = "Projected"
	// OutcomeNameConflict means an item built through the API holds the key.
	OutcomeNameConflict = "NameConflict"
	// OutcomeInvalid means the spec fails the API's validation. Message says why.
	OutcomeInvalid = "Invalid"
	// OutcomePending means the service cannot apply the spec yet, such as a
	// scope missing from the vocabulary. The operator retries.
	OutcomePending = "Pending"
)

// Item is one declared resource the operator asks a service to apply.
type Item struct {
	TenantID   string          `json:"tenant_id"`
	Name       string          `json:"name"`
	UID        string          `json:"uid"`
	Generation int64           `json:"generation"`
	Spec       json.RawMessage `json:"spec"`
	// Refs holds resolved ids, such as an agent name to its agent id.
	Refs map[string]string `json:"refs,omitempty"`
	// Secret is the TriggerSource signing secret. Other kinds leave it empty.
	Secret []byte `json:"secret,omitempty"`
}

// Result is the service's outcome. The operator writes it to the resource's status.
type Result struct {
	Outcome string `json:"outcome"`
	Message string `json:"message,omitempty"`
	ID      string `json:"id,omitempty"`
	Ready   bool   `json:"ready"`
}

// Declared is one item a service holds for a resource.
type Declared struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// Applier is implemented by each owning service once per kind. Apply is
// idempotent. Delete of an item that does not exist succeeds.
type Applier interface {
	Apply(ctx context.Context, item Item) (Result, error)
	Delete(ctx context.Context, tenantID, name string) error
	List(ctx context.Context, tenantID string) ([]Declared, error)
}
