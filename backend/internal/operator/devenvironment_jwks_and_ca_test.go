package operator

import (
	"context"
	"fmt"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func newIdentityTestReconciler(t *testing.T, objs ...client.Object) *DevEnvironmentReconciler {
	t.Helper()
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &DevEnvironmentReconciler{
		Client: c,
		Logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

// TestMirrorIstioCARootCert_CreatesLiteralNameFromSyncedConfigMap guards
// pinned decision 10's CA-root mirror: every SPIRE/Istio-injected pod mounts
// a volume literally named "istio-ca-root-cert", but the vcluster syncer
// only ever produces the translated name
// "istio-ca-root-cert-x-istio-system-x-vcluster" on the host — this must
// mirror the translated source into the literal-named destination.
func TestMirrorIstioCARootCert_CreatesLiteralNameFromSyncedConfigMap(t *testing.T) {
	env := newTestEnvForIdentity("alice-env")
	ns := devEnvNamespace(env)
	sourceName := fmt.Sprintf("%s-x-istio-system-x-%s", istioCARootCertConfigMapName, vclusterReleaseName)
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: ns},
		Data:       map[string]string{"root-cert.pem": "CERT-DATA"},
	}

	r := newIdentityTestReconciler(t, source)
	if err := r.mirrorIstioCARootCert(context.Background(), env); err != nil {
		t.Fatalf("mirrorIstioCARootCert: %v", err)
	}

	dest := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: istioCARootCertConfigMapName, Namespace: ns}, dest); err != nil {
		t.Fatalf("mirrored ConfigMap %q not created: %v", istioCARootCertConfigMapName, err)
	}
	if dest.Data["root-cert.pem"] != "CERT-DATA" {
		t.Errorf("mirrored data = %v, want root-cert.pem=CERT-DATA", dest.Data)
	}
}

// TestMirrorIstioCARootCert_SourceMissingIsNonFatal guards the "best-effort"
// contract: the synced ConfigMap doesn't exist until the in-vcluster
// istiod's own namespace-controller pass completes, which can lag behind
// this reconcile step — a missing source must not be an error.
func TestMirrorIstioCARootCert_SourceMissingIsNonFatal(t *testing.T) {
	env := newTestEnvForIdentity("alice-env")
	r := newIdentityTestReconciler(t)
	if err := r.mirrorIstioCARootCert(context.Background(), env); err != nil {
		t.Fatalf("mirrorIstioCARootCert with no source configmap should be a non-fatal no-op, got: %v", err)
	}
}

// TestMirrorIstioCARootCert_UpdatesWhenSourceChanges guards re-mirroring on
// CA rotation: the destination must track the source, not just be created
// once and left stale.
func TestMirrorIstioCARootCert_UpdatesWhenSourceChanges(t *testing.T) {
	env := newTestEnvForIdentity("alice-env")
	ns := devEnvNamespace(env)
	sourceName := fmt.Sprintf("%s-x-istio-system-x-%s", istioCARootCertConfigMapName, vclusterReleaseName)
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: ns},
		Data:       map[string]string{"root-cert.pem": "CERT-V1"},
	}
	dest := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: istioCARootCertConfigMapName, Namespace: ns},
		Data:       map[string]string{"root-cert.pem": "STALE"},
	}

	r := newIdentityTestReconciler(t, source, dest)
	if err := r.mirrorIstioCARootCert(context.Background(), env); err != nil {
		t.Fatalf("mirrorIstioCARootCert: %v", err)
	}

	got := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: istioCARootCertConfigMapName, Namespace: ns}, got); err != nil {
		t.Fatalf("get mirrored configmap: %v", err)
	}
	if got.Data["root-cert.pem"] != "CERT-V1" {
		t.Errorf("mirrored data = %v, want the refreshed source data", got.Data)
	}
}
