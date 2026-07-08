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
	"errors"
	"fmt"

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
	if fixturesErr := ensureFixtures(ctx, vcClient, secretValues); fixturesErr != nil {
		return fmt.Errorf("ensure in-vcluster fixtures: %w", fixturesErr)
	}

	trustDomain, err := r.hostTrustDomain(ctx)
	if err != nil {
		return fmt.Errorf("determine host trust domain: %w", err)
	}

	if err := r.applyInVClusterOrbitPlatform(ctx, vcClient, trustDomain); err != nil {
		return fmt.Errorf("apply in-vcluster OrbitPlatform: %w", err)
	}
	if err := r.applyInVClusterOrbitTenant(ctx, vcClient); err != nil {
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

// inVClusterDexIssuerURL points the seeded OrbitTenant's OIDC issuer at the
// Dex fixture ensureFixtures deploys inside the same vcluster.
func (r *DevEnvironmentReconciler) inVClusterDexIssuerURL() string {
	return inVClusterDexIssuerURLValue()
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

// applyInVClusterOrbitPlatform creates or updates the OrbitPlatform CR inside
// the vcluster. spire.enabled is always false (option C3 — SPIRE cannot run
// inside a vcluster and identity is host-provided instead); everything else
// mirrors deploy/local/orbit-platform.yaml.
func (r *DevEnvironmentReconciler) applyInVClusterOrbitPlatform(ctx context.Context, vcClient client.Client, trustDomain string) error {
	spec := OrbitPlatformSpec{
		Version:       inVClusterOrbitPlatformVersion,
		TrustDomain:   trustDomain,
		SPIRE:         SPIRESpec{Enabled: false},
		Istio:         IstioSpec{Enabled: true},
		Gateway:       GatewaySpec{ServiceType: "ClusterIP"},
		ImageRegistry: r.ImageRegistry,
		Secrets: PlatformSecretsSpec{
			PlatformSecretRef: SecretRef{Name: "orbit-secrets", Namespace: "orbit-system"},
		},
		Config:          r.inVClusterPlatformConfig(),
		UpgradeStrategy: UpgradeStrategySpec{Mode: "Explicit"},
	}

	existing := &OrbitPlatform{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterOrbitPlatformName}, existing)
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

// applyInVClusterOrbitTenant creates or updates the seeded OrbitTenant CR
// inside the vcluster, mirroring deploy/local/tenants/example.yaml.
func (r *DevEnvironmentReconciler) applyInVClusterOrbitTenant(ctx context.Context, vcClient client.Client) error {
	spec := OrbitTenantSpec{
		DisplayName: "Orbit Dev Environment",
		Active:      true,
		OIDC: TenantOIDCSpec{
			IssuerURL: r.inVClusterDexIssuerURL(),
			ClientID:  "orbit-local",
			Scopes:    []string{"openid", "profile", "email", "groups"},
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
