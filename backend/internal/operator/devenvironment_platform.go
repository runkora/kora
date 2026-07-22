// In-vcluster Orbit platform install for OrbitDevEnvironment.
//
// Once an environment's vcluster is Ready, this file drives the second half
// of "each environment is a self-contained Orbit install": installing the
// orbit-operator chart inside the vcluster's own API server, then applying an
// OrbitPlatform CR (spire.enabled: false — identity is host-provided) and a
// seeded OrbitTenant CR so the in-vcluster operator takes over the real
// release sequence (spire-crds → spire → istio-base → istiod → gateway →
// orbit-platform → orbit), exactly as it would on any other cluster.
package operator

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	// "vc-config-{release}", which holds the rendered vcluster.yaml values.
	vclusterKubeconfigSecretPrefix = "vc-"
	vclusterKubeconfigSecretKey    = "config"

	// vclusterAPIPort is the port the vcluster chart's own Service exposes
	// the virtual API server on (the Service named after the release
	// listens on 443).
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
	// inside every environment. Matches deploy/local/orbit-platform.yaml.
	inVClusterOrbitPlatformVersion = "0.1.0"
)

// inVClusterServiceImageServices lists every orbit-chart top-level service
// key that carries its own image.tag — these are the images built from this
// same repo at the same ref, so every one of them must resolve at
// status.resolvedRef, never the chart's own baked default (which would pull
// whatever :latest/:appVersion happens to be on the registry, not
// necessarily the same commit the rest of the environment installed at).
// opaGateway is deliberately excluded — it is a third-party image (OPA),
// never built from this repo.
var inVClusterServiceImageServices = []string{
	"identity", "inferenceGateway", "orbitMcp", "toolGateway", "frontend",
	"agents", "policyService", "policyGateway", "egressGateway",
}

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
// background kubectl port-forward, which the chart's generated kubeconfig
// assumes via its default https://localhost:8443 host — the operator has no
// such port-forward running.
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
// loop. Called from reconcileInVClusterPlatformAndReadiness once ensureVCluster
// has succeeded — the vcluster's own API server (and therefore its
// kubeconfig Secret) only exists after that. resolvedRef is
// env.Status.ResolvedRef (resolveRef, devenvironment_ref.go) — the
// immutable tag every image installed inside this vcluster must resolve
// at.
func (r *DevEnvironmentReconciler) reconcileInVClusterPlatform(ctx context.Context, env *OrbitDevEnvironment, resolvedRef string) error {
	ns := devEnvNamespace(env)

	vcCfg, err := r.VClusterRestConfigFactory(ctx, ns)
	if err != nil {
		return fmt.Errorf("build vcluster rest config: %w", err)
	}

	vcClient, err := r.VClusterClientFactory(vcCfg)
	if err != nil {
		return fmt.Errorf("build vcluster client: %w", err)
	}

	// Must exist before any kubeconfig minted against this vcluster with
	// Subject Organization "orbit:devenv-admin" (cmd/identity's
	// devenvironments service — WP-10) could actually authorize anything;
	// has no dependency on anything else installed below, so doing it first
	// costs nothing and closes that window immediately.
	if err = r.ensureInVClusterDevEnvAdminBinding(ctx, vcClient); err != nil {
		return fmt.Errorf("ensure in-vcluster devenv-admin binding: %w", err)
	}

	helm := r.InVClusterHelmFactory(inVClusterOperatorNamespace, vcCfg)
	if _, installErr := helm.InstallOrUpgrade(ctx,
		inVClusterOperatorReleaseName, r.inVClusterOperatorChartPath(), "", "",
		r.inVClusterOperatorValues(resolvedRef),
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
	if fixturesErr := ensureFixtures(ctx, vcClient, secretValues, r.appAuthCallbackURL(env), externalDexIssuer); fixturesErr != nil {
		return fmt.Errorf("ensure in-vcluster fixtures: %w", fixturesErr)
	}
	if migErr := r.ensureInVClusterMigrations(ctx, vcClient, resolvedRef); migErr != nil {
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

	if err := r.applyInVClusterOrbitPlatform(ctx, vcClient, env, trustDomain, meshJWT, resolvedRef); err != nil {
		return fmt.Errorf("apply in-vcluster OrbitPlatform: %w", err)
	}
	if err := r.applyInVClusterOrbitTenant(ctx, vcClient, externalDexIssuer); err != nil {
		return fmt.Errorf("apply in-vcluster OrbitTenant: %w", err)
	}
	return nil
}

// inVClusterDevEnvAdminBindingName is the fixed ClusterRoleBinding name
// ensureInVClusterDevEnvAdminBinding creates or verifies — one per
// environment's own vcluster, so a fixed name is sufficient.
const inVClusterDevEnvAdminBindingName = "orbit-devenv-admin"

// ensureInVClusterDevEnvAdminBinding creates the ClusterRoleBinding that
// makes the "orbit:devenv-admin" group resolve to cluster-admin inside this
// environment's own vcluster. cmd/identity's devenvironments service mints
// each developer's brokered kubeconfig client certificate with this group
// as its Subject Organization (decision E2) — replacing the prototype's
// O=system:masters, which bypasses every RBAC check outright and cannot be
// scoped, audited by name, or revoked independently of the CA itself. The
// developer remains cluster-admin inside their own environment either way;
// this makes that grant a nameable, revocable RBAC object instead.
func (r *DevEnvironmentReconciler) ensureInVClusterDevEnvAdminBinding(ctx context.Context, vcClient client.Client) error {
	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterDevEnvAdminBindingName},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "orbit:devenv-admin"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}

	existing := &rbacv1.ClusterRoleBinding{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterDevEnvAdminBindingName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := vcClient.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create ClusterRoleBinding %q: %w", inVClusterDevEnvAdminBindingName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get ClusterRoleBinding %q: %w", inVClusterDevEnvAdminBindingName, err)
	}

	// Subjects/RoleRef are immutable on an existing ClusterRoleBinding — a
	// mismatch here can only mean a stale object from an older code version;
	// delete and recreate rather than erroring forever on every reconcile.
	if subjectsAndRoleRefMatch(existing, desired) {
		return nil
	}
	if err := vcClient.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete stale ClusterRoleBinding %q: %w", inVClusterDevEnvAdminBindingName, err)
	}
	if err := vcClient.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("recreate ClusterRoleBinding %q: %w", inVClusterDevEnvAdminBindingName, err)
	}
	return nil
}

func subjectsAndRoleRefMatch(a, b *rbacv1.ClusterRoleBinding) bool {
	if a.RoleRef != b.RoleRef {
		return false
	}
	if len(a.Subjects) != len(b.Subjects) {
		return false
	}
	for i := range a.Subjects {
		if a.Subjects[i] != b.Subjects[i] {
			return false
		}
	}
	return true
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
// a single replica with nothing else to contend with. The image is pinned to
// resolvedRef with IfNotPresent — never :latest+Always — because resolvedRef
// (typically SelfImageTag, devenvironment_ref.go — the HOST operator's own
// tag) names one specific, immutable build; there is nothing for a node to
// go stale against.
func (r *DevEnvironmentReconciler) inVClusterOperatorValues(resolvedRef string) map[string]any {
	return map[string]any{
		"logLevel": "debug",
		"image": map[string]any{
			"repository": r.inVClusterOperatorImageRepo(),
			"tag":        resolvedRef,
			"pullPolicy": "IfNotPresent",
		},
		"leaderElection": false,
	}
}

// inVClusterOperatorImageRepo is the registry-qualified repository (no tag)
// for the operator's own image, shared by inVClusterOperatorValues and
// inVClusterOperatorImage (devenvironment_migrations.go) so both always
// agree on where the image comes from.
func (r *DevEnvironmentReconciler) inVClusterOperatorImageRepo() string {
	if r.ImageRegistry != "" {
		return r.ImageRegistry + "/orbit-operator"
	}
	return "orbit-operator"
}

// inVClusterPlatformConfig points the OrbitPlatform CR's db/redis config at
// the fixtures ensureFixtures deploys inside the same vcluster — environments
// never share the host's Postgres/Redis.
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
// value (not a per-environment one): identity is host-provided, so
// in-vcluster Istio's mTLS trust config has to agree with the trust domain
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
// the vcluster. spire.enabled is always false (SPIRE cannot run inside a
// vcluster and identity is host-provided instead); everything else mirrors
// deploy/local/orbit-platform.yaml. resolvedRef pins every orbit-chart
// service image (inVClusterServiceImageServices) to the same tag the
// in-vcluster orbit-operator itself was just installed at.
func (r *DevEnvironmentReconciler) applyInVClusterOrbitPlatform(ctx context.Context, vcClient client.Client, env *OrbitDevEnvironment, trustDomain string, meshJWT MeshJWTValidationSpec, resolvedRef string) error {
	// Every environment's charts serve the SHARED app hostname (the
	// path-based-app-ingress design): the host gateway forwards app traffic
	// with the original Host header intact, so the in-vcluster orbit-gateway
	// and the orbit chart's VirtualServices must match that hostname, not
	// the baked deploy/local frontendHost. The same value for every
	// environment — per-environment routing is the HOST gateway's
	// path/cookie concern (devenvironment_app_ingress.go), never the
	// environment's own bits (every environment installs the same image).
	appHost, err := r.appHostname()
	if err != nil {
		return err
	}
	// gateway.tls.enabled: false overrides the baked platform-values.yaml
	// (which sets it true for the HOST's own real external-facing
	// orbit-gateway). Every dev environment's in-vcluster gateway must stay
	// plain-HTTP: the host gateway forwards app traffic to it over HTTP by
	// design (devEnvAPIGatewayPort's doc comment — TLS terminates once, at
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
		// image runs everywhere, so this is the one bit of per-environment
		// identity threaded into an otherwise identical chart install,
		// mirroring frontendHost above. identity.config.devEnvironmentId is
		// the analogous passthrough for the nested-provisioning guard
		// (charts/orbit/templates/identity/deployment.yaml's
		// ORBIT_DEV_ENVIRONMENT_ID, consumed by cmd/identity's
		// devenvironments feature). Every orbit-chart service image is
		// pinned to resolvedRef so the whole environment installs at one
		// consistent commit.
		OrbitValues: mergeMaps(inVClusterServiceImageTags(resolvedRef), map[string]any{
			"frontendHost": appHost,
			"frontend":     map[string]any{"config": map[string]any{"devEnvironmentId": env.Name}},
			"identity":     map[string]any{"config": map[string]any{"devEnvironmentId": env.Name}},
		}),
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

// inVClusterServiceImageTags pins every orbit-chart service in
// inVClusterServiceImageServices to resolvedRef, as a raw OrbitValues map
// (merged, not templated — no chart change needed: PlatformValues/OrbitValues
// are already merged verbatim into the rendered chart values).
func inVClusterServiceImageTags(resolvedRef string) map[string]any {
	values := make(map[string]any, len(inVClusterServiceImageServices))
	for _, svc := range inVClusterServiceImageServices {
		values[svc] = map[string]any{"image": map[string]any{"tag": resolvedRef}}
	}
	return values
}

// inVClusterPlatformReady reports whether the in-vcluster OrbitPlatform CR
// (submitted by reconcileInVClusterPlatform) has itself reached
// status.phase == PhaseReady — i.e. the nested orbit-operator has finished
// the real release sequence (spire-crds → spire → istio-base → istiod →
// gateway → orbit-platform → orbit) inside the vcluster, not merely that the
// CR was applied. Without this check, OrbitDevEnvironment would report
// status.phase Ready ~60-90s before orbit-identity/orbit-frontend/the
// gateways were even scheduled, so the UI would show a live ingress link the
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
