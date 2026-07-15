package operator

import (
	"context"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func newRefTestReconciler(t *testing.T, env *OrbitDevEnvironment) *DevEnvironmentReconciler {
	t.Helper()
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&OrbitDevEnvironment{}).WithObjects(env).Build()
	return &DevEnvironmentReconciler{
		Client: c,
		Logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

func newRefTestEnv(name, ref string) *OrbitDevEnvironment {
	return &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       OrbitDevEnvironmentSpec{Owner: "dev@example.com", Ref: ref},
	}
}

// TestResolveRef_ImmutableTagPassthrough guards the "verbatim tag" branch of
// decision B2: an already-immutable spec.ref is stored exactly as given,
// never coerced to "main".
func TestResolveRef_ImmutableTagPassthrough(t *testing.T) {
	env := newRefTestEnv("alice", "v1.2.3")
	r := newRefTestReconciler(t, env)

	ref, failed, err := r.resolveRef(context.Background(), env)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if failed {
		t.Fatal("failed = true, want false")
	}
	if ref != "v1.2.3" {
		t.Errorf("ref = %q, want %q", ref, "v1.2.3")
	}
	if env.Status.ResolvedRef != "v1.2.3" {
		t.Errorf("status.resolvedRef = %q, want %q", env.Status.ResolvedRef, "v1.2.3")
	}
}

// TestResolveRef_SymbolicMainResolvesToLiteralMainThenReuses guards decision
// B2: the symbolic ref "main" resolves to the literal floating tag "main" —
// no registry lookup, no pinning to a point-in-time build — and every later
// call (simulating a subsequent reconcile) reuses status.resolvedRef without
// re-deriving it.
func TestResolveRef_SymbolicMainResolvesToLiteralMainThenReuses(t *testing.T) {
	env := newRefTestEnv("bob", "main")
	r := newRefTestReconciler(t, env)

	ref, failed, err := r.resolveRef(context.Background(), env)
	if err != nil {
		t.Fatalf("resolveRef (first call): %v", err)
	}
	if failed {
		t.Fatal("failed = true, want false")
	}
	if ref != "main" {
		t.Errorf("ref = %q, want %q", ref, "main")
	}
	if env.Status.ResolvedRef != "main" {
		t.Fatalf("status.resolvedRef = %q, want %q", env.Status.ResolvedRef, "main")
	}

	ref2, failed2, err2 := r.resolveRef(context.Background(), env)
	if err2 != nil {
		t.Fatalf("resolveRef (second call): %v", err2)
	}
	if failed2 {
		t.Fatal("failed = true on second call, want false")
	}
	if ref2 != "main" {
		t.Errorf("second call ref = %q, want %q", ref2, "main")
	}
}

// TestResolveRef_EmptySpecRefTreatedAsMain guards the CRD default: an unset
// spec.ref (defaulted to "main" by the CRD schema, but observed empty on a
// raw Go struct in a test) must resolve the same way "main" explicitly does.
func TestResolveRef_EmptySpecRefTreatedAsMain(t *testing.T) {
	env := newRefTestEnv("carol", "")
	r := newRefTestReconciler(t, env)

	ref, failed, err := r.resolveRef(context.Background(), env)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if failed {
		t.Fatal("failed = true, want false")
	}
	if ref != "main" {
		t.Errorf("ref = %q, want %q", ref, "main")
	}
}

// TestResolveRef_InvalidImmutableTagSyntaxIsNonRetryableFailed guards ref
// syntax validation: a spec.ref that is neither "main" nor a valid
// immutable-tag pattern must be non-retryable Failed — there is nothing a
// retry can fix about a malformed ref.
func TestResolveRef_InvalidImmutableTagSyntaxIsNonRetryableFailed(t *testing.T) {
	env := newRefTestEnv("erin", "Not Valid!!")
	r := newRefTestReconciler(t, env)

	_, failed, err := r.resolveRef(context.Background(), env)
	if err == nil {
		t.Fatal("expected an error for an invalid ref")
	}
	if !failed {
		t.Error("failed = false, want true")
	}

	got := &OrbitDevEnvironment{}
	if getErr := r.Get(context.Background(), client.ObjectKey{Name: env.Name}, got); getErr != nil {
		t.Fatalf("get env: %v", getErr)
	}
	if got.Status.Phase != DevEnvPhaseFailed {
		t.Errorf("status.phase = %q, want %q", got.Status.Phase, DevEnvPhaseFailed)
	}
}
