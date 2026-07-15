// CRClient is this package's only path to the Kubernetes API: a narrow,
// CRUD-only abstraction over OrbitDevEnvironment CRs. It deliberately never
// exposes the underlying controller-runtime client.Client, so nothing in
// this package can reach for any other resource type — mirroring the
// identity ServiceAccount's own RBAC, which is CRUD on orbitdevenvironments
// only (charts/orbit/templates/identity/clusterrole.yaml).
package devenvironments

import (
	"context"
	"fmt"

	"github.com/clear-route/orbit/internal/platform/orbitapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CRClient is the storage-shaped contract the service layer uses to
// create/read/mutate/delete OrbitDevEnvironment CRs. Lifecycle state
// (phase, ingress host, expiry, kubeconfig material, ...) is always read
// live through this interface, never cached in Postgres — see repository.go.
type CRClient interface {
	Create(ctx context.Context, env *orbitapi.OrbitDevEnvironment) error
	Get(ctx context.Context, id string) (*orbitapi.OrbitDevEnvironment, error)
	List(ctx context.Context) ([]orbitapi.OrbitDevEnvironment, error)
	// PatchSpec re-reads the CR, applies mutate to its spec, and patches only
	// the changed fields (client.MergeFrom) so a concurrent operator status
	// update is never clobbered.
	PatchSpec(ctx context.Context, id string, mutate func(*orbitapi.OrbitDevEnvironmentSpec)) (*orbitapi.OrbitDevEnvironment, error)
	Delete(ctx context.Context, id string) error
	// Watch streams update events for a single named CR — used by
	// Kubeconfig's wait for the operator to sign a submitted CSR, bounded by
	// the caller's context deadline. metadata.name is a universally
	// supported field selector at the apiserver level for any resource
	// type, so this needs no custom index even on a non-cached direct
	// client (see NewCRClient).
	Watch(ctx context.Context, id string) (watch.Interface, error)
	// WatchAll streams ADDED/MODIFIED/DELETED events for every
	// OrbitDevEnvironment, unfiltered — used by the audit ingester
	// (devenvironment_audit_ingester.go) to derive events from
	// operator-driven phase transitions and CR deletions.
	WatchAll(ctx context.Context) (watch.Interface, error)
}

// crClient is the production CRClient: a plain controller-runtime
// client.WithWatch with no Manager or cache — every call is a direct
// request against the apiserver, matching this package's request/response
// HTTP API shape (it never reconciles anything).
type crClient struct {
	c client.WithWatch
}

// NewCRClient builds a CRClient from a *rest.Config, using orbitapi.NewScheme
// — the same "OrbitDevEnvironment plus core types, nothing else" scheme this
// package's RBAC is scoped to.
func NewCRClient(cfg *rest.Config) (CRClient, error) {
	c, err := client.NewWithWatch(cfg, client.Options{Scheme: orbitapi.NewScheme()})
	if err != nil {
		return nil, fmt.Errorf("build devenvironments k8s client: %w", err)
	}
	return &crClient{c: c}, nil
}

func (cc *crClient) Create(ctx context.Context, env *orbitapi.OrbitDevEnvironment) error {
	if err := cc.c.Create(ctx, env); err != nil {
		return fmt.Errorf("create OrbitDevEnvironment %q: %w", env.Name, err)
	}
	return nil
}

func (cc *crClient) Get(ctx context.Context, id string) (*orbitapi.OrbitDevEnvironment, error) {
	env := &orbitapi.OrbitDevEnvironment{}
	if err := cc.c.Get(ctx, client.ObjectKey{Name: id}, env); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get OrbitDevEnvironment %q: %w", id, err)
	}
	return env, nil
}

func (cc *crClient) List(ctx context.Context) ([]orbitapi.OrbitDevEnvironment, error) {
	list := &orbitapi.OrbitDevEnvironmentList{}
	if err := cc.c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list OrbitDevEnvironments: %w", err)
	}
	return list.Items, nil
}

func (cc *crClient) PatchSpec(ctx context.Context, id string, mutate func(*orbitapi.OrbitDevEnvironmentSpec)) (*orbitapi.OrbitDevEnvironment, error) {
	env, err := cc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	patch := client.MergeFrom(env.DeepCopy())
	mutate(&env.Spec)
	if err := cc.c.Patch(ctx, env, patch); err != nil {
		return nil, fmt.Errorf("patch OrbitDevEnvironment %q spec: %w", id, err)
	}
	return env, nil
}

func (cc *crClient) Delete(ctx context.Context, id string) error {
	env := &orbitapi.OrbitDevEnvironment{}
	env.Name = id
	if err := cc.c.Delete(ctx, env); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete OrbitDevEnvironment %q: %w", id, err)
	}
	return nil
}

func (cc *crClient) Watch(ctx context.Context, id string) (watch.Interface, error) {
	list := &orbitapi.OrbitDevEnvironmentList{}
	w, err := cc.c.Watch(ctx, list, client.MatchingFields{"metadata.name": id})
	if err != nil {
		return nil, fmt.Errorf("watch OrbitDevEnvironment %q: %w", id, err)
	}
	return w, nil
}

func (cc *crClient) WatchAll(ctx context.Context) (watch.Interface, error) {
	list := &orbitapi.OrbitDevEnvironmentList{}
	w, err := cc.c.Watch(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("watch all OrbitDevEnvironments: %w", err)
	}
	return w, nil
}

// BuildRestConfig resolves the Kubernetes REST config this package's CRClient
// uses: in-cluster first (production — cmd/identity runs as a pod under the
// orbit-identity ServiceAccount, RBAC narrowed to OrbitDevEnvironment only),
// falling back to KUBECONFIG for local dev. Mirrors cmd/operator/main.go's
// buildRestConfig; duplicated rather than imported since internal/operator is
// itself a feature package this one must not depend on.
func BuildRestConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}
