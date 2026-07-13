package operator

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeVClusterKubeconfig builds a minimal, well-formed kubeconfig YAML the
// same shape the vcluster chart's "vc-{release}" Secret holds: a cluster with
// a placeholder localhost server (what `vcluster connect`'s port-forward
// would use) and CA data, so VClusterRestConfig has something real to parse
// and then override.
func fakeVClusterKubeconfig(t *testing.T) []byte {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["vcluster"] = &clientcmdapi.Cluster{
		Server:                   "https://localhost:8443",
		CertificateAuthorityData: []byte("fake-ca-data"),
	}
	cfg.AuthInfos["vcluster"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: []byte("fake-client-cert"),
		ClientKeyData:         []byte("fake-client-key"),
	}
	cfg.Contexts["vcluster"] = &clientcmdapi.Context{Cluster: "vcluster", AuthInfo: "vcluster"}
	cfg.CurrentContext = "vcluster"

	data, err := clientcmd.Write(*cfg)
	if err != nil {
		t.Fatalf("write fake kubeconfig: %v", err)
	}
	return data
}

func TestVClusterRestConfig_RewritesHostToInClusterServiceDNS(t *testing.T) {
	scheme := NewScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-" + vclusterReleaseName, Namespace: "orbit-devenv-alice"},
		Data:       map[string][]byte{vclusterKubeconfigSecretKey: fakeVClusterKubeconfig(t)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	cfg, err := VClusterRestConfig(context.Background(), c, "orbit-devenv-alice")
	if err != nil {
		t.Fatalf("VClusterRestConfig: %v", err)
	}

	// Confirmed empirically against a live vcluster (see devenvironment_platform.go
	// doc comment): the chart's own server cert carries "{release}.{namespace}" as
	// a SAN, so the host must be exactly this two-label short form — no ".svc" or
	// ".svc.cluster.local" suffix, which would NOT match the certificate.
	want := "https://vcluster.orbit-devenv-alice:443"
	if cfg.Host != want {
		t.Errorf("Host = %q, want %q", cfg.Host, want)
	}
	if string(cfg.CAData) != "fake-ca-data" {
		t.Errorf("CAData not carried through from kubeconfig, got %q", cfg.CAData)
	}
	if string(cfg.CertData) != "fake-client-cert" {
		t.Errorf("CertData not carried through from kubeconfig, got %q", cfg.CertData)
	}
}

func TestVClusterRestConfig_MissingSecretErrors(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	if _, err := VClusterRestConfig(context.Background(), c, "orbit-devenv-missing"); err == nil {
		t.Fatal("expected error when the vcluster kubeconfig secret does not exist")
	}
}

func TestReconcile_InstallsInVClusterPlatformOnceReady(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("bob-env", nil)
	r, _, vcClient, inVClusterHelm := newDevEnvTestReconcilerFull(t, clock, env)

	reconcileOnce(t, r, env.Name)

	if inVClusterHelm.InstallOrUpgradeCalls == 0 {
		t.Fatal("expected the in-vcluster orbit-operator chart to be installed")
	}

	platform := &OrbitPlatform{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterOrbitPlatformName}, platform); err != nil {
		t.Fatalf("in-vcluster OrbitPlatform not applied: %v", err)
	}
	if platform.Spec.SPIRE.Enabled {
		t.Error("in-vcluster OrbitPlatform must have spire.enabled=false (option C3 — identity is host-provided)")
	}
	if !platform.Spec.Istio.Enabled {
		t.Error("in-vcluster OrbitPlatform must have istio.enabled=true")
	}
	if platform.Spec.TrustDomain != "orbit.test" {
		t.Errorf("TrustDomain = %q, want the HOST OrbitPlatform's trust domain %q", platform.Spec.TrustDomain, "orbit.test")
	}
	if platform.Spec.Config.DB.Host == "" || platform.Spec.Config.Redis.Host == "" {
		t.Error("in-vcluster OrbitPlatform must point at the fixture Postgres/Redis hosts")
	}
	// The in-vcluster istiod must be able to validate HOST-signed workload
	// tokens: the host issuer/JWKS ride the CR spec (devenvironment_jwks.go).
	if !platform.Spec.MeshJWTValidation.Enabled {
		t.Error("in-vcluster OrbitPlatform must have meshJWTValidation.enabled=true")
	}
	if platform.Spec.MeshJWTValidation.Issuer == "" || platform.Spec.MeshJWTValidation.JWKS == "" {
		t.Errorf("meshJWTValidation must carry the host issuer and JWKS, got issuer=%q jwks=%q",
			platform.Spec.MeshJWTValidation.Issuer, platform.Spec.MeshJWTValidation.JWKS)
	}

	// The in-vcluster frontend needs its own environment's ID (not baked into
	// the shared image) to show "Dev Env: {id}" and a way back to the host's
	// own Orbit — threaded as a chart values override, same mechanism as
	// frontendHost.
	orbitValues, ok := platform.Spec.OrbitValues["frontend"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"frontend\"] missing or wrong type: %#v", platform.Spec.OrbitValues["frontend"])
	}
	frontendConfig, ok := orbitValues["config"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"frontend\"][\"config\"] missing or wrong type: %#v", orbitValues["config"])
	}
	if frontendConfig["devEnvironmentId"] != env.Name {
		t.Errorf("OrbitValues frontend.config.devEnvironmentId = %v, want %q", frontendConfig["devEnvironmentId"], env.Name)
	}

	tenant := &OrbitTenant{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterOrbitTenantName}, tenant); err != nil {
		t.Fatalf("in-vcluster OrbitTenant not applied: %v", err)
	}
	if tenant.Spec.OIDC.IssuerURL == "" {
		t.Error("seeded OrbitTenant must have an OIDC issuer URL (the fixture Dex)")
	}

	secret := &corev1.Secret{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: "orbit-secrets", Namespace: "orbit-system"}, secret); err != nil {
		t.Fatalf("orbit-secrets not created inside the vcluster: %v", err)
	}
	if len(secret.Data["ORBIT_TOKEN_SIGNING_KEY"]) == 0 {
		t.Error("orbit-secrets missing ORBIT_TOKEN_SIGNING_KEY")
	}

	// The fixture Postgres must be migrated or orbit-identity cannot boot —
	// a goose Job using the operator's own image (devenvironment_migrations.go).
	// The fake client runs no Deployment controller, so the fixture Postgres
	// Deployment this first reconcile created never gains a Ready replica on
	// its own — the migration Job must therefore still be absent.
	job := &batchv1.Job{}
	jobErr := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, job)
	if jobErr == nil {
		t.Fatal("migration Job created before the fixture Postgres reported a Ready replica")
	}

	postgres := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterPostgresName, Namespace: inVClusterFixturesNamespace}, postgres); err != nil {
		t.Fatalf("fixture postgres deployment not created: %v", err)
	}
	postgres.Status.ReadyReplicas = 1
	if err := vcClient.Status().Update(context.Background(), postgres); err != nil {
		t.Fatalf("mark fixture postgres ready: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, job); err != nil {
		t.Fatalf("migration Job not created inside the vcluster once postgres was ready: %v", err)
	}
	if img := job.Spec.Template.Spec.Containers[0].Image; img != "orbit-registry:5001/orbit-operator:latest" {
		t.Errorf("migration Job image = %q, want the operator's own registry-prefixed image", img)
	}
	if inject := job.Spec.Template.Annotations["sidecar.istio.io/inject"]; inject != "false" {
		t.Error("migration Job pod must opt out of sidecar injection or it never completes")
	}

	got := getEnv(t, r, env.Name)
	found := false
	for _, c := range got.Status.Conditions {
		if c.Type == ConditionDevEnvPlatformApplied && c.Status == metav1.ConditionTrue {
			found = true
		}
	}
	if !found {
		t.Error("expected InVClusterPlatformApplied condition to be True")
	}
}

func TestReconcile_NoHostOrbitPlatformFailsInVClusterInstall(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("no-host-platform", nil)
	r, _, _, _ := newDevEnvTestReconcilerFull(t, clock, env)

	// Remove the host OrbitPlatform newDevEnvTestReconcilerFull seeds by
	// default — hostTrustDomain has nothing to read from.
	hostPlatform := &OrbitPlatform{ObjectMeta: metav1.ObjectMeta{Name: "orbit"}}
	if err := r.Delete(context.Background(), hostPlatform); err != nil {
		t.Fatalf("delete seeded host OrbitPlatform: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseFailed {
		t.Errorf("phase = %q, want %q (no host trust domain to derive)", got.Status.Phase, DevEnvPhaseFailed)
	}
}

func TestEnsureInVClusterSecrets_GeneratesOnceAndIsStableAcrossReconciles(t *testing.T) {
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()

	first, err := ensureInVClusterSecrets(context.Background(), vcClient)
	if err != nil {
		t.Fatalf("ensureInVClusterSecrets (create): %v", err)
	}
	if first.DBPassword == "" || first.RedisPassword == "" || len(first.SigningKeyPEM) == 0 {
		t.Fatal("expected generated credentials to be non-empty")
	}

	second, err := ensureInVClusterSecrets(context.Background(), vcClient)
	if err != nil {
		t.Fatalf("ensureInVClusterSecrets (re-read): %v", err)
	}
	if second.DBPassword != first.DBPassword || second.RedisPassword != first.RedisPassword {
		t.Error("credentials must not be regenerated once created — Postgres/Redis were deployed with the first set")
	}
}

func TestEnsureFixtures_DeploysPostgresRedisDexGlauth(t *testing.T) {
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	secrets, err := ensureInVClusterSecrets(context.Background(), vcClient)
	if err != nil {
		t.Fatalf("ensureInVClusterSecrets: %v", err)
	}

	callback := "http://devenv.orbit.test:8880/auth/callback"
	issuer := "http://devenv.orbit.test:8880/alice-env/dex"
	if err := ensureFixtures(context.Background(), vcClient, secrets, callback, issuer); err != nil {
		t.Fatalf("ensureFixtures: %v", err)
	}
	// Re-apply must be idempotent.
	if err := ensureFixtures(context.Background(), vcClient, secrets, callback, issuer); err != nil {
		t.Fatalf("ensureFixtures (re-apply): %v", err)
	}

	for _, name := range []string{inVClusterPostgresName, inVClusterRedisName, inVClusterDexName, inVClusterGlauthName} {
		svc := &corev1.Service{}
		if err := vcClient.Get(context.Background(), client.ObjectKey{Name: name, Namespace: inVClusterFixturesNamespace}, svc); err != nil {
			t.Errorf("service %q not created: %v", name, err)
		}
	}

	// glauth must carry a "dev" user in the platform-admin group — without
	// it, DefaultRoleScopes (policyservice/bundle/compiler.go) resolves zero
	// scopes for the devenvironment's bootstrap user and the frontend hides
	// every scope-gated nav item (Triggers, Policy, most of Model Gateway).
	glauthCM := &corev1.ConfigMap{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterGlauthName + "-config", Namespace: inVClusterFixturesNamespace}, glauthCM); err != nil {
		t.Fatalf("glauth configmap not created: %v", err)
	}
	if !strings.Contains(glauthCM.Data[inVClusterGlauthConfigFile], `mail = "dev@orbit.local.clearroute.io"`) {
		t.Errorf("glauth config missing dev user:\n%s", glauthCM.Data[inVClusterGlauthConfigFile])
	}
	if !strings.Contains(glauthCM.Data[inVClusterGlauthConfigFile], `name = "platform-admin"`) {
		t.Errorf("glauth config missing platform-admin group:\n%s", glauthCM.Data[inVClusterGlauthConfigFile])
	}

	// The Dex client must register the shared-host browser callback (the
	// path-based app-ingress amendment) alongside the relative device-flow
	// callback.
	cm := &corev1.ConfigMap{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterDexName + "-config", Namespace: inVClusterFixturesNamespace}, cm); err != nil {
		t.Fatalf("dex configmap not created: %v", err)
	}
	if !strings.Contains(cm.Data[inVClusterDexConfigFile], callback) {
		t.Errorf("dex config missing external callback %q:\n%s", callback, cm.Data[inVClusterDexConfigFile])
	}
	if !strings.Contains(cm.Data[inVClusterDexConfigFile], "issuer: "+issuer) {
		t.Errorf("dex config issuer must be the external, browser-reachable URL %q:\n%s", issuer, cm.Data[inVClusterDexConfigFile])
	}
	// Dex must authenticate against glauth (LDAP), not its own passwordDB —
	// only the LDAP connector's "groups" claim lets DefaultRoleScopes resolve
	// any scopes at all for the devenvironment's bootstrap user.
	if !strings.Contains(cm.Data[inVClusterDexConfigFile], "type: ldap") {
		t.Errorf("dex config must configure the LDAP connector:\n%s", cm.Data[inVClusterDexConfigFile])
	}
	if strings.Contains(cm.Data[inVClusterDexConfigFile], "enablePasswordDB") {
		t.Errorf("dex config must not fall back to passwordDB — it cannot return group membership:\n%s", cm.Data[inVClusterDexConfigFile])
	}
}

func TestEnsureInVClusterCoreDNSSplitHorizon_CopiesAndRewritesPort(t *testing.T) {
	hostCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace},
		Data: map[string]string{
			"orbit.local.clearroute.io.server": "orbit.local.clearroute.io:53 {\n    hosts {\n      127.0.0.1 orbit.local.clearroute.io\n      fallthrough\n    }\n    forward . /etc/resolv.conf\n}\n",
		},
	}
	r := &DevEnvironmentReconciler{Client: fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(hostCM).Build()}
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()

	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon: %v", err)
	}

	got := &corev1.ConfigMap{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace}, got); err != nil {
		t.Fatalf("in-vcluster coredns-custom configmap not created: %v", err)
	}
	// The vcluster's own k8s-distro CoreDNS listens on :1053 (its "kube-dns"
	// Service maps the standard port 53 to it) — a copied zone header that
	// kept the host's ":53" would bind a port nothing ever queries and never
	// actually intercept anything (confirmed live).
	server := got.Data["orbit.local.clearroute.io.server"]
	if !strings.Contains(server, "orbit.local.clearroute.io:1053 {") {
		t.Errorf("zone header not rewritten to the vcluster CoreDNS port:\n%s", server)
	}
	if strings.Contains(server, ":53 {") {
		t.Errorf("zone header still has the host's port 53:\n%s", server)
	}

	// Re-run must be idempotent and update in place (e.g. after the host's
	// own DOCKER_BRIDGE_IP changes on a cluster recreate).
	hostCM.Data["orbit.local.clearroute.io.server"] = strings.ReplaceAll(server, "127.0.0.1", "172.19.0.1")
	if err := r.Update(context.Background(), hostCM); err != nil {
		t.Fatalf("update host configmap: %v", err)
	}
	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon (re-run): %v", err)
	}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace}, got); err != nil {
		t.Fatalf("in-vcluster coredns-custom configmap missing after re-run: %v", err)
	}
	if !strings.Contains(got.Data["orbit.local.clearroute.io.server"], "172.19.0.1") {
		t.Errorf("in-vcluster configmap not updated to the host's new data:\n%s", got.Data["orbit.local.clearroute.io.server"])
	}
}

func TestEnsureInVClusterCoreDNSSplitHorizon_NoHostConfigMapIsNoop(t *testing.T) {
	r := &DevEnvironmentReconciler{Client: fake.NewClientBuilder().WithScheme(NewScheme()).Build()}
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()

	// Real infrastructure has no coredns-custom ConfigMap at all (a single
	// real DNS zone needs no split horizon) — this must be a no-op, not an
	// error.
	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon: %v", err)
	}

	got := &corev1.ConfigMap{}
	err := vcClient.Get(context.Background(), client.ObjectKey{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace}, got)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected no in-vcluster coredns-custom configmap, got err=%v", err)
	}
}

// TestEnsureInVClusterCoreDNSSplitHorizon_ForcesCoreDNSRestartOnContentChange
// is a regression test for a live CrashLoopBackOff: orbit-identity's
// synchronous, non-retrying OIDC discovery raced CoreDNS's own "reload"
// plugin picking up a coredns-custom content change on its ~30s poll cycle
// (stacked on kubelet's own ConfigMap-volume propagation delay), resolving
// the split-horizon hostname to the wrong answer for long enough to
// crash-loop several times. ensureInVClusterCoreDNSSplitHorizon must instead
// force a deterministic CoreDNS rollout via a checksum annotation the moment
// the content actually changes.
func TestEnsureInVClusterCoreDNSSplitHorizon_ForcesCoreDNSRestartOnContentChange(t *testing.T) {
	hostCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace},
		Data: map[string]string{
			"orbit.local.clearroute.io.server": "orbit.local.clearroute.io:53 {\n    hosts {\n      127.0.0.1 orbit.local.clearroute.io\n      fallthrough\n    }\n    forward . /etc/resolv.conf\n}\n",
		},
	}
	r := &DevEnvironmentReconciler{Client: fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(hostCM).Build()}
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(fakeReadyInVClusterCoreDNSDeployment()).Build()

	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon: %v", err)
	}
	deploy := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy); err != nil {
		t.Fatalf("get in-vcluster coredns deployment: %v", err)
	}
	firstChecksum := deploy.Spec.Template.Annotations[coreDNSCustomChecksumAnnotation]
	if firstChecksum == "" {
		t.Fatal("expected ensureInVClusterCoreDNSSplitHorizon to patch a checksum annotation on the CoreDNS deployment")
	}

	// Re-running with unchanged content must not touch the annotation.
	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon (re-run, unchanged): %v", err)
	}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy); err != nil {
		t.Fatalf("get in-vcluster coredns deployment: %v", err)
	}
	if got := deploy.Spec.Template.Annotations[coreDNSCustomChecksumAnnotation]; got != firstChecksum {
		t.Errorf("checksum annotation changed on a no-op re-run: got %q, want unchanged %q", got, firstChecksum)
	}

	// A real content change (e.g. the host's DOCKER_BRIDGE_IP changing on a
	// cluster recreate) must force a new checksum — i.e. a new rollout.
	hostCM.Data["orbit.local.clearroute.io.server"] = strings.ReplaceAll(
		hostCM.Data["orbit.local.clearroute.io.server"], "127.0.0.1", "172.19.0.1")
	if err := r.Update(context.Background(), hostCM); err != nil {
		t.Fatalf("update host configmap: %v", err)
	}
	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon (re-run, changed): %v", err)
	}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy); err != nil {
		t.Fatalf("get in-vcluster coredns deployment: %v", err)
	}
	if got := deploy.Spec.Template.Annotations[coreDNSCustomChecksumAnnotation]; got == firstChecksum {
		t.Error("checksum annotation did not change after coredns-custom content changed")
	}
}

func TestEnsureInVClusterCoreDNSSplitHorizon_MissingCoreDNSDeploymentIsNoop(t *testing.T) {
	hostCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: coreDNSCustomConfigMapName, Namespace: coreDNSNamespace},
		Data:       map[string]string{"orbit.local.clearroute.io.server": "orbit.local.clearroute.io:53 {\n}\n"},
	}
	r := &DevEnvironmentReconciler{Client: fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(hostCM).Build()}
	// No CoreDNS Deployment seeded — models the brief window on a brand-new
	// environment where the vcluster chart's own CoreDNS hasn't been created
	// yet.
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()

	if err := r.ensureInVClusterCoreDNSSplitHorizon(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterCoreDNSSplitHorizon: %v", err)
	}
}

func TestInVClusterCoreDNSRolloutComplete(t *testing.T) {
	tests := []struct {
		name    string
		deploy  *appsv1.Deployment
		want    bool
		wantErr bool
	}{
		{
			name: "missing deployment is not ready, not an error",
			want: false,
		},
		{
			name: "ready and fully rolled out",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace, Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1, ReadyReplicas: 1},
			},
			want: true,
		},
		{
			name: "status not yet observed the latest spec generation",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace, Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, ReadyReplicas: 1},
			},
			want: false,
		},
		{
			name: "rollout in progress — old replica still Ready, new one not yet Updated",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace, Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 0, ReadyReplicas: 1},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(NewScheme())
			if tt.deploy != nil {
				builder = builder.WithObjects(tt.deploy)
			}
			got, err := inVClusterCoreDNSRolloutComplete(context.Background(), builder.Build())
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ready = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestReconcileInVClusterPlatform_WaitsForCoreDNSReadyBeforeInstalling is the
// end-to-end regression test for the CrashLoopBackOff: reconcileInVClusterPlatform
// must not install the nested orbit-operator chart (and therefore must not let it
// go on to schedule orbit-identity) while the vcluster's own CoreDNS Deployment
// has not yet finished rolling out — confirmed live to otherwise let
// orbit-identity's synchronous OIDC discovery race a stale DNS answer.
func TestReconcileInVClusterPlatform_WaitsForCoreDNSReadyBeforeInstalling(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("coredns-not-ready-env", nil)
	r, _, vcClient, inVClusterHelm := newDevEnvTestReconcilerFull(t, clock, env)

	// newDevEnvTestReconcilerFull seeds a fully-Ready CoreDNS deployment;
	// simulate the moment right after a rollout was triggered but before it
	// has completed.
	deploy := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy); err != nil {
		t.Fatalf("get seeded coredns deployment: %v", err)
	}
	deploy.Status.UpdatedReplicas = 0
	if err := vcClient.Status().Update(context.Background(), deploy); err != nil {
		t.Fatalf("simulate in-progress coredns rollout: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	if inVClusterHelm.InstallOrUpgradeCalls != 0 {
		t.Fatal("expected the in-vcluster orbit-operator chart NOT to be installed while CoreDNS is still rolling out")
	}
	// Not asserting on env.Status.Phase here: newDevEnvTestReconcilerFull
	// pre-seeds the in-vcluster OrbitPlatform CR as already Ready (see its
	// doc comment), so phase reflects that CR's own status, independent of
	// whether reconcileInVClusterPlatform's CoreDNS gate let anything through
	// this reconcile. TestReconcile_DoesNotReportReadyUntilInVClusterPlatformReady
	// covers the phase/CR-readiness relationship with its own vcClient.

	// CoreDNS finishes rolling out — the next reconcile must proceed.
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterCoreDNSDeploymentName, Namespace: coreDNSNamespace}, deploy); err != nil {
		t.Fatalf("get coredns deployment: %v", err)
	}
	deploy.Status.UpdatedReplicas = 1
	if err := vcClient.Status().Update(context.Background(), deploy); err != nil {
		t.Fatalf("simulate coredns rollout complete: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	if inVClusterHelm.InstallOrUpgradeCalls == 0 {
		t.Fatal("expected the in-vcluster orbit-operator chart to be installed once CoreDNS finished rolling out")
	}
}
