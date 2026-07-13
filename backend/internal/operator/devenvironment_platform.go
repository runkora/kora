// In-vcluster Orbit platform install for OrbitDevEnvironment (decision 2 of
// docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
//
// Once an environment's vcluster is Ready, this file drives the second half
// of "each environment is a self-contained Orbit install": installing the
// orbit-operator chart inside the vcluster's own API server, then applying an
// OrbitPlatform CR (spire.enabled: false — identity is host-provided, option
// C3) and a seeded OrbitTenant CR so the in-vcluster operator takes over the
// real release sequence (spire-crds → spire → istio-base → istiod → gateway →
// orbit-platform → orbit), exactly as it would on any other cluster.
package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// vclusterKubeconfigSecretPrefix + vclusterReleaseName gives the Secret
	// the vcluster chart creates holding the generated kubeconfig for
	// connecting to the vcluster's own API server — distinct from
	// "vc-config-{release}", which holds the rendered vcluster.yaml values
	// (see the ADR's Corrections section).
	vclusterKubeconfigSecretPrefix = "vc-"
	vclusterKubeconfigSecretKey    = "config"

	// vclusterAPIPort is the port the vcluster chart's own Service exposes
	// the virtual API server on (confirmed against a live vcluster: the
	// Service named after the release listens on 443, matching
	// scripts/cluster/spike-vcluster-up.sh's port-forward target).
	vclusterAPIPort = 443

	// In-vcluster install targets. One orbit-operator release, one
	// OrbitPlatform, one seeded OrbitTenant per environment — there is
	// exactly one of each inside any given vcluster, so fixed names (rather
	// than names templated per environment) are sufficient.
	inVClusterOperatorReleaseName = "orbit-operator"
	inVClusterOperatorNamespace   = "orbit-operator"
	inVClusterOperatorChartName   = "orbit-operator"
	inVClusterOrbitPlatformName   = "orbit"
	inVClusterOrbitTenantName     = "devenv"

	// inVClusterOrbitPlatformVersion is the Orbit chart version installed
	// inside every environment. Matches deploy/local/orbit-platform.yaml —
	// dev environments track the same version local k3d dev does until
	// decision 4's spec.ref selection is implemented (Phase 3+).
	inVClusterOrbitPlatformVersion = "0.1.0"
)

// VClusterClientFactory constructs a controller-runtime client targeting a
// vcluster's own API server. Production wiring supplies newVClusterClient;
// tests inject a factory returning a fake client (typically pre-loaded with
// the OrbitPlatform/OrbitTenant CRDs "installed") so reconcile logic can be
// exercised without a live vcluster.
type VClusterClientFactory func(*rest.Config) (client.Client, error)

// NewVClusterClientFactory returns the production VClusterClientFactory:
// every call builds a real controller-runtime client against the given
// rest.Config, using the operator's own scheme so it can create/patch
// OrbitPlatform and OrbitTenant objects once the in-vcluster orbit-operator
// chart has installed their CRDs.
func NewVClusterClientFactory(scheme *runtime.Scheme) VClusterClientFactory {
	return func(cfg *rest.Config) (client.Client, error) {
		c, err := client.New(cfg, client.Options{Scheme: scheme})
		if err != nil {
			return nil, fmt.Errorf("build vcluster client: %w", err)
		}
		return c, nil
	}
}

// VClusterRestConfigFactory resolves a vcluster's rest.Config for a given
// environment namespace. A reconciler field (rather than calling
// vclusterRestConfig directly) so tests can substitute a dummy config without
// a real vcluster kubeconfig Secret ever existing in the fake client.
type VClusterRestConfigFactory func(ctx context.Context, namespace string) (*rest.Config, error)

// NewVClusterRestConfigFactory returns the production VClusterRestConfigFactory,
// bound to the host cluster's own client.
func NewVClusterRestConfigFactory(hostClient client.Client) VClusterRestConfigFactory {
	return func(ctx context.Context, namespace string) (*rest.Config, error) {
		return VClusterRestConfig(ctx, hostClient, namespace)
	}
}

// VClusterRestConfig builds a rest.Config the HOST-side operator can use to
// reach a vcluster's own API server directly, without `vcluster connect`'s
// background kubectl port-forward — which the chart's generated kubeconfig
// assumes via its default https://localhost:8443 host (see the ADR's
// Corrections section; the operator has no such port-forward running).
//
// Confirmed empirically against a live vcluster (0.35.1): the chart's own
// server certificate carries "{release}" and "{release}.{namespace}" as SANs
// specifically for this in-cluster access pattern, alongside the standard
// "kubernetes.default..." names. The short two-label form resolves correctly
// from a pod in ANY namespace via the default "svc.cluster.local" search
// suffix in /etc/resolv.conf, so no ".svc"/".svc.cluster.local" suffix is
// needed (and using one would NOT match the certificate's SAN list).
func VClusterRestConfig(ctx context.Context, hostClient client.Client, namespace string) (*rest.Config, error) {
	secretName := vclusterKubeconfigSecretPrefix + vclusterReleaseName
	secret := &corev1.Secret{}
	if err := hostClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, secret); err != nil {
		return nil, fmt.Errorf("get vcluster kubeconfig secret %q: %w", secretName, err)
	}
	kubeconfig, ok := secret.Data[vclusterKubeconfigSecretKey]
	if !ok {
		return nil, fmt.Errorf("vcluster kubeconfig secret %q missing key %q", secretName, vclusterKubeconfigSecretKey)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse vcluster kubeconfig: %w", err)
	}
	cfg.Host = fmt.Sprintf("https://%s.%s:%d", vclusterReleaseName, namespace, vclusterAPIPort)
	return cfg, nil
}

// reconcileInVClusterPlatform installs the orbit-operator chart inside the
// environment's vcluster, then applies the OrbitPlatform and seeded
// OrbitTenant CRs that hand off to the in-vcluster operator's own reconcile
// loop. Called from reconcileReady once ensureVCluster has succeeded — the
// vcluster's own API server (and therefore its kubeconfig Secret) only exists
// after that.
func (r *DevEnvironmentReconciler) reconcileInVClusterPlatform(ctx context.Context, env *OrbitDevEnvironment) error {
	ns := devEnvNamespace(env)

	vcCfg, err := r.VClusterRestConfigFactory(ctx, ns)
	if err != nil {
		return fmt.Errorf("build vcluster rest config: %w", err)
	}

	vcClient, err := r.VClusterClientFactory(vcCfg)
	if err != nil {
		return fmt.Errorf("build vcluster client: %w", err)
	}

	if dnsErr := r.ensureInVClusterCoreDNSSplitHorizon(ctx, vcClient); dnsErr != nil {
		return fmt.Errorf("ensure in-vcluster coredns split-horizon config: %w", dnsErr)
	}
	dnsReady, dnsReadyErr := inVClusterCoreDNSRolloutComplete(ctx, vcClient)
	if dnsReadyErr != nil {
		return fmt.Errorf("check in-vcluster coredns rollout: %w", dnsReadyErr)
	}
	if !dnsReady {
		// Not an error: ensureInVClusterCoreDNSSplitHorizon may have just
		// forced a rollout restart (or the vcluster's own CoreDNS Deployment
		// may not exist yet on a brand-new environment). Installing the
		// nested orbit-operator chart — and therefore letting it eventually
		// schedule orbit-identity — before this completes is exactly the race
		// that crash-loops orbit-identity's synchronous, non-retrying OIDC
		// discovery against a stale DNS answer (confirmed live). The caller's
		// 5s fast-requeue (reconcileInVClusterPlatformAndReadiness) retries
		// this same call once CoreDNS is serving its current config.
		return nil
	}

	helm := r.InVClusterHelmFactory(inVClusterOperatorNamespace, vcCfg)
	if _, installErr := helm.InstallOrUpgrade(ctx,
		inVClusterOperatorReleaseName, r.inVClusterOperatorChartPath(), "", "",
		r.inVClusterOperatorValues(),
	); installErr != nil {
		return fmt.Errorf("install in-vcluster orbit-operator: %w", installErr)
	}

	secretValues, err := ensureInVClusterSecrets(ctx, vcClient)
	if err != nil {
		return fmt.Errorf("ensure in-vcluster platform secret: %w", err)
	}
	if frontendSecretErr := ensureInVClusterFrontendSecret(ctx, vcClient); frontendSecretErr != nil {
		return fmt.Errorf("ensure in-vcluster frontend secret: %w", frontendSecretErr)
	}
	externalDexIssuer := r.appDexIssuerURL(env)
	if fixturesErr := ensureFixtures(ctx, vcClient, secretValues, r.appAuthCallbackURL(), externalDexIssuer); fixturesErr != nil {
		return fmt.Errorf("ensure in-vcluster fixtures: %w", fixturesErr)
	}
	if migErr := r.ensureInVClusterMigrations(ctx, vcClient); migErr != nil {
		return fmt.Errorf("ensure in-vcluster db migrations: %w", migErr)
	}

	trustDomain, err := r.hostTrustDomain(ctx)
	if err != nil {
		return fmt.Errorf("determine host trust domain: %w", err)
	}

	// The in-vcluster istiod must validate HOST-signed workload tokens
	// (devenvironment_jwks.go) — fetch the host issuer/JWKS fresh on every
	// reconcile so a signing-key rotation propagates within one requeue.
	issuer, jwks, err := r.HostJWKSFetcher(ctx)
	if err != nil {
		return fmt.Errorf("fetch host service-account issuer/JWKS: %w", err)
	}
	meshJWT := MeshJWTValidationSpec{Enabled: true, Issuer: issuer, JWKS: jwks}

	if err := r.applyInVClusterOrbitPlatform(ctx, vcClient, env, trustDomain, meshJWT); err != nil {
		return fmt.Errorf("apply in-vcluster OrbitPlatform: %w", err)
	}
	if err := r.applyInVClusterOrbitTenant(ctx, vcClient, externalDexIssuer); err != nil {
		return fmt.Errorf("apply in-vcluster OrbitTenant: %w", err)
	}
	return nil
}

// inVClusterOperatorChartPath resolves the local filesystem path to the
// orbit-operator chart the same way PlatformReconciler.chartPath does for
// orbit-platform/orbit — via ChartDir, baked into the operator image at
// /etc/orbit/charts (see Dockerfile.operator) or ../charts in local dev.
func (r *DevEnvironmentReconciler) inVClusterOperatorChartPath() string {
	return fmt.Sprintf("%s/%s", r.ChartDir, inVClusterOperatorChartName)
}

// inVClusterOperatorValues mirrors deploy/local/operator-values.yaml: debug
// logging and no leader election, since every environment's orbit-operator is
// a single replica with nothing else to contend with.
func (r *DevEnvironmentReconciler) inVClusterOperatorValues() map[string]any {
	repo := "orbit-operator"
	if r.ImageRegistry != "" {
		repo = r.ImageRegistry + "/orbit-operator"
	}
	return map[string]any{
		"logLevel": "debug",
		"image": map[string]any{
			"repository": repo,
			"tag":        "latest",
			"pullPolicy": "Always",
		},
		"leaderElection": false,
	}
}

// inVClusterPlatformConfig points the OrbitPlatform CR's db/redis config at
// the fixtures ensureFixtures deploys inside the same vcluster (decision 2 —
// environments never share the host's Postgres/Redis).
func (r *DevEnvironmentReconciler) inVClusterPlatformConfig() PlatformConfigSpec {
	return PlatformConfigSpec{
		DB: DBConfig{
			Host: inVClusterPostgresHost(),
			Name: inVClusterDBName,
			Port: inVClusterPostgresPort,
			User: inVClusterDBUser,
		},
		Redis: RedisConfig{
			Host: inVClusterRedisHost(),
			Port: inVClusterRedisPort,
		},
	}
}

// hostTrustDomain reads the trust domain from the HOST cluster's own
// OrbitPlatform CR. Every in-vcluster OrbitPlatform must be told this same
// value (not a per-environment one): identity is host-provided (option C3),
// so in-vcluster Istio's mTLS trust config has to agree with the trust domain
// the HOST SPIRE actually issues devenv-prefixed SVIDs under. OrbitPlatform
// is cluster-scoped, "one per cluster" by convention, so the first result is
// authoritative.
func (r *DevEnvironmentReconciler) hostTrustDomain(ctx context.Context) (string, error) {
	platforms := &OrbitPlatformList{}
	if err := r.List(ctx, platforms); err != nil {
		return "", fmt.Errorf("list host OrbitPlatform: %w", err)
	}
	if len(platforms.Items) == 0 {
		return "", errors.New("no host OrbitPlatform found — cannot derive devenv trust domain")
	}
	return platforms.Items[0].Spec.TrustDomain, nil
}

// coreDNSCustomConfigMapName / coreDNSNamespace name the split-horizon
// CoreDNS patch scripts/cluster/dev-cluster-up.sh applies to the HOST
// cluster's own CoreDNS (kube-system/coredns-custom) — mapping
// AppExternalURL's *.orbit.local.clearroute.io hostnames to the Docker
// bridge gateway IP instead of whatever a laptop's own resolver (dnsmasq,
// answering 127.0.0.1 — meaningless inside any pod's network namespace)
// would otherwise return. Real infrastructure has no such ConfigMap (a
// single real DNS zone needs no split horizon), so its absence is expected,
// not an error.
const (
	coreDNSCustomConfigMapName = "coredns-custom"
	coreDNSNamespace           = "kube-system"

	// hostCoreDNSPort is the port scripts/cluster/dev-cluster-up.sh's
	// zone-header string literally names (its main server block is ".:53",
	// standard k3d CoreDNS) — the substring rewritten by
	// rewriteCoreDNSCustomForVCluster below.
	hostCoreDNSPort = 53

	// inVClusterCoreDNSPort is the port the vcluster chart's own k8s-distro
	// CoreDNS server block actually listens on: confirmed live against a
	// running vcluster (0.35.1), its rendered Corefile reads ".:1053", with
	// the synced "kube-dns" Service mapping the standard port 53 to it. A
	// copied-verbatim custom zone block that keeps the host's ":53" binds a
	// port nothing inside this pod ever queries and never actually
	// intercepts anything — confirmed live: with the ConfigMap copied
	// unmodified, CoreDNS happily loaded "orbit.local.clearroute.io.:53"
	// alongside ".:1053", but every real query still fell through the main
	// block to the upstream resolver, exactly as if the custom zone did not
	// exist at all.
	inVClusterCoreDNSPort = 1053

	// inVClusterCoreDNSDeploymentName is the vcluster's own embedded CoreDNS
	// Deployment — standard k3s-distro naming, confirmed live: "coredns" in
	// the same coreDNSNamespace ("kube-system") the coredns-custom ConfigMap
	// above targets.
	inVClusterCoreDNSDeploymentName = "coredns"

	// coreDNSCustomChecksumAnnotation is patched onto the CoreDNS Deployment's
	// pod template whenever coredns-custom's content changes (including the
	// very first time it is created), forcing an immediate rolling restart —
	// see restartInVClusterCoreDNSOnChange's doc comment for why this
	// replaces relying on CoreDNS's own "reload" plugin poll interval.
	coreDNSCustomChecksumAnnotation = "orbit.io/coredns-custom-checksum"
)

// ensureInVClusterCoreDNSSplitHorizon mirrors the host cluster's
// coredns-custom ConfigMap into this environment's vcluster, so the
// in-vcluster orbit-identity service's OIDC discovery against the
// externalized Dex issuer (appDexIssuerURL) resolves AppExternalURL's host
// the same way the host cluster and a laptop browser do.
//
// Without this, the vcluster's own embedded CoreDNS — a genuinely separate
// instance from the host's, not a delegate of it — forwards
// AppExternalURL's host straight upstream (its Corefile's "forward .
// /etc/resolv.conf") and never sees the host's split-horizon answer,
// landing on whatever the laptop's own resolver returns instead: confirmed
// live, 127.0.0.1 (the pod itself), producing "connection refused" from
// orbit-identity's OIDC discovery and a permanent CrashLoopBackOff. The
// vcluster chart's k8s-distro CoreDNS already mounts an optional
// "coredns-custom" ConfigMap for exactly this extension point (its Corefile
// imports /etc/coredns/custom/*.server); it just needs the ConfigMap to
// exist, with its zone header rewritten to the port the vcluster's CoreDNS
// actually listens on (rewriteCoreDNSCustomForVCluster). It then forces a
// deterministic CoreDNS rollout via restartInVClusterCoreDNSOnChange —
// confirmed live, letting CoreDNS's own "reload" plugin pick the change up
// on its own poll cycle instead stacks that poll interval on top of
// kubelet's ConfigMap-volume propagation delay, together taking long enough
// that orbit-identity's synchronous OIDC discovery crash-loops several times
// against a stale DNS answer before converging. The caller
// (reconcileInVClusterPlatform) gates on inVClusterCoreDNSRolloutComplete
// before proceeding, so nothing that depends on this zone gets installed
// until the rollout this triggers has actually finished.
func (r *DevEnvironmentReconciler) ensureInVClusterCoreDNSSplitHorizon(ctx context.Context, vcClient client.Client) error {
	hostCM := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace}, hostCM)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("get host coredns-custom configmap: %w", err)
	}
	data := rewriteCoreDNSCustomForVCluster(hostCM.Data)

	existing := &corev1.ConfigMap{}
	err = vcClient.Get(ctx, client.ObjectKey{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace},
			Data:       data,
		}
		if createErr := vcClient.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create in-vcluster coredns-custom configmap: %w", createErr)
		}
	case err != nil:
		return fmt.Errorf("get in-vcluster coredns-custom configmap: %w", err)
	default:
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = data
		if patchErr := vcClient.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch in-vcluster coredns-custom configmap: %w", patchErr)
		}
	}

	if restartErr := restartInVClusterCoreDNSOnChange(ctx, vcClient, data); restartErr != nil {
		return fmt.Errorf("restart in-vcluster coredns for custom zone change: %w", restartErr)
	}
	return nil
}

// restartInVClusterCoreDNSOnChange patches the vcluster's own CoreDNS
// Deployment with a checksum of coredns-custom's content, forcing an
// immediate rolling restart whenever that content changes (including the
// very first time the ConfigMap is created). CoreDNS's "reload" plugin would
// eventually pick the same change up on its own (default 30s poll), but only
// after kubelet's own ConfigMap-volume propagation delay — together long
// enough, confirmed live, to race orbit-identity's synchronous OIDC discovery
// at startup. A fresh pod instead mounts the current ConfigMap content
// directly, with no polling delay on either side. A missing CoreDNS
// Deployment is a no-op, not an error — on a brand-new environment this call
// can race the vcluster chart's own CoreDNS install by a few seconds, and
// inVClusterCoreDNSRolloutComplete's "not ready" result drives the next
// periodic reconcile to retry it.
func restartInVClusterCoreDNSOnChange(ctx context.Context, vcClient client.Client, data map[string]string) error {
	deploy := &appsv1.Deployment{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("get in-vcluster coredns deployment: %w", err)
	}

	checksum := coreDNSCustomChecksum(data)
	if deploy.Spec.Template.Annotations[coreDNSCustomChecksumAnnotation] == checksum {
		return nil
	}

	patch := client.MergeFrom(deploy.DeepCopy())
	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = map[string]string{}
	}
	deploy.Spec.Template.Annotations[coreDNSCustomChecksumAnnotation] = checksum
	if patchErr := vcClient.Patch(ctx, deploy, patch); patchErr != nil {
		return fmt.Errorf("patch in-vcluster coredns deployment for restart: %w", patchErr)
	}
	return nil
}

// coreDNSCustomChecksum hashes coredns-custom's data deterministically
// (sorted keys) so restartInVClusterCoreDNSOnChange detects real content
// changes regardless of Go's randomized map iteration order.
func coreDNSCustomChecksum(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(data[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// inVClusterCoreDNSRolloutComplete reports whether the vcluster's own
// CoreDNS Deployment has finished rolling out its current pod template spec —
// in particular, any restart restartInVClusterCoreDNSOnChange just
// triggered — rather than merely existing with old, still-Ready replicas
// serving a stale coredns-custom mount. A missing Deployment is reported as
// "not ready" rather than an error, the same posture as
// inVClusterPostgresRunning.
func inVClusterCoreDNSRolloutComplete(ctx context.Context, vcClient client.Client) (bool, error) {
	deploy := &appsv1.Deployment{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get in-vcluster coredns deployment: %w", err)
	}
	if deploy.Status.ObservedGeneration < deploy.Generation {
		return false, nil
	}
	wantReplicas := int32(1)
	if deploy.Spec.Replicas != nil {
		wantReplicas = *deploy.Spec.Replicas
	}
	return deploy.Status.UpdatedReplicas == wantReplicas && deploy.Status.ReadyReplicas == wantReplicas, nil
}

// rewriteCoreDNSCustomForVCluster rewrites every zone header's listening
// port from the host CoreDNS's own port (hostCoreDNSPort) to the vcluster
// CoreDNS's port (inVClusterCoreDNSPort) — see inVClusterCoreDNSPort's doc
// comment for why a verbatim copy silently never intercepts anything.
func rewriteCoreDNSCustomForVCluster(hostData map[string]string) map[string]string {
	from := fmt.Sprintf(":%d {", hostCoreDNSPort)
	to := fmt.Sprintf(":%d {", inVClusterCoreDNSPort)
	rewritten := make(map[string]string, len(hostData))
	for k, v := range hostData {
		rewritten[k] = strings.ReplaceAll(v, from, to)
	}
	return rewritten
}

// applyInVClusterOrbitPlatform creates or updates the OrbitPlatform CR inside
// the vcluster. spire.enabled is always false (option C3 — SPIRE cannot run
// inside a vcluster and identity is host-provided instead); everything else
// mirrors deploy/local/orbit-platform.yaml.
func (r *DevEnvironmentReconciler) applyInVClusterOrbitPlatform(ctx context.Context, vcClient client.Client, env *OrbitDevEnvironment, trustDomain string, meshJWT MeshJWTValidationSpec) error {
	// Every environment's charts serve the SHARED app hostname (the ADR's
	// path-based-app-ingress amendment): the host gateway forwards app
	// traffic with the original Host header intact, so the in-vcluster
	// orbit-gateway and the orbit chart's VirtualServices must match that
	// hostname, not the baked deploy/local frontendHost. The same value for
	// every environment — per-environment routing is the HOST gateway's
	// path/cookie concern (devenvironment_app_ingress.go), never the
	// environment's own bits (decision 4: identical image/chart everywhere).
	appHost, err := r.appHostname()
	if err != nil {
		return err
	}
	// gateway.tls.enabled: false overrides the baked platform-values.yaml
	// (which sets it true for the HOST's own real external-facing
	// orbit-gateway). Every dev environment's in-vcluster gateway must stay
	// plain-HTTP: the host gateway forwards app traffic to it over HTTP by
	// design (devEnvAppGatewayPort's doc comment — TLS terminates once, at
	// the SHARED gateway, never per environment). Left at the baked true
	// default, the in-vcluster gateway's port-80 listener 301-redirects
	// every request to its own HTTPS port — which the host gateway then
	// immediately re-requests over HTTP again, looping forever.
	gatewayValues := map[string]any{"frontendHost": appHost, "gateway": map[string]any{"tls": map[string]any{"enabled": false}}}
	spec := OrbitPlatformSpec{
		Version:           inVClusterOrbitPlatformVersion,
		TrustDomain:       trustDomain,
		SPIRE:             SPIRESpec{Enabled: false},
		Istio:             IstioSpec{Enabled: true},
		MeshJWTValidation: meshJWT,
		DevEnvironmentID:  env.Name,
		Gateway:           GatewaySpec{ServiceType: "ClusterIP"},
		ImageRegistry:     r.ImageRegistry,
		Secrets: PlatformSecretsSpec{
			PlatformSecretRef: SecretRef{Name: "orbit-secrets", Namespace: "orbit-system"},
		},
		Config:          r.inVClusterPlatformConfig(),
		UpgradeStrategy: UpgradeStrategySpec{Mode: "Explicit"},
		PlatformValues:  gatewayValues,
		// frontend.config.devEnvironmentId lets the in-vcluster frontend show
		// "Dev Env: {id}" and a way back to the host's own Orbit — the same
		// image runs everywhere (decision 4), so this is the one bit of
		// per-environment identity threaded into an otherwise identical chart
		// install, mirroring frontendHost above.
		OrbitValues: map[string]any{
			"frontendHost": appHost,
			"frontend":     map[string]any{"config": map[string]any{"devEnvironmentId": env.Name}},
		},
	}

	existing := &OrbitPlatform{}
	err = vcClient.Get(ctx, client.ObjectKey{Name: inVClusterOrbitPlatformName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &OrbitPlatform{
			ObjectMeta: metav1.ObjectMeta{Name: inVClusterOrbitPlatformName},
			Spec:       spec,
		}
		if createErr := vcClient.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create OrbitPlatform %q: %w", inVClusterOrbitPlatformName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get OrbitPlatform %q: %w", inVClusterOrbitPlatformName, err)
	default:
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Spec = spec
		if patchErr := vcClient.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch OrbitPlatform %q: %w", inVClusterOrbitPlatformName, patchErr)
		}
		return nil
	}
}

// inVClusterPlatformReady reports whether the in-vcluster OrbitPlatform CR
// (submitted by reconcileInVClusterPlatform) has itself reached
// status.phase == PhaseReady — i.e. the nested orbit-operator has finished
// the real release sequence (spire-crds → spire → istio-base → istiod →
// gateway → orbit-platform → orbit) inside the vcluster, not merely that the
// CR was applied. Confirmed live: without this check, OrbitDevEnvironment
// reported status.phase Ready ~60-90s before orbit-identity/orbit-frontend/the
// gateways were even scheduled, so the UI showed a live ingress link the
// environment could not yet serve.
//
// A missing vcluster API server or a not-yet-created OrbitPlatform CR is
// reported as "not ready" rather than an error — both are expected
// transiently right after apply and self-heal on the next reconcile, the
// same posture as the rest of this reconciler.
func (r *DevEnvironmentReconciler) inVClusterPlatformReady(ctx context.Context, env *OrbitDevEnvironment) (bool, error) {
	ns := devEnvNamespace(env)

	vcCfg, err := r.VClusterRestConfigFactory(ctx, ns)
	if err != nil {
		return false, fmt.Errorf("build vcluster rest config: %w", err)
	}
	vcClient, err := r.VClusterClientFactory(vcCfg)
	if err != nil {
		return false, fmt.Errorf("build vcluster client: %w", err)
	}

	platform := &OrbitPlatform{}
	if err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterOrbitPlatformName}, platform); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get in-vcluster OrbitPlatform %q: %w", inVClusterOrbitPlatformName, err)
	}
	return platform.Status.Phase == PhaseReady, nil
}

// applyInVClusterOrbitTenant creates or updates the seeded OrbitTenant CR
// inside the vcluster, mirroring deploy/local/tenants/example.yaml.
// issuerURL must be identical to the fixture Dex's own configured issuer
// (appDexIssuerURL, threaded through ensureFixtures by the caller) — the
// in-vcluster identity service's OIDC discovery rejects any mismatch between
// the two.
func (r *DevEnvironmentReconciler) applyInVClusterOrbitTenant(ctx context.Context, vcClient client.Client, issuerURL string) error {
	spec := OrbitTenantSpec{
		DisplayName: "Orbit Dev Environment",
		Active:      true,
		OIDC: TenantOIDCSpec{
			IssuerURL:    issuerURL,
			ClientID:     "orbit-local",
			Scopes:       []string{"openid", "profile", "email", "groups"},
			EmailDomains: []string{inVClusterDevUserEmailDomain},
		},
		DeleteNamespaceOnRemoval: false,
	}

	existing := &OrbitTenant{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterOrbitTenantName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &OrbitTenant{
			ObjectMeta: metav1.ObjectMeta{Name: inVClusterOrbitTenantName},
			Spec:       spec,
		}
		if createErr := vcClient.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create OrbitTenant %q: %w", inVClusterOrbitTenantName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get OrbitTenant %q: %w", inVClusterOrbitTenantName, err)
	default:
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Spec = spec
		if patchErr := vcClient.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch OrbitTenant %q: %w", inVClusterOrbitTenantName, patchErr)
		}
		return nil
	}
}
