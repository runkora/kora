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

// Sources of a projected row: built through the API or projected from a resource.
const (
	SourceAPI = "api"
	SourceCRD = "crd"
)

// PlatformTenant is the tenant the operator sends for platform-wide kinds such
// as VirtualModel. It is not a valid tenant name, so it cannot collide with one.
const PlatformTenant = "_platform"

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
	Namespace  string          `json:"namespace"`
	Name       string          `json:"name"`
	UID        string          `json:"uid"`
	Generation int64           `json:"generation"`
	Spec       json.RawMessage `json:"spec"`
	// Refs holds resolved ids keyed by RefEnvironment, RefAgent and RefSource.
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

// RefEnvironment is the Item.Refs key holding the id of the Environment name.
func RefEnvironment(name string) string { return "environment/" + name }

// RefAgent is the Item.Refs key holding the id of the Agent name.
func RefAgent(name string) string { return "agent/" + name }

// RefSource is the Item.Refs key holding the id of the TriggerSource name.
func RefSource(name string) string { return "source/" + name }
