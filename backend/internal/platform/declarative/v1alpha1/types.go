// Package v1alpha1 holds the declarative resource kinds: VirtualModel,
// Environment, Agent, TriggerSource, Trigger and PolicyRole. Each owning
// service projects its kinds into its own tables; the kora-crds chart ships
// their CRDs.
//
// See docs/adr/2026-10-24-declarative-resources.md.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version of the declarative kinds.
var GroupVersion = schema.GroupVersion{Group: "runkora.io", Version: "v1alpha1"}

// AddToScheme registers the six kinds and their lists.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion,
		&VirtualModel{}, &VirtualModelList{},
		&Environment{}, &EnvironmentList{},
		&Agent{}, &AgentList{},
		&TriggerSource{}, &TriggerSourceList{},
		&Trigger{}, &TriggerList{},
		&PolicyRole{}, &PolicyRoleList{},
	)
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// Status is the status every declarative kind reports. The operator writes it
// from the owning service's response.
type Status struct {
	// ID is the id of the item the resource is projected to.
	ID                 string             `json:"id,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// ModelRef names a VirtualModel by its family and name.
type ModelRef struct {
	Family string `json:"family"`
	Name   string `json:"name"`
}

// SecretKeyRef names one key of a Secret in the resource's namespace.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// Connector attaches a Connection and the actions the agent may invoke
// through it. Resources narrows it to upstream resources; empty means all.
type Connector struct {
	Connection string   `json:"connection"`
	Actions    []string `json:"actions"`
	Resources  []string `json:"resources,omitempty"`
}

// ─── VirtualModel ─────────────────────────────────────────────────────────────

// VirtualModel is a platform-wide catalog model. It lives in the system
// namespace and inference-gateway owns it.
type VirtualModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VirtualModelSpec `json:"spec"`
	Status Status           `json:"status,omitzero"`
}

// VirtualModelSpec mirrors the virtual model create request. Enabled
// defaults to true in the CRD.
type VirtualModelSpec struct {
	Family      string              `json:"family"`
	Name        string              `json:"name"`
	DisplayName string              `json:"displayName"`
	Description string              `json:"description,omitempty"`
	Strategy    string              `json:"strategy"`
	Enabled     bool                `json:"enabled"`
	Routes      []VirtualModelRoute `json:"routes,omitempty"`
}

// VirtualModelRoute sends the model to one provider model. Enabled defaults
// to true in the CRD.
type VirtualModelRoute struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelID"`
	Priority int32  `json:"priority"`
	Weight   int32  `json:"weight,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// VirtualModelList is a list of VirtualModel resources.
type VirtualModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []VirtualModel `json:"items"`
}

// ─── Environment ──────────────────────────────────────────────────────────────

// Environment is a sandbox environment for the tenant whose namespace holds
// it. Its metadata.name is the environment slug.
type Environment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   EnvironmentSpec `json:"spec"`
	Status Status          `json:"status,omitzero"`
}

// EnvironmentSpec mirrors the environment create request.
type EnvironmentSpec struct {
	DisplayName  string          `json:"displayName"`
	Description  string          `json:"description,omitempty"`
	SandboxImage string          `json:"sandboxImage,omitempty"`
	Size         EnvironmentSize `json:"size,omitzero"`
	EgressHosts  []string        `json:"egressHosts,omitempty"`
	Connectors   []Connector     `json:"connectors,omitempty"`
}

// EnvironmentSize holds Kubernetes quantities. An empty one takes the install
// default.
type EnvironmentSize struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Disk   string `json:"disk,omitempty"`
}

// EnvironmentList is a list of Environment resources.
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Environment `json:"items"`
}

// ─── Agent ────────────────────────────────────────────────────────────────────

// Agent is an agent type for the tenant whose namespace holds it. Its
// metadata.name is the agent slug.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   AgentSpec `json:"spec"`
	Status Status    `json:"status,omitzero"`
}

// AgentSpec mirrors the agent create request. Scopes is the API's
// grant_scopes. Framework, IdentityMode and Connectivity.Mode have CRD
// defaults.
type AgentSpec struct {
	DisplayName             string             `json:"displayName"`
	Description             string             `json:"description,omitempty"`
	SystemPrompt            string             `json:"systemPrompt,omitempty"`
	Framework               string             `json:"framework"`
	IdentityMode            string             `json:"identityMode"`
	Model                   ModelRef           `json:"model"`
	Environments            []AgentEnvironment `json:"environments,omitempty"`
	Connectivity            Connectivity       `json:"connectivity,omitzero"`
	Connectors              []Connector        `json:"connectors,omitempty"`
	MCPTools                []MCPTools         `json:"mcpTools,omitempty"`
	Scopes                  []string           `json:"scopes,omitempty"`
	Guardrails              *Guardrails        `json:"guardrails,omitempty"`
	Budget                  *AgentBudget       `json:"budget,omitempty"`
	MaxConcurrentRunsPerPod int32              `json:"maxConcurrentRunsPerPod,omitempty"`
}

// AgentEnvironment attaches an Environment by name.
type AgentEnvironment struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// Connectivity is the agent's egress posture. Hosts applies in whitelist mode.
type Connectivity struct {
	Mode  string   `json:"mode,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
}

// MCPTools lists the tools of one MCPServer the agent may call. Each expands
// to the scope mcp:<server>:<tool>:use.
type MCPTools struct {
	Server string   `json:"server"`
	Tools  []string `json:"tools"`
}

// Guardrails are loop-guard thresholds. Unset fields take the platform
// defaults.
type Guardrails struct {
	RepeatedCallWarn  *int32 `json:"repeatedCallWarn,omitempty"`
	RepeatedCallPause *int32 `json:"repeatedCallPause,omitempty"`
	WaitRearmWarn     *int32 `json:"waitRearmWarn,omitempty"`
	WaitRearmPause    *int32 `json:"waitRearmPause,omitempty"`
}

// AgentBudget is applied to the agent's grant.
type AgentBudget struct {
	LimitUSD       float64 `json:"limitUSD,omitempty"`
	ResetCadence   string  `json:"resetCadence,omitempty"`
	PerRunLimitUSD float64 `json:"perRunLimitUSD,omitempty"`
}

// AgentList is a list of Agent resources.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Agent `json:"items"`
}

// ─── TriggerSource ────────────────────────────────────────────────────────────

// TriggerSource is a webhook source for the tenant whose namespace holds it.
type TriggerSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   TriggerSourceSpec   `json:"spec"`
	Status TriggerSourceStatus `json:"status,omitzero"`
}

// TriggerSourceSpec mirrors the source create request. The signing secret
// comes from a Secret instead of the request body. Enabled defaults to true
// in the CRD.
type TriggerSourceSpec struct {
	Provider         string                `json:"provider"`
	Config           *runtime.RawExtension `json:"config,omitempty"`
	SigningSecretRef SecretKeyRef          `json:"signingSecretRef"`
	Enabled          bool                  `json:"enabled"`
}

// TriggerSourceStatus adds the ingest path to the common status.
type TriggerSourceStatus struct {
	Status      `json:",inline"`
	WebhookPath string `json:"webhookPath,omitempty"`
}

// TriggerSourceList is a list of TriggerSource resources.
type TriggerSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []TriggerSource `json:"items"`
}

// ─── Trigger ──────────────────────────────────────────────────────────────────

// Trigger starts runs of an Agent, or of a routed choice of Agents, for the
// tenant whose namespace holds it.
type Trigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   TriggerSpec `json:"spec"`
	Status Status      `json:"status,omitzero"`
}

// TriggerSpec mirrors the trigger create request. Source, Agent and the
// routing names are resource names in the same namespace. A nil admission
// field takes the platform default. Kind defaults to webhook and Enabled to
// true in the CRD.
type TriggerSpec struct {
	Kind              string              `json:"kind"`
	Source            string              `json:"source,omitempty"`
	EventType         string              `json:"eventType,omitempty"`
	Match             map[string][]string `json:"match,omitempty"`
	MatchContains     map[string][]string `json:"matchContains,omitempty"`
	SenderRestriction *SenderRestriction  `json:"senderRestriction,omitempty"`
	Coalesce          *Coalesce           `json:"coalesce,omitempty"`
	Agent             string              `json:"agent,omitempty"`
	Routing           *Routing            `json:"routing,omitempty"`
	Schedule          *Schedule           `json:"schedule,omitempty"`
	Manual            *Manual             `json:"manual,omitempty"`
	PromptTemplate    string              `json:"promptTemplate"`
	MaxParallel       *int32              `json:"maxParallel,omitempty"`
	Overflow          *string             `json:"overflow,omitempty"`
	MaxQueueDepth     *int32              `json:"maxQueueDepth,omitempty"`
	TimeoutSeconds    *int32              `json:"timeoutSeconds,omitempty"`
	DedupeEnabled     *bool               `json:"dedupeEnabled,omitempty"`
	AutopauseEnabled  *bool               `json:"autopauseEnabled,omitempty"`
	Enabled           bool                `json:"enabled"`
}

// SenderRestriction limits a webhook trigger to listed sender ids or roles.
type SenderRestriction struct {
	IDs   []string `json:"ids,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

// Coalesce folds deliveries with the same key inside the window into one.
type Coalesce struct {
	Key           string `json:"key"`
	WindowSeconds int32  `json:"windowSeconds"`
}

// Routing lets a decision model pick among Agents. Candidates and Fallback
// are Agent names. A nil JoinRuns means on.
type Routing struct {
	Candidates []string `json:"candidates"`
	Model      ModelRef `json:"model"`
	Confidence string   `json:"confidence,omitempty"`
	Fallback   string   `json:"fallback,omitempty"`
	JoinRuns   *bool    `json:"joinRuns,omitempty"`
	JoinKey    string   `json:"joinKey,omitempty"`
}

// Schedule is a scheduled trigger's cron configuration.
type Schedule struct {
	CronExpression string `json:"cronExpression"`
	Timezone       string `json:"timezone,omitempty"`
	CatchUp        bool   `json:"catchUp,omitempty"`
}

// Manual is a manual trigger's input form.
type Manual struct {
	Inputs []ManualInput `json:"inputs,omitempty"`
}

// ManualInput is one field a manual trigger accepts.
type ManualInput struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     string   `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

// TriggerList is a list of Trigger resources.
type TriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Trigger `json:"items"`
}

// ─── PolicyRole ───────────────────────────────────────────────────────────────

// PolicyRole maps an identity provider role claim to scopes for the tenant
// whose namespace holds it.
type PolicyRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PolicyRoleSpec `json:"spec"`
	Status Status         `json:"status,omitzero"`
}

// RoleName is the role claim the resource defines: spec.role, or
// metadata.name when spec.role is empty.
func (r *PolicyRole) RoleName() string {
	if r.Spec.Role != "" {
		return r.Spec.Role
	}
	return r.Name
}

// PolicyRoleSpec mirrors the role upsert request. Tenant is set only on a
// PolicyRole in the system namespace; the operator refuses it elsewhere.
type PolicyRoleSpec struct {
	Tenant string      `json:"tenant,omitempty"`
	Role   string      `json:"role,omitempty"`
	Scopes []string    `json:"scopes"`
	Budget *RoleBudget `json:"budget,omitempty"`
}

// RoleBudget is the budget every holder of the role gets, or shares when
// Pooled is set.
type RoleBudget struct {
	LimitUSD     float64 `json:"limitUSD"`
	ResetCadence string  `json:"resetCadence,omitempty"`
	Pooled       bool    `json:"pooled,omitempty"`
}

// PolicyRoleList is a list of PolicyRole resources.
type PolicyRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []PolicyRole `json:"items"`
}
