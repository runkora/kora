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

	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/logging"
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
		if apierrors.IsNotFound(err) {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("DaemonSet istio-cni-node not found in %q: is Istio CNI installed?", r.Config.IstioNamespace)}
		}
		return Check{Name: name, OK: false, Message: fmt.Sprintf("get DaemonSet istio-cni-node in %q: %v", r.Config.IstioNamespace, err)}
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
		if apierrors.IsNotFound(err) {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("ConfigMap %s not found in %q: is istiod installed at this revision?", cmName, r.Config.IstioNamespace)}
		}
		return Check{Name: name, OK: false, Message: fmt.Sprintf("get ConfigMap %s in %q: %v", cmName, r.Config.IstioNamespace, err)}
	}
	var mesh struct {
		TrustDomain string `json:"trustDomain"`
	}
	if err := sigsyaml.Unmarshal([]byte(cm.Data["mesh"]), &mesh); err != nil {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("parse meshConfig: %v", err)}
	}
	if mesh.TrustDomain != r.Config.TrustDomain {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("meshConfig.trustDomain is %q, kora is configured for %q", mesh.TrustDomain, r.Config.TrustDomain)}
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
		return Check{Name: name, OK: false, Message: "system namespace is in ambient mode; kora requires sidecar injection"}
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

// svidRegistrationGrace bounds how long a fresh pod waits for its SPIRE
// entry. The controller manager registers a pod a few seconds after it
// exists; until then the Workload API answers PermissionDenied.
const svidRegistrationGrace = 45 * time.Second

// spireWorkloadAPI verifies that this pod can fetch an SVID.
func (r *Runner) spireWorkloadAPI(ctx context.Context) Check {
	const name = "spire-workload-api"
	deadline := time.Now().Add(svidRegistrationGrace)
	var svid *x509svid.SVID
	for {
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var err error
		svid, err = workloadapi.FetchX509SVID(fetchCtx, workloadapi.WithAddr(r.Config.SPIFFESocket))
		cancel()
		if err == nil {
			break
		}
		if status.Code(err) != codes.PermissionDenied || time.Now().After(deadline) {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("fetch SVID over %s: %v (is the SPIFFE CSI driver installed and is the operator registered)", r.Config.SPIFFESocket, err)}
		}
		select {
		case <-ctx.Done():
			return Check{Name: name, OK: false, Message: "fetch SVID: " + ctx.Err().Error()}
		case <-time.After(3 * time.Second):
		}
	}
	if svid.ID.TrustDomain().Name() != r.Config.TrustDomain {
		return Check{Name: name, OK: false, Message: fmt.Sprintf("SVID trust domain is %q, kora is configured for %q", svid.ID.TrustDomain().Name(), r.Config.TrustDomain)}
	}
	return Check{Name: name, OK: true, Message: "SVID issued in " + r.Config.TrustDomain}
}

var clusterSPIFFEIDGVK = schema.GroupVersionKind{Group: "spire.spiffe.io", Version: "v1alpha1", Kind: "ClusterSPIFFEIDList"}

// clusterSPIFFEIDs verifies kora registrations and rejects conflicting catch-all registrations.
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
	koraOwned := 0
	for _, item := range list.Items {
		spec, _ := item.Object["spec"].(map[string]any)
		className, _ := spec["className"].(string)
		if item.GetLabels()["app.kubernetes.io/part-of"] == "kora" {
			if className != r.Config.SPIREClassName {
				return Check{Name: name, OK: false, Message: fmt.Sprintf("kora registration %s uses className %q but spire.className is %q", item.GetName(), className, r.Config.SPIREClassName)}
			}
			if failures := statFailures(item.Object); failures != "" {
				return Check{Name: name, OK: false, Message: fmt.Sprintf("kora registration %s: %s; spire-controller-manager is not reconciling it", item.GetName(), failures)}
			}
			koraOwned++
			continue
		}
		if claimsAllManagedPods(spec) {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("ClusterSPIFFEID %s selects every spiffe.io/spire-managed-identity pod without fallback; disable the SPIRE chart's default registration or set fallback: true", item.GetName())}
		}
	}
	if koraOwned == 0 {
		return Check{Name: name, OK: false, Message: "no kora ClusterSPIFFEIDs found; is the kora chart installed"}
	}
	return Check{Name: name, OK: true, Message: fmt.Sprintf("%d kora registrations for class %s", koraOwned, r.Config.SPIREClassName)}
}

// statFailures reads status.stats.entryFailures and podEntryRenderFailures
// off a ClusterSPIFFEID; a nonzero count means the controller manager has
// the object but isn't successfully turning it into SPIRE registration
// entries. Missing status.stats fails too: the controller manager has not
// reconciled the object, and the check runs again on the next interval.
func statFailures(obj map[string]any) string {
	status, _ := obj["status"].(map[string]any)
	stats, _ := status["stats"].(map[string]any)
	if stats == nil {
		return "no status.stats yet"
	}
	entryFailures := statInt(stats["entryFailures"])
	podFailures := statInt(stats["podEntryRenderFailures"])
	if entryFailures == 0 && podFailures == 0 {
		return ""
	}
	return fmt.Sprintf("entryFailures=%d podEntryRenderFailures=%d", entryFailures, podFailures)
}

// statInt reads a numeric status field from unstructured JSON, which decodes
// whole numbers as either int64 or float64 depending on the client path.
func statInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
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
	if _, ok := ml["spiffe.io/spire-managed-identity"]; ok {
		return true
	}
	exprs, _ := ps["matchExpressions"].([]any)
	for _, e := range exprs {
		expr, _ := e.(map[string]any)
		if key, _ := expr["key"].(string); key == "spiffe.io/spire-managed-identity" {
			return true
		}
	}
	return false
}

func (r *Runner) webhooks(ctx context.Context) Check {
	const name = "kora-webhooks"
	for _, hook := range r.Config.WebhookNames {
		var bundle []byte
		var policy *admissionv1.FailurePolicyType
		mw := &admissionv1.MutatingWebhookConfiguration{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: hook}, mw); err == nil && len(mw.Webhooks) > 0 {
			bundle, policy = mw.Webhooks[0].ClientConfig.CABundle, mw.Webhooks[0].FailurePolicy
		} else {
			vw := &admissionv1.ValidatingWebhookConfiguration{}
			if err := r.Client.Get(ctx, client.ObjectKey{Name: hook}, vw); err != nil || len(vw.Webhooks) == 0 {
				return Check{Name: name, OK: false, Message: fmt.Sprintf("webhook configuration %s not found", hook)}
			}
			bundle, policy = vw.Webhooks[0].ClientConfig.CABundle, vw.Webhooks[0].FailurePolicy
		}
		if len(bundle) == 0 {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("%s has no caBundle yet", hook)}
		}
		if policy == nil || *policy != admissionv1.Fail {
			return Check{Name: name, OK: false, Message: fmt.Sprintf("%s failurePolicy is not Fail yet", hook)}
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
//
// A failed run is retried after RetryInterval rather than Interval. The
// startup run on a fresh pod races SPIRE entry propagation (the controller
// manager registers the pod a few seconds after it exists), and without the
// short retry every operator restart leaves tenants Degraded for a full
// Interval.
type Loop struct {
	Runner        *Runner
	Interval      time.Duration
	RetryInterval time.Duration
}

// defaultRetryInterval applies when RetryInterval is zero.
const defaultRetryInterval = 15 * time.Second

// nextDelay is how long to wait before the next run given the last result.
func (l *Loop) nextDelay(ok bool) time.Duration {
	if ok {
		return l.Interval
	}
	retry := l.RetryInterval
	if retry <= 0 {
		retry = defaultRetryInterval
	}
	return min(retry, l.Interval)
}

// NeedLeaderElection reports false so a follower promoted to leader has a
// fresh result to gate on.
func (l *Loop) NeedLeaderElection() bool { return false }

func (l *Loop) Start(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		res := l.Runner.Run(ctx)
		if res.OK() {
			l.Runner.Logger.Info(ctx, "preflight passed")
		} else {
			l.Runner.Logger.Error(ctx, "preflight failed", logging.Field{Key: "failures", Value: res.Failures()})
		}
		timer.Reset(l.nextDelay(res.OK()))
	}
}
