package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// registryFile is the in-memory representation of the docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md tenant registry
// YAML written to the orbit-tenants ConfigMap. Marshaled via sigs.k8s.io/yaml
// so the output is consistent with how the identity service parses it.
type registryFile struct {
	Version string          `json:"version"`
	Tenants []registryEntry `json:"tenants"`
}

type registryEntry struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	State       string       `json:"state"`
	OIDC        registryOIDC `json:"oidc"`
}

type registryOIDC struct {
	IssuerURL    string   `json:"issuerURL"`
	ClientID     string   `json:"clientID"`
	Scopes       []string `json:"scopes"`
	EmailDomains []string `json:"emailDomains,omitempty"`
}

const (
	// tenantsConfigMapName is the ConfigMap read by the identity service to
	// discover tenant OIDC providers. See docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md.
	tenantsConfigMapName = "orbit-tenants"

	// tenantsConfigMapKey is the key within the ConfigMap that holds the YAML.
	tenantsConfigMapKey = "tenants.yaml"
)

// TenantRegistry manages the orbit-tenants ConfigMap. It translates the set of
// active OrbitTenant CRs into the docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md tenant registry YAML format that
// the identity service reads at startup and watches for changes via a file watcher.
//
// Registry operations are idempotent: calling Sync multiple times with the
// same tenant list is safe.
type TenantRegistry struct {
	client client.Client
	logger logging.Logger
}

// NewTenantRegistry creates a TenantRegistry backed by the provided client.
func NewTenantRegistry(c client.Client, logger logging.Logger) *TenantRegistry {
	return &TenantRegistry{client: c, logger: logger}
}

// Sync rebuilds the orbit-tenants ConfigMap from the current list of
// OrbitTenant CRs. It creates the ConfigMap if absent and patches it if
// the content has changed.
func (r *TenantRegistry) Sync(ctx context.Context) error {
	tenants, err := r.listActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	yaml, err := r.buildRegistryYAML(tenants)
	if err != nil {
		return fmt.Errorf("build tenant registry: %w", err)
	}

	existing := &corev1.ConfigMap{}
	getErr := r.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      tenantsConfigMapName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		return r.createConfigMap(ctx, yaml)
	case getErr != nil:
		return fmt.Errorf("get %s ConfigMap: %w", tenantsConfigMapName, getErr)
	default:
		if existing.Data[tenantsConfigMapKey] == yaml {
			r.logger.Debug(ctx, "tenant registry unchanged, skipping update")
			return nil
		}
		return r.updateConfigMap(ctx, existing, yaml)
	}
}

// -----------------------------------------------------------------------------
// private helpers
// -----------------------------------------------------------------------------

func (r *TenantRegistry) listActiveTenants(ctx context.Context) ([]KoraTenant, error) {
	list := &KoraTenantList{}
	if err := r.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list OrbitTenants: %w", err)
	}

	active := make([]KoraTenant, 0, len(list.Items))
	for _, t := range list.Items {
		if t.Spec.Active {
			active = append(active, t)
		}
	}
	return active, nil
}

// buildRegistryYAML produces the docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md tenant registry YAML that the
// identity service parses. The format must remain stable: the identity
// service owns the schema; the operator only writes it.
//
// All tenants passed in are assumed active: listActiveTenants already
// filters the list before this function is called.
func (r *TenantRegistry) buildRegistryYAML(tenants []KoraTenant) (string, error) {
	entries := make([]registryEntry, 0, len(tenants))
	for _, t := range tenants {
		scopes := t.Spec.OIDC.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}
		entries = append(entries, registryEntry{
			ID:          t.Name,
			DisplayName: t.Spec.DisplayName,
			State:       "active",
			OIDC: registryOIDC{
				IssuerURL:    t.Spec.OIDC.IssuerURL,
				ClientID:     t.Spec.OIDC.ClientID,
				Scopes:       scopes,
				EmailDomains: t.Spec.OIDC.EmailDomains,
			},
		})
	}

	out, err := sigsyaml.Marshal(registryFile{Version: "v1", Tenants: entries})
	if err != nil {
		return "", fmt.Errorf("marshal tenant registry: %w", err)
	}
	return string(out), nil
}

func (r *TenantRegistry) createConfigMap(ctx context.Context, yaml string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantsConfigMapName,
			Namespace: systemNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/component":           "tenant-registry",
			},
		},
		Data: map[string]string{
			tenantsConfigMapKey: yaml,
		},
	}
	if err := r.client.Create(ctx, cm); err != nil {
		return fmt.Errorf("create %s ConfigMap: %w", tenantsConfigMapName, err)
	}
	r.logger.Info(ctx, "created tenant registry ConfigMap",
		logging.Field{Key: "configmap", Value: tenantsConfigMapName},
		logging.Field{Key: "namespace", Value: systemNamespace},
	)
	return nil
}

func (r *TenantRegistry) updateConfigMap(ctx context.Context, existing *corev1.ConfigMap, yaml string) error {
	patch := client.MergeFrom(existing.DeepCopy())
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[tenantsConfigMapKey] = yaml

	if err := r.client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s ConfigMap: %w", tenantsConfigMapName, err)
	}
	r.logger.Info(ctx, "updated tenant registry ConfigMap",
		logging.Field{Key: "configmap", Value: tenantsConfigMapName},
		logging.Field{Key: "namespace", Value: systemNamespace},
	)
	return nil
}
