package operator

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
	job := &batchv1.Job{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, job); err != nil {
		t.Fatalf("migration Job not created inside the vcluster: %v", err)
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

func TestEnsureFixtures_DeploysPostgresRedisDex(t *testing.T) {
	vcClient := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	secrets, err := ensureInVClusterSecrets(context.Background(), vcClient)
	if err != nil {
		t.Fatalf("ensureInVClusterSecrets: %v", err)
	}

	callback := "http://devenv.orbit.test:8880/auth/callback"
	if err := ensureFixtures(context.Background(), vcClient, secrets, callback); err != nil {
		t.Fatalf("ensureFixtures: %v", err)
	}
	// Re-apply must be idempotent.
	if err := ensureFixtures(context.Background(), vcClient, secrets, callback); err != nil {
		t.Fatalf("ensureFixtures (re-apply): %v", err)
	}

	for _, name := range []string{inVClusterPostgresName, inVClusterRedisName, inVClusterDexName} {
		svc := &corev1.Service{}
		if err := vcClient.Get(context.Background(), client.ObjectKey{Name: name, Namespace: inVClusterFixturesNamespace}, svc); err != nil {
			t.Errorf("service %q not created: %v", name, err)
		}
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
}
