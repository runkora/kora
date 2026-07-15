package operator

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

// TestReconcile_InstallsInVClusterPlatformOnceReady, which asserts on the
// full reconcileReady flow (host trust domain, MeshJWTValidation, resolved
// ref pinning every service image, the frontend/identity devEnvironmentId
// passthrough, and the migration Job's image), and
// TestReconcile_NoHostOrbitPlatformFailsInVClusterInstall land alongside
// devenvironment_controller_test.go's shared test harness
// (newDevEnvTestReconcilerFull et al.), once WP-9 completes the package.

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

// TestInVClusterServiceImageTags_SetsEveryOrbitServiceToResolvedRefWithAlwaysPull
// guards the resolvedRef threading added in this WP: every orbit-chart
// service image.tag must resolve to the same ref the in-vcluster
// orbit-operator itself was installed at, with pullPolicy: Always — resolvedRef
// is typically the literal floating tag "main", so nodes must actually re-pull
// rather than reuse whatever they cached under that tag name.
func TestInVClusterServiceImageTags_SetsEveryOrbitServiceToResolvedRefWithAlwaysPull(t *testing.T) {
	values := inVClusterServiceImageTags("main-abc1234")
	if len(values) != len(inVClusterServiceImageServices) {
		t.Fatalf("got %d services, want %d", len(values), len(inVClusterServiceImageServices))
	}
	for _, svc := range inVClusterServiceImageServices {
		svcValues, ok := values[svc].(map[string]any)
		if !ok {
			t.Fatalf("values[%q] missing or wrong type: %#v", svc, values[svc])
		}
		image, ok := svcValues["image"].(map[string]any)
		if !ok {
			t.Fatalf("values[%q][\"image\"] missing or wrong type: %#v", svc, svcValues["image"])
		}
		if image["tag"] != "main-abc1234" {
			t.Errorf("values[%q][\"image\"][\"tag\"] = %v, want %q", svc, image["tag"], "main-abc1234")
		}
		if image["pullPolicy"] != "Always" {
			t.Errorf("values[%q][\"image\"][\"pullPolicy\"] = %v, want %q", svc, image["pullPolicy"], "Always")
		}
	}
	if _, ok := values["opaGateway"]; ok {
		t.Error(`values["opaGateway"] must not be set — it is a third-party image, not built from this repo`)
	}
}

// TestEnsureInVClusterDevEnvAdminBinding_CreatesGroupBoundToClusterAdmin
// guards decision E2: the kubeconfig client cert's Subject Organization
// "orbit:devenv-admin" must resolve to cluster-admin inside the
// environment's own vcluster, as a nameable, revocable RBAC object rather
// than the built-in O=system:masters bypass.
func TestEnsureInVClusterDevEnvAdminBinding_CreatesGroupBoundToClusterAdmin(t *testing.T) {
	scheme := NewScheme()
	vcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DevEnvironmentReconciler{}

	if err := r.ensureInVClusterDevEnvAdminBinding(context.Background(), vcClient); err != nil {
		t.Fatalf("ensureInVClusterDevEnvAdminBinding: %v", err)
	}

	got := &rbacv1.ClusterRoleBinding{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterDevEnvAdminBindingName}, got); err != nil {
		t.Fatalf("get ClusterRoleBinding: %v", err)
	}
	if len(got.Subjects) != 1 {
		t.Fatalf("subjects = %v, want exactly one", got.Subjects)
	}
	if got.Subjects[0].Kind != rbacv1.GroupKind || got.Subjects[0].Name != "orbit:devenv-admin" {
		t.Errorf("subject = %+v, want Kind=Group Name=orbit:devenv-admin", got.Subjects[0])
	}
	if got.RoleRef.Kind != "ClusterRole" || got.RoleRef.Name != "cluster-admin" {
		t.Errorf("roleRef = %+v, want Kind=ClusterRole Name=cluster-admin", got.RoleRef)
	}

	// Idempotent: calling again must not error or duplicate.
	if err := r.ensureInVClusterDevEnvAdminBinding(context.Background(), vcClient); err != nil {
		t.Fatalf("second ensureInVClusterDevEnvAdminBinding: %v", err)
	}
}
