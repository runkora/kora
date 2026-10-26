package operator

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	dv1 "github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

// TriggerSourceSecretLabel marks the Secrets a TriggerSource may reference.
const TriggerSourceSecretLabel = "runkora.io/trigger-source"

// TriggerSourceSecretSelector selects the Secrets the operator caches: only
// those a TriggerSource may reference.
func TriggerSourceSecretSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{TriggerSourceSecretLabel: "true"})
}

// NewDeclaredReconcilers returns one reconciler per declarative kind. reader
// reads from the API server, bypassing the cache.
func NewDeclaredReconcilers(c client.Client, reader client.Reader, clients DeclaredClients, logger logging.Logger) []*DeclaredReconciler {
	kinds := []declaredKind{
		{
			kind:    declarative.KindVirtualModels,
			newObj:  func() client.Object { return &dv1.VirtualModel{} },
			newList: func() client.ObjectList { return &dv1.VirtualModelList{} },
			client:  clients.InferenceGateway,
			prepare: prepareVirtualModel,
		},
		{
			kind:    declarative.KindEnvironments,
			newObj:  func() client.Object { return &dv1.Environment{} },
			newList: func() client.ObjectList { return &dv1.EnvironmentList{} },
			client:  clients.Agents,
			prepare: prepareEnvironment,
		},
		{
			kind:    declarative.KindAgents,
			newObj:  func() client.Object { return &dv1.Agent{} },
			newList: func() client.ObjectList { return &dv1.AgentList{} },
			client:  clients.Agents,
			prepare: prepareAgent,
		},
		{
			kind:    declarative.KindTriggerSources,
			newObj:  func() client.Object { return &dv1.TriggerSource{} },
			newList: func() client.ObjectList { return &dv1.TriggerSourceList{} },
			client:  clients.Triggers,
			prepare: prepareTriggerSource,
		},
		{
			kind:    declarative.KindTriggers,
			newObj:  func() client.Object { return &dv1.Trigger{} },
			newList: func() client.ObjectList { return &dv1.TriggerList{} },
			client:  clients.Triggers,
			prepare: prepareTrigger,
		},
		{
			kind:    declarative.KindPolicyRoles,
			newObj:  func() client.Object { return &dv1.PolicyRole{} },
			newList: func() client.ObjectList { return &dv1.PolicyRoleList{} },
			client:  clients.PolicyService,
			prepare: preparePolicyRole,
		},
	}
	out := make([]*DeclaredReconciler, len(kinds))
	for i, k := range kinds {
		out[i] = &DeclaredReconciler{Client: c, Reader: reader, Logger: logger, kind: k}
	}
	// Triggers reconcile when a resource they reference changes, including its
	// status. A recreated Agent or TriggerSource has a new id the trigger must
	// follow. Agents wait for their references on the 30-second requeue.
	sources, triggers := out[3], out[4]
	// A rotated or deleted signing Secret reaches its TriggerSources at once.
	sources.kind.watches = []declaredWatch{{obj: &corev1.Secret{}, mapper: sources.enqueueSecretUsers}}
	triggers.kind.watches = []declaredWatch{
		{obj: &dv1.Agent{}, mapper: triggers.enqueueNamespace(&dv1.TriggerList{})},
		{obj: &dv1.TriggerSource{}, mapper: triggers.enqueueNamespace(&dv1.TriggerList{})},
		{obj: &dv1.VirtualModel{}, mapper: triggers.enqueueModelUsers},
	}
	return out
}

// declaredStatus returns the common status of a declarative resource.
func declaredStatus(obj client.Object) *dv1.Status {
	switch o := obj.(type) {
	case *dv1.VirtualModel:
		return &o.Status
	case *dv1.Environment:
		return &o.Status
	case *dv1.Agent:
		return &o.Status
	case *dv1.TriggerSource:
		return &o.Status.Status
	case *dv1.Trigger:
		return &o.Status
	case *dv1.PolicyRole:
		return &o.Status
	}
	panic(fmt.Sprintf("declaredStatus: unexpected %T", obj))
}

// refused reports a resource the operator or its service did not accept. An
// older refused resource does not hold a key against a newer one.
func refused(s *dv1.Status) bool {
	return meta.IsStatusConditionFalse(s.Conditions, condAccepted)
}

func isReady(s *dv1.Status) bool {
	return s.ID != "" && meta.IsStatusConditionTrue(s.Conditions, condReady)
}

func refusal(reason, message string) *metav1.Condition {
	c := condFalse(reason, message)
	return &c
}

// servedNamespaces maps each namespace held by an active tenant to that tenant.
func (r *DeclaredReconciler) servedNamespaces(ctx context.Context) (map[string]string, error) {
	list := &KoraTenantList{}
	if err := r.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list KoraTenants: %w", err)
	}
	out := map[string]string{}
	for _, t := range servedTenants(list.Items) {
		out[tenantNamespace(&t)] = t.Name
	}
	return out, nil
}

// tenantOrRefusal resolves the tenant of a tenant-namespace resource.
func (r *DeclaredReconciler) tenantOrRefusal(ctx context.Context, obj client.Object) (prepared, error) {
	served, err := r.servedNamespaces(ctx)
	if err != nil {
		return prepared{}, err
	}
	if id, ok := served[obj.GetNamespace()]; ok {
		return prepared{tenantID: id}, nil
	}
	return prepared{refusal: refusal(reasonUnknownTenant, fmt.Sprintf("Namespace %q belongs to no active tenant.", obj.GetNamespace()))}, nil
}

// exists reports whether the object is found. Other errors are returned.
func (r *DeclaredReconciler) exists(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
	err := r.Get(ctx, key, obj)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get %T %s: %w", obj, key, err)
	}
	return true, nil
}

// readyID returns the status.id of a Ready declarative resource, or "" when it
// is missing or not Ready.
func (r *DeclaredReconciler) readyID(ctx context.Context, key client.ObjectKey, obj client.Object) (string, error) {
	ok, err := r.exists(ctx, key, obj)
	if err != nil || !ok || !isReady(declaredStatus(obj)) {
		return "", err
	}
	return declaredStatus(obj).ID, nil
}

// readyModel reports whether a Ready VirtualModel declares family/name.
func (r *DeclaredReconciler) readyModel(ctx context.Context, ref dv1.ModelRef) (bool, error) {
	list := &dv1.VirtualModelList{}
	if err := r.List(ctx, list, client.InNamespace(systemNamespace)); err != nil {
		return false, fmt.Errorf("list VirtualModels: %w", err)
	}
	for i := range list.Items {
		vm := &list.Items[i]
		if vm.Spec.Family == ref.Family && vm.Spec.Name == ref.Name && isReady(&vm.Status) {
			return true, nil
		}
	}
	return false, nil
}

// refs collects resolved ids and the names of unresolved references.
type refs struct {
	ids     map[string]string
	missing []string
}

func (f *refs) miss(kind, name string) {
	f.missing = append(f.missing, fmt.Sprintf("%s %q", kind, name))
}

// declared resolves a reference to a Ready declarative resource in namespace
// and records its id under key.
func (f *refs) declared(ctx context.Context, r *DeclaredReconciler, kind, key, namespace, name string, obj client.Object) error {
	id, err := r.readyID(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
	if err != nil {
		return err
	}
	if id == "" {
		f.miss(kind, name)
		return nil
	}
	if f.ids == nil {
		f.ids = map[string]string{}
	}
	f.ids[key] = id
	return nil
}

// present resolves a reference that only has to exist, in any of namespaces.
func (f *refs) present(ctx context.Context, r *DeclaredReconciler, kind, name string, newObj func() client.Object, namespaces ...string) error {
	for _, ns := range namespaces {
		ok, err := r.exists(ctx, client.ObjectKey{Namespace: ns, Name: name}, newObj())
		if err != nil || ok {
			return err
		}
	}
	f.miss(kind, name)
	return nil
}

func (f *refs) model(ctx context.Context, r *DeclaredReconciler, ref dv1.ModelRef) error {
	ok, err := r.readyModel(ctx, ref)
	if err == nil && !ok {
		f.miss("VirtualModel", ref.Family+"/"+ref.Name)
	}
	return err
}

func (f *refs) connections(ctx context.Context, r *DeclaredReconciler, namespace string, connectors []dv1.Connector) error {
	for _, c := range connectors {
		if err := f.present(ctx, r, "Connection", c.Connection, func() client.Object { return &Connection{} }, namespace); err != nil {
			return err
		}
	}
	return nil
}

// ─── Per-kind checks ──────────────────────────────────────────────────────────

func prepareVirtualModel(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	vm := obj.(*dv1.VirtualModel)
	if vm.Namespace != systemNamespace {
		return prepared{refusal: refusal(reasonInvalid, fmt.Sprintf("A VirtualModel must be in the %s namespace.", systemNamespace))}, nil
	}
	list := &dv1.VirtualModelList{}
	if err := r.List(ctx, list, client.InNamespace(systemNamespace)); err != nil {
		return prepared{}, fmt.Errorf("list VirtualModels: %w", err)
	}
	for i := range list.Items {
		other := &list.Items[i]
		if other.UID != vm.UID && other.Spec.Family == vm.Spec.Family && other.Spec.Name == vm.Spec.Name && declaredBefore(other, vm) && !refused(&other.Status) {
			return prepared{refusal: refusal(reasonNameConflict,
				fmt.Sprintf("VirtualModel %q already declares %s/%s.", other.Name, vm.Spec.Family, vm.Spec.Name))}, nil
		}
	}
	return prepared{tenantID: declarative.PlatformTenant, spec: vm.Spec}, nil
}

func prepareEnvironment(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	env := obj.(*dv1.Environment)
	p, err := r.tenantOrRefusal(ctx, env)
	if err != nil || p.refusal != nil {
		return p, err
	}
	var f refs
	if env.Spec.SandboxImage != "" {
		if err := f.present(ctx, r, "SandboxImage", env.Spec.SandboxImage, func() client.Object { return &si.SandboxImage{} }, env.Namespace); err != nil {
			return p, err
		}
	}
	if err := f.connections(ctx, r, env.Namespace, env.Spec.Connectors); err != nil {
		return p, err
	}
	p.missing, p.spec = f.missing, env.Spec
	return p, nil
}

func prepareAgent(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	a := obj.(*dv1.Agent)
	p, err := r.tenantOrRefusal(ctx, a)
	if err != nil || p.refusal != nil {
		return p, err
	}
	var f refs
	for _, e := range a.Spec.Environments {
		if err := f.declared(ctx, r, "Environment", declarative.RefEnvironment(e.Name), a.Namespace, e.Name, &dv1.Environment{}); err != nil {
			return p, err
		}
	}
	if err := f.model(ctx, r, a.Spec.Model); err != nil {
		return p, err
	}
	if err := f.connections(ctx, r, a.Namespace, a.Spec.Connectors); err != nil {
		return p, err
	}
	for _, m := range a.Spec.MCPTools {
		// A global MCP server lives in the system namespace.
		if err := f.present(ctx, r, "MCPServer", m.Server, func() client.Object { return &MCPServer{} }, a.Namespace, systemNamespace); err != nil {
			return p, err
		}
	}
	p.missing, p.refs, p.spec = f.missing, f.ids, a.Spec
	return p, nil
}

func prepareTriggerSource(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	ts := obj.(*dv1.TriggerSource)
	p, err := r.tenantOrRefusal(ctx, ts)
	if err != nil || p.refusal != nil {
		return p, err
	}
	ref := ts.Spec.SigningSecretRef
	secret := &corev1.Secret{}
	ok, err := r.exists(ctx, client.ObjectKey{Namespace: ts.Namespace, Name: ref.Name}, secret)
	if err != nil {
		return p, err
	}
	// One message for every failure. A resource cannot probe which Secrets exist.
	if !ok || secret.Labels[TriggerSourceSecretLabel] != "true" || len(secret.Data[ref.Key]) == 0 {
		p.missing = []string{fmt.Sprintf("Secret %q with key %q and label %s=true", ref.Name, ref.Key, TriggerSourceSecretLabel)}
		return p, nil
	}
	p.secret, p.spec = secret.Data[ref.Key], ts.Spec
	return p, nil
}

func prepareTrigger(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	t := obj.(*dv1.Trigger)
	p, err := r.tenantOrRefusal(ctx, t)
	if err != nil || p.refusal != nil {
		return p, err
	}
	if p.refusal, err = r.delegatedBinding(ctx, t); err != nil || p.refusal != nil {
		return p, err
	}
	var f refs
	if t.Spec.Source != "" {
		if err := f.declared(ctx, r, "TriggerSource", declarative.RefSource(t.Spec.Source), t.Namespace, t.Spec.Source, &dv1.TriggerSource{}); err != nil {
			return p, err
		}
	}
	for _, name := range triggerAgents(t) {
		if err := f.declared(ctx, r, "Agent", declarative.RefAgent(name), t.Namespace, name, &dv1.Agent{}); err != nil {
			return p, err
		}
	}
	if rt := t.Spec.Routing; rt != nil {
		if err := f.model(ctx, r, rt.Model); err != nil {
			return p, err
		}
	}
	p.missing, p.refs, p.spec = f.missing, f.ids, t.Spec
	return p, nil
}

// delegatedBinding refuses a Trigger that names a delegated Agent. Only a
// person may start a delegated agent's runs. The operator refuses the binding
// before kora-triggers sees the trigger.
func (r *DeclaredReconciler) delegatedBinding(ctx context.Context, t *dv1.Trigger) (*metav1.Condition, error) {
	for _, name := range triggerAgents(t) {
		a := &dv1.Agent{}
		ok, err := r.exists(ctx, client.ObjectKey{Namespace: t.Namespace, Name: name}, a)
		if err != nil {
			return nil, err
		}
		if ok && a.Spec.IdentityMode == "delegated" {
			return refusal(reasonInvalid, fmt.Sprintf("Agent %q is delegated. Delegated agents cannot be bound to triggers.", name)), nil
		}
	}
	return nil, nil
}

// triggerAgents lists every Agent a Trigger names, once each.
func triggerAgents(t *dv1.Trigger) []string {
	var names []string
	if t.Spec.Agent != "" {
		names = append(names, t.Spec.Agent)
	}
	if rt := t.Spec.Routing; rt != nil {
		names = append(names, rt.Candidates...)
		if rt.Fallback != "" {
			names = append(names, rt.Fallback)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// preparePolicyRole applies the role rules: a tenant-namespace role belongs to
// that tenant and holds no platform scope; a system-namespace role names its
// tenant and may hold any scope.
func preparePolicyRole(ctx context.Context, r *DeclaredReconciler, obj client.Object) (prepared, error) {
	role := obj.(*dv1.PolicyRole)
	var p prepared
	if role.Namespace == systemNamespace {
		if role.Spec.Tenant == "" {
			return prepared{refusal: refusal(reasonInvalid, fmt.Sprintf("A PolicyRole in the %s namespace must set spec.tenant.", systemNamespace))}, nil
		}
		served, err := r.servedNamespaces(ctx)
		if err != nil {
			return p, err
		}
		if !slices.Contains(slices.Collect(maps.Values(served)), role.Spec.Tenant) {
			return prepared{refusal: refusal(reasonUnknownTenant, fmt.Sprintf("Tenant %q does not exist or is not active.", role.Spec.Tenant))}, nil
		}
		p.tenantID = role.Spec.Tenant
	} else {
		if role.Spec.Tenant != "" {
			return prepared{refusal: refusal(reasonInvalid, fmt.Sprintf("spec.tenant is allowed only in the %s namespace.", systemNamespace))}, nil
		}
		for _, s := range role.Spec.Scopes {
			if strings.HasPrefix(s, "platform:") {
				return prepared{refusal: refusal(reasonInvalid, fmt.Sprintf("Scope %q is a platform scope. Declare it in the %s namespace.", s, systemNamespace))}, nil
			}
		}
		var err error
		if p, err = r.tenantOrRefusal(ctx, role); err != nil || p.refusal != nil {
			return p, err
		}
	}
	if winner, err := r.olderRole(ctx, role, p.tenantID); err != nil || winner != "" {
		if winner != "" {
			p.refusal = refusal(reasonNameConflict, fmt.Sprintf("PolicyRole %s already declares role %q or the name %q for this tenant.", winner, role.RoleName(), role.Name))
		}
		return p, err
	}
	spec := role.Spec
	spec.Role = role.RoleName()
	p.spec = spec
	return p, nil
}

// olderRole returns the namespace/name of an older PolicyRole for the tenant
// with the same role or the same metadata.name, or "".
func (r *DeclaredReconciler) olderRole(ctx context.Context, role *dv1.PolicyRole, tenantID string) (string, error) {
	list := &dv1.PolicyRoleList{}
	if err := r.List(ctx, list); err != nil {
		return "", fmt.Errorf("list PolicyRoles: %w", err)
	}
	served, err := r.servedNamespaces(ctx)
	if err != nil {
		return "", err
	}
	for i := range list.Items {
		other := &list.Items[i]
		// The service keys a declared role by resource name as well as by role.
		if other.UID == role.UID || (other.RoleName() != role.RoleName() && other.Name != role.Name) || !declaredBefore(other, role) || refused(&other.Status) {
			continue
		}
		otherTenant := other.Spec.Tenant
		if other.Namespace != systemNamespace {
			otherTenant = served[other.Namespace]
		}
		if otherTenant == tenantID {
			return other.Namespace + "/" + other.Name, nil
		}
	}
	return "", nil
}

// ─── Watches ──────────────────────────────────────────────────────────────────

// enqueueSecretUsers maps a labeled Secret change to the TriggerSources in
// its namespace that reference it.
func (r *DeclaredReconciler) enqueueSecretUsers(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &dv1.TriggerSourceList{}
	if err := r.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
		r.Logger.Error(ctx, "list Secret users", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		if list.Items[i].Spec.SigningSecretRef.Name == obj.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return reqs
}

// enqueueNamespace maps a change to every resource of this kind in the same
// namespace.
func (r *DeclaredReconciler) enqueueNamespace(list client.ObjectList) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		l := list.DeepCopyObject().(client.ObjectList)
		if err := r.List(ctx, l, client.InNamespace(obj.GetNamespace())); err != nil {
			r.Logger.Error(ctx, "list declared dependents", logging.Field{Key: "error", Value: err.Error()})
			return nil
		}
		var reqs []reconcile.Request
		_ = meta.EachListItem(l, func(o runtime.Object) error {
			co := o.(client.Object)
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(co)})
			return nil
		})
		return reqs
	}
}

// enqueueModelUsers maps a VirtualModel change to the Triggers that route
// with its family and name.
func (r *DeclaredReconciler) enqueueModelUsers(ctx context.Context, obj client.Object) []reconcile.Request {
	vm, ok := obj.(*dv1.VirtualModel)
	if !ok {
		return nil
	}
	list := &dv1.TriggerList{}
	if err := r.List(ctx, list); err != nil {
		r.Logger.Error(ctx, "list VirtualModel users", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		if rt := list.Items[i].Spec.Routing; rt != nil && rt.Model.Family == vm.Spec.Family && rt.Model.Name == vm.Spec.Name {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return reqs
}
