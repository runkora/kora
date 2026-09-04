// Package preflight verifies prerequisites for admitting tenant workloads.
package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config is the contract to verify against.
type Config struct {
	TrustDomain          string
	SystemNamespace      string
	IstioNamespace       string
	IstioRevision        string
	SPIREClassName       string
	SupportedIstioMinors []string
	SPIFFESocket         string
	WebhookNames         []string
}

// Check is one verified requirement.
type Check struct {
	Name    string
	OK      bool
	Message string
}

// Result is one complete preflight pass.
type Result struct {
	At     time.Time
	Checks []Check
}

// OK reports whether every check passed.
func (r Result) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return len(r.Checks) > 0
}

// Failures lists the failed checks as "name: message" lines.
func (r Result) Failures() string {
	var lines []string
	for _, c := range r.Checks {
		if !c.OK {
			lines = append(lines, c.Name+": "+c.Message)
		}
	}
	return strings.Join(lines, "; ")
}

// Runner executes the checks and remembers the latest result for the tenant
// reconciler to gate on.
type Runner struct {
	Client client.Reader
	Logger logging.Logger
	Config Config

	mu     sync.RWMutex
	latest *Result
}

// Latest returns the most recent result and whether one exists yet.
func (r *Runner) Latest() (Result, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.latest == nil {
		return Result{}, false
	}
	return *r.latest, true
}

// Run executes every check once and records the result.
func (r *Runner) Run(ctx context.Context) Result {
	res := Result{At: time.Now()}
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, Check{Name: name, OK: ok, Message: msg})
	}

	inj, err := r.injectorConfig(ctx)
	switch {
	case err != nil:
		add("istio-injector", false, err.Error())
		add("istio-version", false, "injector configuration not found")
		add("istio-cni", false, "injector configuration not found")
	default:
		add("istio-injector", true, fmt.Sprintf("revision %q, Istio %s", inj.revision, inj.version))
		ok, msg := r.versionSupported(inj.version)
		add("istio-version", ok, msg)
		if inj.cniEnabled {
			add("istio-cni", true, "pilot.cni.enabled is true")
		} else {
			add("istio-cni", false, "istiod is not configured with pilot.cni.enabled=true; the injected istio-init container needs NET_ADMIN, which the restricted pod security profile rejects")
		}
	}
	for _, check := range []func(context.Context) Check{
		r.cniDaemonSet, r.meshTrustDomain, r.systemNamespaceInjection, r.spireWorkloadAPI, r.clusterSPIFFEIDs, r.webhooks,
	} {
		res.Checks = append(res.Checks, check(ctx))
	}

	r.mu.Lock()
	r.latest = &res
	r.mu.Unlock()
	return res
}

type injector struct {
	revision   string
	version    string
	cniEnabled bool
}

// injectorConfig selects the injector for the configured Istio revision.
func (r *Runner) injectorConfig(ctx context.Context) (*injector, error) {
	rev := r.Config.IstioRevision
	if rev == "" {
		rev = "default"
	}
	list := &corev1.ConfigMapList{}
	if err := r.Client.List(ctx, list, client.InNamespace(r.Config.IstioNamespace), client.MatchingLabels{"istio.io/rev": rev}); err != nil {
		return nil, fmt.Errorf("list injector ConfigMaps in %q: %w", r.Config.IstioNamespace, err)
	}
	for i := range list.Items {
		cm := &list.Items[i]
		if !strings.HasPrefix(cm.Name, "istio-sidecar-injector") {
			continue
		}
		raw, ok := cm.Data["values"]
		if !ok {
			continue
		}
		var values struct {
			Global struct {
				Tag      any    `json:"tag"`
				Revision string `json:"revision"`
			} `json:"global"`
			Pilot struct {
				CNI struct {
					Enabled bool `json:"enabled"`
				} `json:"cni"`
			} `json:"pilot"`
		}
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("parse %s values: %w", cm.Name, err)
		}
		return &injector{
			revision:   firstNonEmpty(values.Global.Revision, rev),
			version:    fmt.Sprint(values.Global.Tag),
			cniEnabled: values.Pilot.CNI.Enabled,
		}, nil
	}
	return nil, fmt.Errorf("no istio-sidecar-injector ConfigMap with istio.io/rev=%s in %q; is Istio installed for that revision", rev, r.Config.IstioNamespace)
}

func (r *Runner) versionSupported(version string) (bool, string) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false, fmt.Sprintf("cannot parse Istio version %q", version)
	}
	minor := parts[0] + "." + parts[1]
	if slices.Contains(r.Config.SupportedIstioMinors, minor) {
		return true, fmt.Sprintf("Istio %s is supported", minor)
	}
	return false, fmt.Sprintf("Istio %s is not supported by this release (supported: %s)", minor, strings.Join(r.Config.SupportedIstioMinors, ", "))
}

func (r *Runner) cniDaemonSet(ctx context.Context) Check {
	const name = "istio-cni-daemonset"
	ds := &appsv1.DaemonSet{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: "istio-cni-node", Namespace: r.Config.IstioNamespace}, ds); err != nil {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("DaemonSet istio-cni-node not found in %q: %v", r.Config.IstioNamespace, err)}
	}
	if ds.Status.DesiredNumberScheduled == 0 || ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("istio-cni-node ready %d/%d", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)}
	}
	return Check{Name: name, OK: true, Message: "istio-cni-node is ready on every node"}
}

func (r *Runner) meshTrustDomain(ctx context.Context) Check {
	const name = "istio-trust-domain"
	cmName := "istio"
	if r.Config.IstioRevision != "" {
		cmName = "istio-" + r.Config.IstioRevision
	}
	cm := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: cmName, Namespace: r.Config.IstioNamespace}, cm); err != nil {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("ConfigMap %s not found in %q: %v", cmName, r.Config.IstioNamespace, err)}
	}
	var mesh struct {
		TrustDomain string `json:"trustDomain"`
	}
	if err := sigsyaml.Unmarshal([]byte(cm.Data["mesh"]), &mesh); err != nil {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("parse meshConfig: %v", err)}
	}
	if mesh.TrustDomain != r.Config.TrustDomain {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("meshConfig.trustDomain is %q, Orbit is configured for %q", mesh.TrustDomain, r.Config.TrustDomain)}
	}
	return Check{Name: name, OK: true, Message: "meshConfig.trustDomain matches"}
}

func (r *Runner) systemNamespaceInjection(ctx context.Context) Check {
	const name = "system-namespace-injection"
	ns := &corev1.Namespace{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: r.Config.SystemNamespace}, ns); err != nil {
		return Check{Name: name, OK: false, Message: err.Error()}
	}
	if ns.Labels["istio.io/dataplane-mode"] == "ambient" {
		return Check{Name: name, OK: false, Message: "system namespace is in ambient mode; Orbit requires sidecar injection"}
	}
	if r.Config.IstioRevision != "" {
		if ns.Labels["istio.io/rev"] != r.Config.IstioRevision {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("system namespace needs label istio.io/rev=%s", r.Config.IstioRevision)}
		}
		return Check{Name: name, OK: true, Message: "labeled for revision " + r.Config.IstioRevision}
	}
	if ns.Labels["istio-injection"] != "enabled" && ns.Labels["istio.io/rev"] == "" {
		return Check{Name: name, OK: false, Message: "system namespace needs label istio-injection=enabled"}
	}
	return Check{Name: name, OK: true, Message: "sidecar injection enabled"}
}

// spireWorkloadAPI verifies that this pod can fetch an SVID.
func (r *Runner) spireWorkloadAPI(ctx context.Context) Check {
	const name = "spire-workload-api"
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	svid, err := workloadapi.FetchX509SVID(fetchCtx, workloadapi.WithAddr(r.Config.SPIFFESocket))
	if err != nil {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("fetch SVID over %s: %v (is the SPIFFE CSI driver installed and is the operator registered)", r.Config.SPIFFESocket, err)}
	}
	if svid.ID.TrustDomain().Name() != r.Config.TrustDomain {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("SVID trust domain is %q, Orbit is configured for %q", svid.ID.TrustDomain().Name(), r.Config.TrustDomain)}
	}
	return Check{Name: name, OK: true, Message: "SVID issued in " + r.Config.TrustDomain}
}

var clusterSPIFFEIDGVK = schema.GroupVersionKind{Group: "spire.spiffe.io", Version: "v1alpha1", Kind: "ClusterSPIFFEIDList"}

// clusterSPIFFEIDs verifies Orbit registrations and rejects conflicting catch-all registrations.
func (r *Runner) clusterSPIFFEIDs(ctx context.Context) Check {
	const name = "spire-controller-manager"
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(clusterSPIFFEIDGVK)
	if err := r.Client.List(ctx, list); err != nil {
		if meta.IsNoMatchError(err) {
			return Check{Name: name, OK: false, Message: "ClusterSPIFFEID CRD is not served; install spire-controller-manager"}
		}
		return Check{Name: name, OK: false, Message: err.Error()}
	}
	orbitOwned := 0
	for _, item := range list.Items {
		spec, _ := item.Object["spec"].(map[string]any)
		className, _ := spec["className"].(string)
		if item.GetLabels()["app.kubernetes.io/part-of"] == "orbit" {
			if className != r.Config.SPIREClassName {
				return Check{Name: name, OK: false, Message: fmt.Sprintf("Orbit registration %s uses className %q but spire.className is %q", item.GetName(), className, r.Config.SPIREClassName)}
			}
			orbitOwned++
			continue
		}
		if claimsAllManagedPods(spec) {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("ClusterSPIFFEID %s selects every spiffe.io/spire-managed-identity pod without fallback; disable the SPIRE chart's default registration or set fallback: true", item.GetName())}
		}
	}
	if orbitOwned == 0 {
		return Check{Name: name, OK: false, Message: "no Orbit ClusterSPIFFEIDs found; is the orbit chart installed"}
	}
	return Check{Name: name, OK: true, Message: fmt.Sprintf("%d Orbit registrations for class %s", orbitOwned, r.Config.SPIREClassName)}
}

func claimsAllManagedPods(spec map[string]any) bool {
	if fallback, _ := spec["fallback"].(bool); fallback {
		return false
	}
	if _, hasNS := spec["namespaceSelector"]; hasNS {
		return false
	}
	ps, _ := spec["podSelector"].(map[string]any)
	ml, _ := ps["matchLabels"].(map[string]any)
	_, ok := ml["spiffe.io/spire-managed-identity"]
	return ok
}

func (r *Runner) webhooks(ctx context.Context) Check {
	const name = "orbit-webhooks"
	for _, name := range r.Config.WebhookNames {
		var bundle []byte
		var policy *admissionv1.FailurePolicyType
		mw := &admissionv1.MutatingWebhookConfiguration{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: name}, mw); err == nil && len(mw.Webhooks) > 0 {
			bundle, policy = mw.Webhooks[0].ClientConfig.CABundle, mw.Webhooks[0].FailurePolicy
		} else {
			vw := &admissionv1.ValidatingWebhookConfiguration{}
			if err := r.Client.Get(ctx, client.ObjectKey{Name: name}, vw); err != nil || len(vw.Webhooks) == 0 {
				return Check{Name: name, OK: false, Message: fmt.Sprintf("webhook configuration %s not found", name)}
			}
			bundle, policy = vw.Webhooks[0].ClientConfig.CABundle, vw.Webhooks[0].FailurePolicy
		}
		if len(bundle) == 0 {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("%s has no caBundle yet", name)}
		}
		if policy == nil || *policy != admissionv1.Fail {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("%s failurePolicy is not Fail yet", name)}
		}
	}
	return Check{Name: name, OK: true, Message: "both webhooks wired and fail closed"}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Loop runs the checks on an interval until ctx ends. Every replica may run it;
// only the tenant reconciler (leader) acts on the result.
type Loop struct {
	Runner   *Runner
	Interval time.Duration
}

// NeedLeaderElection reports false so a follower promoted to leader has a
// fresh result to gate on.
func (l *Loop) NeedLeaderElection() bool { return false }

func (l *Loop) Start(ctx context.Context) error {
	tick := time.NewTicker(l.Interval)
	defer tick.Stop()
	for {
		res := l.Runner.Run(ctx)
		if res.OK() {
			l.Runner.Logger.Info(ctx, "preflight passed")
		} else {
			l.Runner.Logger.Error(ctx, "preflight failed", logging.Field{Key: "failures", Value: res.Failures()})
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
