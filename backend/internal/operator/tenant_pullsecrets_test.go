package operator

import (
	"context"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func systemPullSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: systemNamespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{".dockerconfigjson": []byte(`{"auths":{}}`)},
	}
}

// replicatedPullSecret is a replica as ensureImagePullSecrets writes it.
func replicatedPullSecret(name, ns, tenant string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/component":           pullSecretComponent,
				"orbit.io/tenant":              tenant,
			},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{".dockerconfigjson": []byte(`{"auths":{}}`)},
	}
}

// TestEnsureImagePullSecrets_PrunesRemoved covers a credential rotated out of
// global.imagePullSecrets. Replication only ever visited the configured names,
// so the replica of a removed name stayed in every tenant namespace that had
// received it and the revoked credential remained usable there.
func TestEnsureImagePullSecrets_PrunesRemoved(t *testing.T) {
	const ns = "orbit-tenant-example"
	tenant := &OrbitTenant{ObjectMeta: metav1.ObjectMeta{Name: "example"}}

	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(
		systemPullSecret("keep-registry"),
		replicatedPullSecret("keep-registry", ns, "example"),
		replicatedPullSecret("rotated-out", ns, "example"),
		// Same managed-by label, different component: the tenant CA must
		// survive the prune.
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      "orbit-tenant-ca",
			Namespace: ns,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"},
		}},
	).Build()

	r := &TenantReconciler{
		Client:           c,
		Logger:           logging.NewLoggerWithOutput(io.Discard),
		ImagePullSecrets: []string{"keep-registry"},
	}

	if err := r.ensureImagePullSecrets(context.Background(), tenant); err != nil {
		t.Fatalf("ensureImagePullSecrets: %v", err)
	}

	assertSecretExists(t, c, ns, "keep-registry", true)
	assertSecretExists(t, c, ns, "rotated-out", false)
	assertSecretExists(t, c, ns, "orbit-tenant-ca", true)
}

// TestEnsureImagePullSecrets_LabelsExistingReplica covers a replica created
// before the component label existed. Without the label the prune cannot see
// it, so reconciliation has to add it.
func TestEnsureImagePullSecrets_LabelsExistingReplica(t *testing.T) {
	const ns = "orbit-tenant-example"
	tenant := &OrbitTenant{ObjectMeta: metav1.ObjectMeta{Name: "example"}}

	unlabelled := replicatedPullSecret("keep-registry", ns, "example")
	delete(unlabelled.Labels, "orbit.io/component")

	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(
		systemPullSecret("keep-registry"),
		unlabelled,
	).Build()

	r := &TenantReconciler{
		Client:           c,
		Logger:           logging.NewLoggerWithOutput(io.Discard),
		ImagePullSecrets: []string{"keep-registry"},
	}

	if err := r.ensureImagePullSecrets(context.Background(), tenant); err != nil {
		t.Fatalf("ensureImagePullSecrets: %v", err)
	}

	got := &corev1.Secret{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "keep-registry", Namespace: ns}, got); err != nil {
		t.Fatalf("get replica: %v", err)
	}
	if got.Labels["orbit.io/component"] != pullSecretComponent {
		t.Errorf("component label = %q, want %q", got.Labels["orbit.io/component"], pullSecretComponent)
	}
}

func assertSecretExists(t *testing.T, c client.Client, ns, name string, want bool) {
	t.Helper()
	err := c.Get(context.Background(), client.ObjectKey{Name: name, Namespace: ns}, &corev1.Secret{})
	switch {
	case err == nil && !want:
		t.Errorf("secret %q still exists", name)
	case apierrors.IsNotFound(err) && want:
		t.Errorf("secret %q was deleted", name)
	case err != nil && !apierrors.IsNotFound(err):
		t.Fatalf("get %q: %v", name, err)
	}
}
