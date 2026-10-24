// +kubebuilder:object:generate=true
// +groupName=runkora.io

// Package v1alpha1 holds the declarative resource kinds: VirtualModel,
// Environment, Agent, TriggerSource, Trigger and PolicyRole. Each owning
// service projects its kinds into its own tables; the kora-crds chart ships
// their CRDs, which controller-gen generates from the markers in this file
// (make generate).
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
	ID                 string `json:"id,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ModelRef names a VirtualModel by its family and name.
type ModelRef struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9_-]{1,255}$`
	Family string `json:"family"`
	// +kubebuilder:validation:Pattern=`^[a-z0-9_-]{1,255}$`
	Name string `json:"name"`
}

// SecretKeyRef names one key of a Secret in the resource's namespace.
type SecretKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// Connector attaches a Connection and the actions the agent may invoke
// through it. Resources narrows it to upstream resources; empty means all.
type Connector struct {
	// Connection is a Connection name in this namespace.
	// +kubebuilder:validation:MinLength=1
	Connection string `json:"connection"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Actions []string `json:"actions"`
	// +kubebuilder:validation:items:MinLength=1
	Resources []string `json:"resources,omitempty"`
}

// ─── VirtualModel ─────────────────────────────────────────────────────────────

// VirtualModel is a platform-wide catalog model. It lives in the system
// namespace and inference-gateway owns it.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kvm
// +kubebuilder:printcolumn:name=Family,type=string,JSONPath=`.spec.family`
// +kubebuilder:printcolumn:name=Model,type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label."
type VirtualModel struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec VirtualModelSpec `json:"spec"`
	// +optional
	Status Status `json:"status,omitzero"`
}

// VirtualModelSpec mirrors the virtual model create request. Enabled
// defaults to true in the CRD.
type VirtualModelSpec struct {
	// Family is the model family. It is the first segment of the model's scope.
	// +kubebuilder:validation:Pattern=`^[a-z0-9_-]{1,255}$`
	Family string `json:"family"`
	// Name is the model name within its family.
	// +kubebuilder:validation:Pattern=`^[a-z0-9_-]{1,255}$`
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// Strategy is how a request picks among enabled routes. Weighted routes
	// must sum to 100.
	// +optional
	// +kubebuilder:validation:Enum=priority;weighted;lowest_cost
	// +kubebuilder:default=priority
	Strategy string `json:"strategy"`
	// +optional
	// +kubebuilder:default=true
	Enabled bool                `json:"enabled"`
	Routes  []VirtualModelRoute `json:"routes,omitempty"`
}

// VirtualModelRoute sends the model to one provider model. Enabled defaults
// to true in the CRD.
type VirtualModelRoute struct {
	// Provider is a provider name, <type>:<InferenceProvider name>.
	// +kubebuilder:validation:Pattern=`^[a-z]+:[a-z0-9][-a-z0-9_]*$`
	Provider string `json:"provider"`
	// ModelID is the provider's model id.
	// +kubebuilder:validation:MinLength=1
	ModelID string `json:"modelID"`
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Priority int32 `json:"priority"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight,omitempty"`
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// VirtualModelList is a list of VirtualModel resources.
// +kubebuilder:object:root=true
type VirtualModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []VirtualModel `json:"items"`
}

// ─── Environment ──────────────────────────────────────────────────────────────

// Environment is a sandbox environment for the tenant whose namespace holds
// it. Its metadata.name is the environment slug.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kenv
// +kubebuilder:printcolumn:name=Image,type=string,JSONPath=`.spec.sandboxImage`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label."
type Environment struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec EnvironmentSpec `json:"spec"`
	// +optional
	Status Status `json:"status,omitzero"`
}

// EnvironmentSpec mirrors the environment create request.
type EnvironmentSpec struct {
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// SandboxImage is a SandboxImage name in this namespace. Empty runs kora-base.
	SandboxImage string `json:"sandboxImage,omitempty"`
	// +optional
	Size EnvironmentSize `json:"size,omitzero"`
	// EgressHosts are the hosts a sandbox may reach, exact or "*.suffix".
	// +kubebuilder:validation:items:MinLength=1
	EgressHosts []string `json:"egressHosts,omitempty"`
	// +listType=map
	// +listMapKey=connection
	Connectors []Connector `json:"connectors,omitempty"`
}

// EnvironmentSize holds Kubernetes quantities. An empty one takes the install
// default.
type EnvironmentSize struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Disk   string `json:"disk,omitempty"`
}

// EnvironmentList is a list of Environment resources.
// +kubebuilder:object:root=true
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Environment `json:"items"`
}

// ─── Agent ────────────────────────────────────────────────────────────────────

// Agent is an agent type for the tenant whose namespace holds it. Its
// metadata.name is the agent slug.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kag
// +kubebuilder:printcolumn:name=Mode,type=string,JSONPath=`.spec.identityMode`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 52",message="metadata.name must be a DNS-1035 label of at most 52 characters."
// +kubebuilder:validation:XValidation:rule="!(self.metadata.name in ['delegated', 'autonomous'])",message="metadata.name must not be delegated or autonomous. They are identity mode names."
type Agent struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec AgentSpec `json:"spec"`
	// +optional
	Status Status `json:"status,omitzero"`
}

// AgentSpec mirrors the agent create request. Scopes is the API's
// grant_scopes. Framework, IdentityMode and Connectivity.Mode have CRD
// defaults.
type AgentSpec struct {
	// +kubebuilder:validation:MinLength=1
	DisplayName  string `json:"displayName"`
	Description  string `json:"description,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// +optional
	// +kubebuilder:validation:Enum=claude-code;codex;opencode
	// +kubebuilder:default=claude-code
	Framework string `json:"framework"`
	// IdentityMode is who the agent acts as. Recreate the Agent to change it.
	// +optional
	// +kubebuilder:validation:Enum=autonomous;delegated
	// +kubebuilder:default=autonomous
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="identityMode is immutable."
	IdentityMode string `json:"identityMode"`
	// Model is the VirtualModel the agent runs on.
	Model ModelRef `json:"model"`
	// Environments the agent may run in, by name. One is the default. Empty
	// runs kora-base.
	// +listType=map
	// +listMapKey=name
	Environments []AgentEnvironment `json:"environments,omitempty"`
	// +optional
	Connectivity Connectivity `json:"connectivity,omitzero"`
	// +listType=map
	// +listMapKey=connection
	Connectors []Connector `json:"connectors,omitempty"`
	// MCPTools are the tools the agent may call, per MCPServer.
	// +listType=map
	// +listMapKey=server
	MCPTools []MCPTools `json:"mcpTools,omitempty"`
	// Scopes are further scopes on the agent's grant.
	// +kubebuilder:validation:items:MinLength=1
	Scopes []string `json:"scopes,omitempty"`
	// Guardrails are loop-guard thresholds. Each pause must be above its warn.
	// Unset fields take the platform defaults.
	Guardrails *Guardrails  `json:"guardrails,omitempty"`
	Budget     *AgentBudget `json:"budget,omitempty"`
	// MaxConcurrentRunsPerPod is the number of runs one agent pod holds at
	// once. Unset takes the install default.
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentRunsPerPod int32 `json:"maxConcurrentRunsPerPod,omitempty"`
}

// AgentEnvironment attaches an Environment by name.
type AgentEnvironment struct {
	// +kubebuilder:validation:MinLength=1
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// Connectivity is the agent's egress posture. Hosts applies in whitelist mode.
type Connectivity struct {
	// +kubebuilder:validation:Enum=none;whitelist;open
	// +kubebuilder:default=none
	Mode string `json:"mode,omitempty"`
	// Hosts the agent may reach in whitelist mode, exact or "*.suffix".
	// +kubebuilder:validation:items:MinLength=1
	Hosts []string `json:"hosts,omitempty"`
}

// MCPTools lists the tools of one MCPServer the agent may call. Each expands
// to the scope mcp:<server>:<tool>:use.
type MCPTools struct {
	// Server is an MCPServer name in this namespace.
	// +kubebuilder:validation:MinLength=1
	Server string `json:"server"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Tools []string `json:"tools"`
}

// Guardrails are loop-guard thresholds. Unset fields take the platform
// defaults.
type Guardrails struct {
	// +kubebuilder:validation:Minimum=1
	RepeatedCallWarn *int32 `json:"repeatedCallWarn,omitempty"`
	// +kubebuilder:validation:Minimum=1
	RepeatedCallPause *int32 `json:"repeatedCallPause,omitempty"`
	// +kubebuilder:validation:Minimum=1
	WaitRearmWarn *int32 `json:"waitRearmWarn,omitempty"`
	// +kubebuilder:validation:Minimum=1
	WaitRearmPause *int32 `json:"waitRearmPause,omitempty"`
}

// AgentBudget is applied to the agent's grant.
type AgentBudget struct {
	// +kubebuilder:validation:Minimum=0
	LimitUSD float64 `json:"limitUSD,omitempty"`
	// +kubebuilder:validation:Enum=weekly;monthly
	// +kubebuilder:default=monthly
	ResetCadence string `json:"resetCadence,omitempty"`
	// +kubebuilder:validation:Minimum=0
	PerRunLimitUSD float64 `json:"perRunLimitUSD,omitempty"`
}

// AgentList is a list of Agent resources.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Agent `json:"items"`
}

// ─── TriggerSource ────────────────────────────────────────────────────────────

// TriggerSource is a webhook source for the tenant whose namespace holds it.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ktsrc
// +kubebuilder:printcolumn:name=Provider,type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Path,type=string,priority=1,JSONPath=`.status.webhookPath`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label."
type TriggerSource struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec TriggerSourceSpec `json:"spec"`
	// +optional
	Status TriggerSourceStatus `json:"status,omitzero"`
}

// TriggerSourceSpec mirrors the source create request. The signing secret
// comes from a Secret instead of the request body. The Secret must carry the
// label runkora.io/trigger-source=true. Enabled defaults to true in the CRD.
type TriggerSourceSpec struct {
	// Provider is the webhook provider, such as github or slack.
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`
	// Config is the provider-specific configuration, as the API accepts it.
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *runtime.RawExtension `json:"config,omitempty"`
	// SigningSecretRef is the Secret key that holds the signing secret. The
	// Secret must carry the label runkora.io/trigger-source=true.
	SigningSecretRef SecretKeyRef `json:"signingSecretRef"`
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// TriggerSourceStatus adds the ingest path to the common status.
type TriggerSourceStatus struct {
	Status `json:",inline"`
	// WebhookPath is the ingest path to configure in the provider. It
	// includes the source id.
	WebhookPath string `json:"webhookPath,omitempty"`
}

// TriggerSourceList is a list of TriggerSource resources.
// +kubebuilder:object:root=true
type TriggerSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []TriggerSource `json:"items"`
}

// ─── Trigger ──────────────────────────────────────────────────────────────────

// Trigger starts runs of an Agent, or of a routed choice of Agents, for the
// tenant whose namespace holds it.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ktrig
// +kubebuilder:printcolumn:name=Kind,type=string,JSONPath=`.spec.kind`
// +kubebuilder:printcolumn:name=Agent,type=string,JSONPath=`.spec.agent`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label."
type Trigger struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec TriggerSpec `json:"spec"`
	// +optional
	Status Status `json:"status,omitzero"`
}

// TriggerSpec mirrors the trigger create request. Source, Agent and the
// routing names are resource names in the same namespace. A nil admission
// field takes the platform default. Kind defaults to webhook and Enabled to
// true in the CRD.
// +kubebuilder:validation:XValidation:rule="has(self.agent) != has(self.routing)",message="set exactly one of agent and routing."
// +kubebuilder:validation:XValidation:rule="self.kind != 'webhook' || (has(self.source) && has(self.eventType))",message="a webhook trigger needs source and eventType."
// +kubebuilder:validation:XValidation:rule="self.kind != 'scheduled' || has(self.schedule)",message="a scheduled trigger needs schedule."
// +kubebuilder:validation:XValidation:rule="self.kind == 'webhook' || !(has(self.source) || has(self.eventType) || has(self.match) || has(self.matchContains) || has(self.senderRestriction) || has(self.coalesce) || has(self.routing))",message="source, eventType, match, matchContains, senderRestriction, coalesce and routing apply to webhook triggers only."
// +kubebuilder:validation:XValidation:rule="self.kind == 'scheduled' || !has(self.schedule)",message="schedule applies to scheduled triggers only."
// +kubebuilder:validation:XValidation:rule="self.kind == 'manual' || !has(self.manual)",message="manual applies to manual triggers only."
type TriggerSpec struct {
	// +optional
	// +kubebuilder:validation:Enum=webhook;scheduled;manual
	// +kubebuilder:default=webhook
	Kind string `json:"kind"`
	// Source is a TriggerSource name in this namespace.
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source,omitempty"`
	// +kubebuilder:validation:MinLength=1
	EventType string `json:"eventType,omitempty"`
	// Match maps payload paths to the values each must equal, one of.
	Match map[string][]string `json:"match,omitempty"`
	// MatchContains maps payload paths to the values each must contain, one of.
	MatchContains     map[string][]string `json:"matchContains,omitempty"`
	SenderRestriction *SenderRestriction  `json:"senderRestriction,omitempty"`
	Coalesce          *Coalesce           `json:"coalesce,omitempty"`
	// Agent is the Agent name in this namespace whose runs the trigger starts.
	// +kubebuilder:validation:MinLength=1
	Agent    string    `json:"agent,omitempty"`
	Routing  *Routing  `json:"routing,omitempty"`
	Schedule *Schedule `json:"schedule,omitempty"`
	Manual   *Manual   `json:"manual,omitempty"`
	// +kubebuilder:validation:MinLength=1
	PromptTemplate string `json:"promptTemplate"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=16
	MaxParallel *int16 `json:"maxParallel,omitempty"`
	// +kubebuilder:validation:Enum=wait;replace;refuse
	Overflow *string `json:"overflow,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=500
	MaxQueueDepth *int16 `json:"maxQueueDepth,omitempty"`
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:validation:Maximum=600
	TimeoutSeconds   *int16 `json:"timeoutSeconds,omitempty"`
	DedupeEnabled    *bool  `json:"dedupeEnabled,omitempty"`
	AutopauseEnabled *bool  `json:"autopauseEnabled,omitempty"`
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// SenderRestriction limits a webhook trigger to listed sender ids or roles.
type SenderRestriction struct {
	IDs   []string `json:"ids,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

// Coalesce folds deliveries with the same key inside the window into one.
type Coalesce struct {
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=300
	WindowSeconds int32 `json:"windowSeconds"`
}

// Routing lets a decision model pick among Agents. Candidates and Fallback
// are Agent names. A nil JoinRuns means on.
type Routing struct {
	// Candidates are Agent names in this namespace.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:items:MinLength=1
	Candidates []string `json:"candidates"`
	// Model is the VirtualModel that decides.
	Model ModelRef `json:"model"`
	// Confidence is the probability the chosen Agent must reach. Unset means
	// balanced.
	// +kubebuilder:validation:Enum=strict;balanced;loose
	Confidence string `json:"confidence,omitempty"`
	// Fallback is the candidate Agent that takes an event the model is unsure
	// about. Unset starts no run.
	// +kubebuilder:validation:MinLength=1
	Fallback string `json:"fallback,omitempty"`
	// JoinRuns lets an event join an open run. Unset means true.
	JoinRuns *bool `json:"joinRuns,omitempty"`
	// +kubebuilder:validation:MaxLength=256
	JoinKey string `json:"joinKey,omitempty"`
}

// Schedule is a scheduled trigger's cron configuration.
type Schedule struct {
	// CronExpression is a standard five-field cron expression.
	// +kubebuilder:validation:MinLength=1
	CronExpression string `json:"cronExpression"`
	// Timezone is an IANA timezone name.
	// +kubebuilder:default=UTC
	Timezone string `json:"timezone,omitempty"`
	// CatchUp fires once for schedules missed while the platform was down.
	CatchUp bool `json:"catchUp,omitempty"`
}

// Manual is a manual trigger's input form.
type Manual struct {
	// +listType=map
	// +listMapKey=name
	Inputs []ManualInput `json:"inputs,omitempty"`
}

// ManualInput is one field a manual trigger accepts.
type ManualInput struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=text;number;boolean;choice
	Type        string   `json:"type"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     string   `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

// TriggerList is a list of Trigger resources.
// +kubebuilder:object:root=true
type TriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Trigger `json:"items"`
}

// ─── PolicyRole ───────────────────────────────────────────────────────────────

// PolicyRole maps an identity provider role claim to scopes for the tenant
// whose namespace holds it.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kpr
// +kubebuilder:printcolumn:name=Role,type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name=Accepted,type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label."
type PolicyRole struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec PolicyRoleSpec `json:"spec"`
	// +optional
	Status Status `json:"status,omitzero"`
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
	// Tenant is the tenant the role belongs to. Set it only on a PolicyRole in
	// the system namespace. Such a role may hold platform scopes.
	// +kubebuilder:validation:MinLength=1
	Tenant string `json:"tenant,omitempty"`
	// Role is the identity provider's role claim. Unset means metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Role string `json:"role,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Scopes []string    `json:"scopes"`
	Budget *RoleBudget `json:"budget,omitempty"`
}

// RoleBudget is the budget every holder of the role gets, or shares when
// Pooled is set.
type RoleBudget struct {
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:ExclusiveMinimum=true
	LimitUSD float64 `json:"limitUSD"`
	// +kubebuilder:validation:Enum=weekly;monthly
	// +kubebuilder:default=monthly
	ResetCadence string `json:"resetCadence,omitempty"`
	Pooled       bool   `json:"pooled,omitempty"`
}

// PolicyRoleList is a list of PolicyRole resources.
// +kubebuilder:object:root=true
type PolicyRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []PolicyRole `json:"items"`
}
