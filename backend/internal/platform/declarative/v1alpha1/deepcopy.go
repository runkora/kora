package v1alpha1

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Hand-written deep copies, as for the other runkora.io kinds. Every slice,
// map and pointer is copied so a cached object is never shared.

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneMatch(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = slices.Clone(v)
	}
	return out
}

func cloneConnectors(in []Connector) []Connector {
	if in == nil {
		return nil
	}
	out := make([]Connector, len(in))
	for i, c := range in {
		out[i] = Connector{Connection: c.Connection, Actions: slices.Clone(c.Actions), Resources: slices.Clone(c.Resources)}
	}
	return out
}

// copyItems deep-copies list items.
func copyItems[T any, P interface {
	*T
	DeepCopyInto(*T)
}](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	for i := range in {
		P(&in[i]).DeepCopyInto(&out[i])
	}
	return out
}

func copyListMeta(m metav1.ListMeta) metav1.ListMeta { return *m.DeepCopy() }

func copyConditions(in []metav1.Condition) []metav1.Condition {
	if in == nil {
		return nil
	}
	out := make([]metav1.Condition, len(in))
	for i := range in {
		in[i].DeepCopyInto(&out[i])
	}
	return out
}

// DeepCopyInto copies the status.
func (in *Status) DeepCopyInto(out *Status) {
	*out = *in
	out.Conditions = copyConditions(in.Conditions)
}

// ─── VirtualModel ─────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *VirtualModel) DeepCopyInto(out *VirtualModel) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.Routes = slices.Clone(in.Spec.Routes)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (in *VirtualModel) DeepCopy() *VirtualModel {
	if in == nil {
		return nil
	}
	out := new(VirtualModel)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *VirtualModel) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *VirtualModelList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &VirtualModelList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}

// ─── Environment ──────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *Environment) DeepCopyInto(out *Environment) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.EgressHosts = slices.Clone(in.Spec.EgressHosts)
	out.Spec.Connectors = cloneConnectors(in.Spec.Connectors)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (in *Environment) DeepCopy() *Environment {
	if in == nil {
		return nil
	}
	out := new(Environment)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *Environment) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *EnvironmentList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &EnvironmentList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}

// ─── Agent ────────────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *Agent) DeepCopyInto(out *Agent) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies the spec.
func (in *AgentSpec) DeepCopyInto(out *AgentSpec) {
	*out = *in
	out.Environments = slices.Clone(in.Environments)
	out.Connectivity.Hosts = slices.Clone(in.Connectivity.Hosts)
	out.Connectors = cloneConnectors(in.Connectors)
	if in.MCPTools != nil {
		out.MCPTools = make([]MCPTools, len(in.MCPTools))
		for i, m := range in.MCPTools {
			out.MCPTools[i] = MCPTools{Server: m.Server, Tools: slices.Clone(m.Tools)}
		}
	}
	out.Scopes = slices.Clone(in.Scopes)
	if in.Guardrails != nil {
		out.Guardrails = &Guardrails{
			RepeatedCallWarn:  clonePtr(in.Guardrails.RepeatedCallWarn),
			RepeatedCallPause: clonePtr(in.Guardrails.RepeatedCallPause),
			WaitRearmWarn:     clonePtr(in.Guardrails.WaitRearmWarn),
			WaitRearmPause:    clonePtr(in.Guardrails.WaitRearmPause),
		}
	}
	out.Budget = clonePtr(in.Budget)
}

// DeepCopy returns a deep copy.
func (in *Agent) DeepCopy() *Agent {
	if in == nil {
		return nil
	}
	out := new(Agent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *Agent) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *AgentList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &AgentList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}

// ─── TriggerSource ────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *TriggerSource) DeepCopyInto(out *TriggerSource) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if in.Spec.Config != nil {
		out.Spec.Config = in.Spec.Config.DeepCopy()
	}
	out.Status.Conditions = copyConditions(in.Status.Conditions)
}

// DeepCopy returns a deep copy.
func (in *TriggerSource) DeepCopy() *TriggerSource {
	if in == nil {
		return nil
	}
	out := new(TriggerSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *TriggerSource) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *TriggerSourceList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &TriggerSourceList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}

// ─── Trigger ──────────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *Trigger) DeepCopyInto(out *Trigger) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies the spec.
func (in *TriggerSpec) DeepCopyInto(out *TriggerSpec) {
	*out = *in
	out.Match = cloneMatch(in.Match)
	out.MatchContains = cloneMatch(in.MatchContains)
	if in.SenderRestriction != nil {
		out.SenderRestriction = &SenderRestriction{IDs: slices.Clone(in.SenderRestriction.IDs), Roles: slices.Clone(in.SenderRestriction.Roles)}
	}
	out.Coalesce = clonePtr(in.Coalesce)
	if in.Routing != nil {
		r := *in.Routing
		r.Candidates = slices.Clone(in.Routing.Candidates)
		r.JoinRuns = clonePtr(in.Routing.JoinRuns)
		out.Routing = &r
	}
	out.Schedule = clonePtr(in.Schedule)
	if in.Manual != nil {
		out.Manual = &Manual{}
		if in.Manual.Inputs != nil {
			out.Manual.Inputs = make([]ManualInput, len(in.Manual.Inputs))
			for i, f := range in.Manual.Inputs {
				f.Choices = slices.Clone(f.Choices)
				out.Manual.Inputs[i] = f
			}
		}
	}
	out.MaxParallel = clonePtr(in.MaxParallel)
	out.Overflow = clonePtr(in.Overflow)
	out.MaxQueueDepth = clonePtr(in.MaxQueueDepth)
	out.TimeoutSeconds = clonePtr(in.TimeoutSeconds)
	out.DedupeEnabled = clonePtr(in.DedupeEnabled)
	out.AutopauseEnabled = clonePtr(in.AutopauseEnabled)
}

// DeepCopy returns a deep copy.
func (in *Trigger) DeepCopy() *Trigger {
	if in == nil {
		return nil
	}
	out := new(Trigger)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *Trigger) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *TriggerList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &TriggerList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}

// ─── PolicyRole ───────────────────────────────────────────────────────────────

// DeepCopyInto copies all fields into out.
func (in *PolicyRole) DeepCopyInto(out *PolicyRole) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.Scopes = slices.Clone(in.Spec.Scopes)
	out.Spec.Budget = clonePtr(in.Spec.Budget)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (in *PolicyRole) DeepCopy() *PolicyRole {
	if in == nil {
		return nil
	}
	out := new(PolicyRole)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *PolicyRole) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (in *PolicyRoleList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return &PolicyRoleList{TypeMeta: in.TypeMeta, ListMeta: copyListMeta(in.ListMeta), Items: copyItems(in.Items)}
}
