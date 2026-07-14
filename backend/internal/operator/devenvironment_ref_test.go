package operator

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

type fakeRefResolver struct {
	tag string
	err error
}

func (f *fakeRefResolver) ResolveMain(context.Context) (string, error) {
	return f.tag, f.err
}

func newRefTestReconciler(t *testing.T, resolver RefResolver, env *OrbitDevEnvironment) *DevEnvironmentReconciler {
	t.Helper()
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&OrbitDevEnvironment{}).WithObjects(env).Build()
	return &DevEnvironmentReconciler{
		Client:      c,
		Logger:      logging.NewLoggerWithOutput(io.Discard),
		RefResolver: resolver,
	}
}

func newRefTestEnv(name, ref string) *OrbitDevEnvironment {
	return &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       OrbitDevEnvironmentSpec{Owner: "dev@example.com", Ref: ref},
	}
}

// TestResolveRef_ImmutableTagPassthroughNeverCallsResolver guards the
// "verbatim tag" branch of pinned decision B2: an already-immutable
// spec.ref must never touch the network-calling RefResolver at all.
func TestResolveRef_ImmutableTagPassthroughNeverCallsResolver(t *testing.T) {
	env := newRefTestEnv("alice", "v1.2.3")
	resolver := &fakeRefResolver{err: errors.New("must not be called")}
	r := newRefTestReconciler(t, resolver, env)

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

// TestResolveRef_SymbolicMainResolvesOnceThenReuses guards decision B2's
// core promise: "main" resolves via RefResolver exactly once; every later
// call (simulating a subsequent reconcile) reuses status.resolvedRef and
// never calls the resolver again — an environment is pinned from birth.
func TestResolveRef_SymbolicMainResolvesOnceThenReuses(t *testing.T) {
	env := newRefTestEnv("bob", "main")
	resolver := &fakeRefResolver{tag: "main-abc1234"}
	r := newRefTestReconciler(t, resolver, env)

	ref, failed, err := r.resolveRef(context.Background(), env)
	if err != nil {
		t.Fatalf("resolveRef (first call): %v", err)
	}
	if failed {
		t.Fatal("failed = true, want false")
	}
	if ref != "main-abc1234" {
		t.Errorf("ref = %q, want %q", ref, "main-abc1234")
	}
	if env.Status.ResolvedRef != "main-abc1234" {
		t.Fatalf("status.resolvedRef = %q, want %q", env.Status.ResolvedRef, "main-abc1234")
	}

	// Second call: resolver now returns a DIFFERENT tag — if resolveRef
	// re-resolved, we'd see it here. It must not.
	resolver.tag = "main-deadbee"
	ref2, failed2, err2 := r.resolveRef(context.Background(), env)
	if err2 != nil {
		t.Fatalf("resolveRef (second call): %v", err2)
	}
	if failed2 {
		t.Fatal("failed = true on second call, want false")
	}
	if ref2 != "main-abc1234" {
		t.Errorf("second call ref = %q, want the already-resolved %q (must not re-resolve)", ref2, "main-abc1234")
	}
}

// TestResolveRef_EmptySpecRefTreatedAsMain guards the CRD default: an unset
// spec.ref (defaulted to "main" by the CRD schema, but observed empty on a
// raw Go struct in a test) must resolve the same way "main" explicitly does.
func TestResolveRef_EmptySpecRefTreatedAsMain(t *testing.T) {
	env := newRefTestEnv("carol", "")
	resolver := &fakeRefResolver{tag: "main-abc1234"}
	r := newRefTestReconciler(t, resolver, env)

	ref, failed, err := r.resolveRef(context.Background(), env)
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if failed {
		t.Fatal("failed = true, want false")
	}
	if ref != "main-abc1234" {
		t.Errorf("ref = %q, want %q", ref, "main-abc1234")
	}
}

// TestResolveRef_ResolverFailureIsNonRetryableFailed guards the
// non-retryable classification: a RefResolver error must set phase Failed
// directly (bypassing failReconcile's consecutive-failure counter) and
// report failed=true.
func TestResolveRef_ResolverFailureIsNonRetryableFailed(t *testing.T) {
	env := newRefTestEnv("dave", "main")
	resolver := &fakeRefResolver{err: errors.New("registry unreachable")}
	r := newRefTestReconciler(t, resolver, env)

	_, failed, err := r.resolveRef(context.Background(), env)
	if err == nil {
		t.Fatal("expected an error when the resolver fails")
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

// TestResolveRef_InvalidImmutableTagSyntaxIsNonRetryableFailed guards ref
// syntax validation: a spec.ref that is neither "main" nor a valid
// immutable-tag pattern must be non-retryable Failed, same as a resolver
// failure — there is nothing a retry can fix about a malformed ref.
func TestResolveRef_InvalidImmutableTagSyntaxIsNonRetryableFailed(t *testing.T) {
	env := newRefTestEnv("erin", "Not Valid!!")
	resolver := &fakeRefResolver{err: errors.New("must not be called")}
	r := newRefTestReconciler(t, resolver, env)

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

// TestPickNewestMainTag covers the pure tag-selection logic in isolation,
// without any registry involved.
func TestPickNewestMainTag(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		candidates []string
		createdAt  map[string]time.Time
		want       string
		wantErr    bool
	}{
		{
			name:       "picks the latest created time regardless of lexical/sha order",
			candidates: []string{"main-zzzzzzz", "main-aaaaaaa"},
			createdAt: map[string]time.Time{
				"main-zzzzzzz": now.Add(-1 * time.Hour),
				"main-aaaaaaa": now,
			},
			want: "main-aaaaaaa",
		},
		{
			name:       "skips candidates missing a created time (failed Config fetch)",
			candidates: []string{"main-broken0", "main-good000"},
			createdAt: map[string]time.Time{
				"main-good000": now,
			},
			want: "main-good000",
		},
		{
			name:       "every candidate missing created time is an error",
			candidates: []string{"main-broken0", "main-broken1"},
			createdAt:  map[string]time.Time{},
			wantErr:    true,
		},
		{
			name:       "no candidates at all is an error",
			candidates: nil,
			createdAt:  map[string]time.Time{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickNewestMainTag(tt.candidates, tt.createdAt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got = %q, want %q", got, tt.want)
			}
		})
	}
}
