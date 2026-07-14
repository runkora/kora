package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// PlatformReconciler reconciles OrbitPlatform resources. It drives the full
// Orbit Helm release sequence:
//
//	spire-crds → spire → istio-base → istiod → gateway → orbit-platform → orbit
//
// See docs/adr/2026-03-31-orbit-kubernetes-operator.md for the design.
type PlatformReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RestConfig *rest.Config
	Logger     logging.Logger

	// ChartDir is the base directory containing local Helm charts.
	// In local dev this is the repo's charts/ directory.
	// In production this is the directory where charts are embedded.
	ChartDir string

	// Registry is shared with TenantReconciler so the platform controller can
	// pre-create the orbit-tenants ConfigMap (empty) before orbit-identity
	// starts. Without it, orbit-identity fails to mount the volume on first boot
	// when no OrbitTenant CRs have been applied yet.
	Registry *TenantRegistry

	// OPAImage is the OPA Envoy plugin sidecar image. Configurable so operators
	// can patch CVEs via Helm values without rebuilding the operator binary.
	OPAImage string

	// OPALogLevel is the log level for the OPA sidecar (info, debug, error).
	OPALogLevel string
}

// Reconcile is called by controller-runtime whenever an OrbitPlatform is
// created, updated, or when a requeue fires.
func (r *PlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger

	platform := &OrbitPlatform{}
	if err := r.Get(ctx, req.NamespacedName, platform); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get OrbitPlatform %q: %w", req.Name, err)
	}

	log.Info(ctx, "reconciling OrbitPlatform",
		logging.Field{Key: "name", Value: platform.Name},
		logging.Field{Key: "version", Value: platform.Spec.Version},
		logging.Field{Key: "phase", Value: platform.Status.Phase},
	)

	// Transition to Installing only from an initial/failed/pending state.
	// Never regress from Ready → Installing at the start of every reconcile —
	// that would generate a status patch, trigger a new reconcile event, and
	// produce an infinite Ready↔Installing loop.
	switch platform.Status.Phase {
	case "", PhasePending, PhaseFailed:
		if err := r.setPhase(ctx, platform, PhaseInstalling); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileReleases(ctx, platform); err != nil {
		// Helm reports "another operation (install/upgrade/rollback) is in
		// progress" when a concurrent reconcile races on the same release.
		// Back off and retry — the in-progress operation will complete shortly.
		if isHelmBusy(err) {
			log.Info(ctx, "helm release busy, requeueing",
				logging.Field{Key: "name", Value: platform.Name},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		log.Error(ctx, "helm release reconciliation failed",
			logging.Field{Key: "name", Value: platform.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
		_ = r.setCondition(ctx, platform, ConditionPlatformReady, metav1.ConditionFalse,
			"HelmError", err.Error())
		_ = r.setPhase(ctx, platform, PhaseFailed)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Sync the tenant count into status.
	tenantList := &OrbitTenantList{}
	if err := r.List(ctx, tenantList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list OrbitTenants: %w", err)
	}

	now := metav1.Now()
	platform.Status.TenantCount = len(tenantList.Items)
	platform.Status.Version = platform.Spec.Version
	platform.Status.LastReconciledAt = &now

	_ = r.setCondition(ctx, platform, ConditionPlatformReady, metav1.ConditionTrue,
		"Reconciled", "all platform releases deployed")
	_ = r.setPhase(ctx, platform, PhaseReady)

	log.Info(ctx, "OrbitPlatform reconciliation complete",
		logging.Field{Key: "name", Value: platform.Name},
		logging.Field{Key: "phase", Value: PhaseReady},
	)

	// Periodic re-check every 5 minutes — catches drift.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager registers the controller with the controller-runtime Manager.
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&OrbitPlatform{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// release sequence
// -----------------------------------------------------------------------------

// ensureSystemNamespace creates the orbit-system namespace if it does not exist.
// The namespace is created with the labels required for SPIRE and Istio injection
// before any Helm release runs. This removes the need for a separate bootstrap manifest.
func (r *PlatformReconciler) ensureSystemNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: "orbit-system"}, ns)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get orbit-system namespace: %w", err)
	}
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orbit-system",
			Labels: map[string]string{
				"istio-injection":         "enabled",
				"spiffe.io/spire-managed": "true",
			},
		},
	}
	if createErr := r.Create(ctx, ns); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("create orbit-system namespace: %w", createErr)
	}
	r.Logger.Info(ctx, "created orbit-system namespace")
	return nil
}

// ensureOrbitConfig creates or updates the orbit-config ConfigMap in orbit-system.
// This ConfigMap holds non-secret infrastructure configuration (DB host/name/port/user,
// Redis host/port) derived from the OrbitPlatform CR spec. Keeping it operator-managed
// means the platform team only needs to fill in the OrbitPlatform CR — no separate
// kubectl commands are required during cluster setup.
func (r *PlatformReconciler) ensureOrbitConfig(ctx context.Context, p *OrbitPlatform) error {
	const name = "orbit-config"

	db := p.Spec.Config.DB
	redis := p.Spec.Config.Redis

	desired := map[string]string{
		"DB_HOST":    db.Host,
		"DB_NAME":    db.Name,
		"DB_PORT":    strconv.Itoa(db.Port),
		"DB_USER":    db.User,
		"REDIS_HOST": redis.Host,
		"REDIS_PORT": strconv.Itoa(redis.Port),
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: systemNamespace}, existing)
	if apierrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "platform-config",
				},
			},
			Data: desired,
		}
		if createErr := r.Create(ctx, cm); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create %s ConfigMap: %w", name, createErr)
		}
		r.Logger.Info(ctx, "created orbit-config ConfigMap")
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %s ConfigMap: %w", name, err)
	}

	// Update only if content has drifted.
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	changed := false
	for k, v := range desired {
		if existing.Data[k] != v {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	maps.Copy(existing.Data, desired)
	if patchErr := r.Patch(ctx, existing, patch); patchErr != nil {
		return fmt.Errorf("patch %s ConfigMap: %w", name, patchErr)
	}
	r.Logger.Info(ctx, "updated orbit-config ConfigMap")
	return nil
}

// reconcileReleases drives all Helm releases in dependency order.
// Each step is a named function so failures are easy to attribute.
func (r *PlatformReconciler) reconcileReleases(ctx context.Context, p *OrbitPlatform) error {
	// Ensure the system namespace exists before any Helm release runs.
	if err := r.ensureSystemNamespace(ctx); err != nil {
		return fmt.Errorf("orbit-system namespace: %w", err)
	}

	versions := r.loadVersions()

	if p.Spec.SPIRE.Enabled {
		if err := r.reconcileSPIRECRDs(ctx, p, versions); err != nil {
			return fmt.Errorf("spire-crds: %w", err)
		}
		if err := r.reconcileSPIRE(ctx, p, versions); err != nil {
			return fmt.Errorf("spire: %w", err)
		}
	}

	if p.Spec.Istio.Enabled {
		if err := r.reconcileIstioBase(ctx, versions); err != nil {
			return fmt.Errorf("istio-base: %w", err)
		}
		if err := r.reconcileIstiod(ctx, p, versions); err != nil {
			return fmt.Errorf("istiod: %w", err)
		}
	}

	// orbit-platform must be installed before the gateway so that the
	// ClusterSPIFFEID resources (istio-gateway, orbit-system-component, etc.)
	// exist before the gateway pod starts and requests a SPIFFE SVID from SPIRE.
	// Without this the gateway is stuck waiting for a SPIRE entry that never
	// arrives, causing the Helm install to time out and the operator to loop.
	if err := r.reconcileOrbitPlatform(ctx, p); err != nil {
		return fmt.Errorf("orbit-platform: %w", err)
	}

	if p.Spec.Istio.Enabled {
		if err := r.reconcileGateway(ctx, p, versions); err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
	}

	// Ensure the non-secret platform config ConfigMap exists before deploying
	// orbit services. This derives DB_HOST, DB_NAME, DB_PORT, DB_USER,
	// REDIS_HOST, REDIS_PORT from the OrbitPlatform CR spec so users do not
	// need to create it manually.
	if err := r.ensureOrbitConfig(ctx, p); err != nil {
		return fmt.Errorf("orbit-config: %w", err)
	}

	// Ensure orbit-tenants ConfigMap exists (may be empty if no OrbitTenant CRs
	// have been applied yet). orbit-identity mounts this as a required volume —
	// it will fail to start if the ConfigMap is absent on first boot.
	if r.Registry != nil {
		if err := r.Registry.Sync(ctx); err != nil {
			return fmt.Errorf("orbit-tenants: %w", err)
		}
	}

	if err := r.reconcileOrbit(ctx, p); err != nil {
		return fmt.Errorf("orbit: %w", err)
	}

	return nil
}

func (r *PlatformReconciler) reconcileSPIRECRDs(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire-crds",
		"spire-crds",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRECRDs,
		map[string]any{},
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire-crds: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionFalse,
		"CRDsInstalled", "spire-crds chart deployed; waiting for SPIRE server")
	return nil
}

func (r *PlatformReconciler) reconcileSPIRE(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	clusterName := p.Spec.ClusterName
	if clusterName == "" {
		clusterName = "orbit"
	}

	// All SPIRE values are derived from the OrbitPlatform CR or are Orbit
	// opinionated defaults. No external values file is required.
	spireValues := map[string]any{
		"global": map[string]any{
			"spire": map[string]any{
				"trustDomain": p.Spec.TrustDomain,
				"clusterName": clusterName,
			},
		},
		"spire-server": map[string]any{
			"controllerManager": map[string]any{
				"identities": map[string]any{
					"clusterSPIFFEIDs": map[string]any{
						// Disable the default ClusterSPIFFEID — Orbit manages
						// its own via the orbit-platform chart.
						"default": map[string]any{"enabled": false},
					},
				},
			},
			"nodeAttestor": map[string]any{
				"k8sPSAT": map[string]any{
					"enabled": true,
					"clusters": map[string]any{
						clusterName: map[string]any{
							"service_account_allow_list": []string{"spire-system:spire-agent"},
						},
					},
				},
			},
		},
		"spire-agent": map[string]any{
			"sds": map[string]any{
				"enabled":               true,
				"defaultSVIDName":       "default",
				"defaultBundleName":     "ROOTCA",
				"defaultAllBundlesName": "ALL",
			},
			"nodeAttestor": map[string]any{
				"k8sPSAT": map[string]any{"enabled": true},
			},
			"workloadAttestors": map[string]any{
				"k8s": map[string]any{
					"enabled":                   true,
					"skipKubeletVerification":   true,
					"disableContainerSelectors": true,
					"useNewContainerLocator":    false,
				},
			},
		},
		"spiffe-csi-driver":        map[string]any{"enabled": true},
		"spire-controller-manager": map[string]any{"enabled": true},
	}

	helm := NewHelmClient("spire-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"spire",
		"spire",
		"https://spiffe.github.io/helm-charts-hardened",
		versions.SPIRE,
		spireValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade spire: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionSPIREReady, metav1.ConditionTrue,
		"Deployed", "spire chart deployed")
	return nil
}

func (r *PlatformReconciler) reconcileIstioBase(ctx context.Context, versions *chartVersions) error {
	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"istio-base",
		"base",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		map[string]any{},
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istio-base: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileIstiod(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	if p.Spec.MeshJWTValidation.Enabled {
		if err := r.ensureMeshJWKSConfigMap(ctx, p); err != nil {
			return fmt.Errorf("ensure mesh JWKS configmap: %w", err)
		}
	}

	istioValues, err := buildIstiodValues(p, r.OPAImage, r.OPALogLevel)
	if err != nil {
		return err
	}

	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err = helm.InstallOrUpgrade(ctx,
		"istiod",
		"istiod",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		istioValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade istiod: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionIstioReady, metav1.ConditionTrue,
		"Deployed", "istiod deployed")
	return nil
}

// buildIstiodValues renders the Helm values for the istiod release. Pure
// (no cluster calls) so the pinned constraints below can be asserted by
// unit test without a live Helm/Kubernetes backend:
//
//   - ENABLE_NATIVE_SIDECARS is pinned "false" under the chart's FLAT
//     top-level `env:` key (`.Values.pilot.env` is never read for istiod's
//     own Deployment).
//   - meshConfig.discoverySelectors excludes orbit.io/role=devenv
//     namespaces, but only for host installs (p.Spec.SPIRE.Enabled) — an
//     in-vcluster istiod has no business discovering devenv namespaces at
//     all and must not carry this selector.
//   - MeshJWTValidation, when enabled, sets JWT_RULE (YAML-escaped via
//     helmDoubleQuotedScalarEscape) with a mandatory non-empty audiences
//     list, plus the jwks-mirror sidecar's volume/initContainer.
func buildIstiodValues(p *OrbitPlatform, opaImage, opaLogLevel string) (map[string]any, error) {
	// The SPIRE and identity-proxy sidecar injection templates are Orbit's
	// internal wiring between its components. They are generated from the
	// OrbitPlatform CR — the user should never need to write or maintain them.
	meshConfig := map[string]any{
		"trustDomain": p.Spec.TrustDomain,
		"defaultConfig": map[string]any{
			"discoveryAddress": "istiod.istio-system.svc:15012",
		},
	}
	// The host's istiod runs a namespace controller that copies the
	// istio-ca-root-cert ConfigMap into every namespace it discovers —
	// cluster-wide by default. A dev environment's flattened host namespace
	// (orbit-devenv-{id}) hosts its OWN independent istiod, inside its own
	// vcluster, publishing a ConfigMap under the exact same name — so without
	// this exclusion the host's istiod clobbers it with the HOST's root,
	// which cannot validate the dev environment's own istiod's serving
	// certificate. p.Spec.SPIRE.Enabled is false only for in-vcluster
	// installs — this only ever applies to the host's own istiod, which has
	// no business discovering devenv namespaces at all.
	if p.Spec.SPIRE.Enabled {
		meshConfig["discoverySelectors"] = []map[string]any{
			{
				"matchExpressions": []map[string]any{
					{"key": "orbit.io/role", "operator": "NotIn", "values": []string{"devenv"}},
				},
			},
		}
	}
	// istiod's own container env goes under the chart's FLAT top-level `env:`
	// key — the deployment template's env-rendering block ranges over
	// .Values.env directly. `.Values.pilot.env` is only referenced by the
	// chart's injection-template files and by the umbrella-chart
	// compatibility profiles, never by istiod's own Deployment, so values
	// placed there silently never reach the pilot process.
	istiodEnv := map[string]any{
		// Istio's "Compatibility" auto-detection silently switches
		// istio-proxy from a `containers` entry to a native-sidecar
		// `initContainers` entry based on the target API server's reported
		// Kubernetes version. buildSpireInjectionTemplate() and
		// buildIdentityProxyInjectionTemplate() patch `containers` only, so
		// that switch produces an invalid pod. Orbit already solves sidecar
		// startup ordering itself via holdApplicationUntilProxyStarts, so
		// native sidecars buy nothing here — pin this off so injection is
		// deterministic regardless of what version string any given
		// cluster reports.
		"ENABLE_NATIVE_SIDECARS": "false",
	}
	istioValues := map[string]any{
		"meshConfig": meshConfig,
		"env":        istiodEnv,
		"sidecarInjectorWebhook": map[string]any{
			"templates": map[string]any{
				"spire": buildSpireInjectionTemplate(),
				"identity-proxy": buildIdentityProxyInjectionTemplate(
					orbitImageRef(defaultIdentityProxyImage, "identity-proxy", p.Spec.ImageRegistry),
				),
				"opa-policy": buildOPAPolicyInjectionTemplate(opaImage, opaLogLevel),
			},
		},
	}
	// Dev-environment installs: this istiod's mesh workloads are host pods
	// carrying HOST-signed bound ServiceAccount tokens, which the default
	// KubeJWTAuthenticator (TokenReview against the VIRTUAL apiserver) can
	// never validate. JWT_RULE prepends a pure public-key JWT authenticator
	// for the host issuer instead; the JWKS it validates against is served
	// from pod loopback by a credential-less static-file sidecar, because
	// istiod's own JWKS fetch can attach neither a bearer token nor a custom
	// CA and the host's JWKS endpoint requires authentication. The issuer
	// and JWKS content arrive on the CR spec, fetched by the HOST operator
	// (devenvironment_jwks.go).
	if p.Spec.MeshJWTValidation.Enabled {
		jwtRule, err := json.Marshal(map[string]any{
			"issuer":   p.Spec.MeshJWTValidation.Issuer,
			"jwks_uri": meshJWKSMirrorURL,
			// Mesh sidecars' bootstrap tokens carry the stock Istio audience
			// ("istio-ca", global.sds.token.aud — unmodified by any Orbit
			// injection template). JWT_RULE's authenticator rejects EVERY
			// token when its expected-audiences list is empty, so this must
			// be set explicitly despite being an optional field.
			"audiences": []string{"istio-ca"},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal JWT_RULE: %w", err)
		}
		// The istiod chart's own env-rendering (templates/deployment.yaml)
		// interpolates values with zero escaping: `value: "{{ $val }}"`. A
		// raw JSON string dropped into that template breaks the rendered
		// YAML outright the moment it contains a `"` — confirmed live
		// ("YAML parse error ... did not find expected key"). Pre-escaping
		// with the same backslash/quote rules as a YAML double-quoted
		// scalar (a strict subset of Go's escaping, since json.Marshal never
		// emits other control characters) makes the chart's own naive
		// quoting produce valid YAML that decodes back to this exact string.
		istiodEnv["JWT_RULE"] = helmDoubleQuotedScalarEscape(string(jwtRule))
		istioValues["volumes"] = []map[string]any{
			{
				"name": "mesh-jwks",
				"configMap": map[string]any{
					"name":     meshJWKSConfigMapName,
					"optional": true,
				},
			},
		}
		// A Kubernetes-native sidecar (initContainers + restartPolicy:
		// Always — GA on every cluster this runs on) serving the mounted
		// JWKS file over loopback. Loopback-only bind: nothing outside the
		// istiod pod can reach it, and istiod's fetch of it needs no TLS
		// and no credential.
		istioValues["initContainers"] = []map[string]any{
			{
				"name":          "jwks-mirror",
				"image":         meshJWKSMirrorImage,
				"restartPolicy": "Always",
				"command": []string{
					"httpd", "-f", "-p", meshJWKSMirrorListen, "-h", "/served",
				},
				"volumeMounts": []map[string]any{
					{"name": "mesh-jwks", "mountPath": "/served", "readOnly": true},
				},
			},
		}
	}
	return istioValues, nil
}

const (
	// meshJWKSConfigMapName holds the external issuer's JWKS document
	// (spec.meshJWTValidation.jwks) in istiod's own namespace, mounted into
	// the jwks-mirror sidecar. Public key material only.
	meshJWKSConfigMapName = "orbit-mesh-jwks"

	// meshJWKSConfigMapKey is the ConfigMap data key — and therefore the
	// filename busybox httpd serves, so it must match the path component of
	// meshJWKSMirrorURL.
	meshJWKSConfigMapKey = "jwks"

	// meshJWKSMirrorListen binds the mirror to pod loopback only: reachable
	// by istiod (same network namespace), unreachable from anywhere else.
	meshJWKSMirrorListen = "127.0.0.1:8899"

	// meshJWKSMirrorURL is where istiod's JWT_RULE authenticator fetches the
	// JWKS — plaintext loopback, so no credential or CA trust is needed on
	// istiod's side (it has no way to supply either; see MeshJWTValidationSpec).
	meshJWKSMirrorURL = "http://" + meshJWKSMirrorListen + "/" + meshJWKSConfigMapKey

	// meshJWKSMirrorImage only needs a static-file HTTP server (busybox
	// httpd). Pinned like the chart versions; not an Orbit image, so
	// spec.imageRegistry does not apply.
	meshJWKSMirrorImage = "busybox:1.36"
)

// helmDoubleQuotedScalarEscape escapes a string for embedding inside the
// istiod chart's unescaped `value: "{{ $val }}"` env template. Backslashes
// are escaped first so a literal backslash already in the input isn't
// mistaken for the start of one of these escapes once quotes are escaped
// next — the same ordering strconv.Quote uses, restricted to the two
// characters YAML's double-quoted scalar and this input can actually
// contain (json.Marshal never emits raw control characters).
func helmDoubleQuotedScalarEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// ensureMeshJWKSConfigMap creates or updates the ConfigMap backing the
// jwks-mirror sidecar from spec.meshJWTValidation.jwks. Runs before the
// istiod Helm install in reconcileIstiod, so the mounted content exists by
// the time the istiod pod first starts; istio-base has already created the
// istio-system namespace by then.
func (r *PlatformReconciler) ensureMeshJWKSConfigMap(ctx context.Context, p *OrbitPlatform) error {
	data := map[string]string{meshJWKSConfigMapKey: p.Spec.MeshJWTValidation.JWKS}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: meshJWKSConfigMapName, Namespace: "istio-system"}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: meshJWKSConfigMapName, Namespace: "istio-system"},
			Data:       data,
		}
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create ConfigMap %q: %w", meshJWKSConfigMapName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get ConfigMap %q: %w", meshJWKSConfigMapName, err)
	default:
		if maps.Equal(existing.Data, data) {
			return nil
		}
		existing.Data = data
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			return fmt.Errorf("update ConfigMap %q: %w", meshJWKSConfigMapName, updateErr)
		}
		return nil
	}
}

func (r *PlatformReconciler) reconcileGateway(ctx context.Context, p *OrbitPlatform, versions *chartVersions) error {
	serviceType := p.Spec.Gateway.ServiceType
	if serviceType == "" {
		serviceType = "LoadBalancer"
	}

	// Wire the Istio ingress gateway to SPIRE so it shares the same CA root as
	// all orbit-system sidecar proxies. Without this the gateway uses istiod's
	// self-signed CA while sidecars use SPIRE's CA, causing TLS verify failures
	// on every inbound route (CERTIFICATE_VERIFY_FAILED / SAN mismatch).
	//
	// This MUST go through the sidecar-injection template mechanism
	// (podAnnotations["inject.istio.io/templates"]), not the gateway chart's
	// own raw env/volumes values. Confirmed empirically: the gateway chart's
	// "gateway" injection template already defines PILOT_CERT_PROVIDER and
	// ISTIO_META_CLUSTER_ID with its own defaults, and the chart's
	// proxy.istio.io/overrides mechanism (fed by .Values.env) does not win
	// against a key the base template already sets — only genuinely new keys
	// (e.g. SPIFFE_ENDPOINT_SOCKET) survive that path. Layering "spire" as an
	// additional injection template alongside the base "gateway" template
	// uses the same strategic-merge-by-name logic already proven correct for
	// every other SPIRE-identified workload (buildSpireInjectionTemplate,
	// applied via "sidecar,spire" elsewhere) — it, and not raw values, is
	// what actually overrides these two keys.
	serviceValues := map[string]any{
		"type": serviceType,
	}
	// Expose the shared TLS-passthrough port used to route external kubectl
	// traffic to each dev environment's own vcluster API server
	// (devenvironment_ingress.go's devEnvAPIGatewayPort). Only on the HOST's
	// own gateway: p.Spec.SPIRE.Enabled is false only for the gateway release
	// installed *inside* a dev environment's own vcluster, which has no
	// vcluster of its own to route to.
	if p.Spec.SPIRE.Enabled {
		serviceValues["ports"] = []map[string]any{
			{"name": "status-port", "port": 15021, "protocol": "TCP", "targetPort": 15021},
			{"name": "http2", "port": 80, "protocol": "TCP", "targetPort": 80},
			{"name": "https", "port": 443, "protocol": "TCP", "targetPort": 443},
			{"name": "tls-devenv-api", "port": devEnvAPIGatewayPort, "protocol": "TCP", "targetPort": devEnvAPIGatewayPort},
		}
	}
	gatewayValues := map[string]any{
		"service": serviceValues,
		"podAnnotations": map[string]any{
			"sidecar.istio.io/inject":   "true",
			"inject.istio.io/templates": "gateway,spire",
			"prometheus.io/port":        "15020",
			"prometheus.io/scrape":      "true",
			"prometheus.io/path":        "/stats/prometheus",
		},
	}

	helm := NewHelmClient("istio-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"gateway",
		"gateway",
		"https://istio-release.storage.googleapis.com/charts",
		versions.Istio,
		gatewayValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade gateway: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbitPlatform(ctx context.Context, p *OrbitPlatform) error {
	// Start with values from the local values file (e.g. deploy/local/platform-values.yaml
	// in dev, /etc/orbit/values/platform-values.yaml in-cluster).
	platformValues := r.loadValuesFile("platform-values.yaml")

	// Overlay computed values — these always win over file-based values.
	platformValues = mergeMaps(platformValues, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"namespaces": map[string]any{
			"system":  "orbit-system",
			"tenants": r.tenantNamespaces(ctx),
		},
		"spire": map[string]any{
			"enabled": p.Spec.SPIRE.Enabled,
		},
		// NetworkPolicy enforcement inside a dev environment's vcluster is
		// unreliable regardless of rule shape — deleting the default-deny-all
		// + allow-dns policies made a persistently crash-looping DB migration
		// Job succeed on its very first retry, with no other change. This is
		// a kube-router/vcluster-sync limitation on the translated (synced)
		// pod/namespace objects, not something a NetworkPolicy YAML change
		// can fix. A dev environment is already fenced from the host by
		// orbit-system's own default-deny-all on the HOST side (one
		// namespace, one owner), so this chart's own internal policy is
		// redundant defense-in-depth there — gated on the same "is this a
		// devenv install" signal (spire disabled) used everywhere else.
		"networkPolicy": map[string]any{
			"enabled": p.Spec.SPIRE.Enabled,
		},
	})
	// Merge any raw values from the CR spec (highest priority).
	platformValues = mergeMaps(platformValues, p.Spec.PlatformValues)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err := helm.InstallOrUpgrade(ctx,
		"orbit-platform",
		r.chartPath("orbit-platform"),
		"", // local chart path — no repoURL
		"",
		platformValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade orbit-platform: %w", err)
	}
	return nil
}

func (r *PlatformReconciler) reconcileOrbit(ctx context.Context, p *OrbitPlatform) error {
	secretName, err := r.platformSecretName(ctx, p)
	if err != nil {
		return err
	}

	// Start with values from the local values file (e.g. deploy/local/orbit-values.yaml
	// in dev, /etc/orbit/values/orbit-values.yaml in-cluster). This carries image
	// repository/tag, appEnv, imagePullPolicy, and other deployment-specific settings.
	orbitValues := r.loadValuesFile("orbit-values.yaml")

	// Overlay computed values — these always win over file-based values.
	// Resource names (orbit-config, orbit-tenants, orbit-tenant-oidc-secrets,
	// orbit-secrets) are operator-managed conventions. Users should never need
	// to set these in a values file.
	orbitValues = mergeMaps(orbitValues, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"identity": map[string]any{
			"existingSecret":              secretName,
			"existingConfigMap":           "orbit-config",
			"existingTenantsConfigMap":    "orbit-tenants",
			"existingClientSecretsSecret": "orbit-tenant-oidc-secrets",
		},
		"platform": map[string]any{
			"namespaces": map[string]any{
				"system": "orbit-system",
			},
			// spiffe.enabled means "workloads carry SPIFFE identities and the
			// chart's DestinationRules must pin their SANs" — true when this
			// cluster runs SPIRE itself, and equally true on a dev-environment
			// install, where identity is HOST-provided and every SVID is
			// prefixed /devenv/<id>. Without the DRs, Istio falls back to the
			// standard ns/sa SAN convention and rejects every SPIRE-issued
			// custom SAN with a 503.
			"spiffe": map[string]any{
				"enabled":  p.Spec.SPIRE.Enabled || p.Spec.DevEnvironmentID != "",
				"devEnvID": p.Spec.DevEnvironmentID,
			},
		},
	})

	// Apply db/redis config if set.
	if p.Spec.Config.DB.Host != "" {
		orbitValues = mergeMaps(orbitValues, map[string]any{
			"config": map[string]any{
				"db": map[string]any{
					"host": p.Spec.Config.DB.Host,
					"name": p.Spec.Config.DB.Name,
					"port": p.Spec.Config.DB.Port,
					"user": p.Spec.Config.DB.User,
				},
			},
		})
	}
	if p.Spec.Config.Redis.Host != "" {
		orbitValues = mergeMaps(orbitValues, map[string]any{
			"config": map[string]any{
				"redis": map[string]any{
					"host": p.Spec.Config.Redis.Host,
					"port": p.Spec.Config.Redis.Port,
				},
			},
		})
	}

	// Merge any raw values from the CR spec.
	orbitValues = mergeMaps(orbitValues, p.Spec.OrbitValues)

	helm := NewHelmClient("orbit-system", r.RestConfig, r.Logger)
	_, err = helm.InstallOrUpgrade(ctx,
		"orbit",
		r.chartPath("orbit"),
		"", // local chart path — no repoURL
		"",
		orbitValues,
	)
	if err != nil {
		return fmt.Errorf("install/upgrade orbit: %w", err)
	}
	_ = r.setCondition(ctx, p, ConditionIdentityReady, metav1.ConditionTrue,
		"Deployed", "orbit chart deployed")
	return nil
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// setPhase updates the OrbitPlatform status phase, patching the API server.
// It is a no-op when the phase is already set to the requested value, which
// prevents spurious status patches from triggering extra reconcile events.
func (r *PlatformReconciler) setPhase(ctx context.Context, p *OrbitPlatform, phase string) error {
	if p.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(p.DeepCopy())
	p.Status.Phase = phase
	if err := r.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("patch OrbitPlatform status phase to %q: %w", phase, err)
	}
	return nil
}

// setCondition sets a named condition on the OrbitPlatform status.
func (r *PlatformReconciler) setCondition(
	ctx context.Context,
	p *OrbitPlatform,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	patch := client.MergeFrom(p.DeepCopy())
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: p.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("patch OrbitPlatform condition %q: %w", condType, err)
	}
	return nil
}

// chartPath returns the absolute path to a chart in the ChartDir.
func (r *PlatformReconciler) chartPath(name string) string {
	return fmt.Sprintf("%s/%s", r.ChartDir, name)
}

// platformSecretName resolves the platform secret name from the CR spec,
// defaulting to "orbit-secrets" when not explicitly set.
func (r *PlatformReconciler) platformSecretName(ctx context.Context, p *OrbitPlatform) (string, error) {
	name := p.Spec.Secrets.PlatformSecretRef.Name
	if name == "" {
		name = "orbit-secrets"
	}
	ns := p.Spec.Secrets.PlatformSecretRef.Namespace
	if ns == "" {
		ns = "orbit-system"
	}

	// Verify the Secret exists before referencing it; emit a clear error if not.
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("platform secret %q not found in namespace %q — create it before applying OrbitPlatform", name, ns)
		}
		return "", fmt.Errorf("get platform secret %q: %w", name, err)
	}
	return name, nil
}

// tenantNamespaces returns the list of tenant namespace names from all active
// OrbitTenant CRs — used to populate orbit-platform's namespaces.tenants value.
func (r *PlatformReconciler) tenantNamespaces(ctx context.Context) []string {
	list := &OrbitTenantList{}
	if err := r.List(ctx, list); err != nil {
		r.Logger.Warn(ctx, "failed to list OrbitTenants for namespace list",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	ns := make([]string, 0, len(list.Items))
	for i := range list.Items {
		t := &list.Items[i]
		if t.Spec.Active {
			ns = append(ns, tenantNamespace(t))
		}
	}
	return ns
}

// loadVersions reads chart version pins. In production these are baked into
// the operator image; in local dev they come from scripts/cluster/versions.env.
// For now the versions are constants matching the values in versions.env.
func (r *PlatformReconciler) loadVersions() *chartVersions {
	return defaultChartVersions()
}

// loadValuesFile reads a Helm values YAML file from the ChartDir's parent
// (deploy/local/ in local dev, the operator image's config dir in production).
// Returns an empty map when the file is absent.
func (r *PlatformReconciler) loadValuesFile(filename string) map[string]any {
	// In local dev mode ChartDir is e.g. "/path/to/repo/charts".
	// Values files are in the sibling "deploy/local/" directory.
	// In production the operator image embeds values at /etc/orbit/values/.
	// We probe both locations.
	candidates := []string{
		fmt.Sprintf("%s/../deploy/local/%s", r.ChartDir, filename),
		fmt.Sprintf("/etc/orbit/values/%s", filename),
	}
	for _, path := range candidates {
		data, err := readYAMLFile(path)
		if err == nil {
			return data
		}
	}
	r.Logger.Warn(context.Background(), "values file not found, using empty map",
		logging.Field{Key: "filename", Value: filename},
	)
	return map[string]any{}
}
