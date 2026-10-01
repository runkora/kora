// Mesh registration of inference provider hosts: one Istio ServiceEntry per
// InferenceProvider, so the gateway reaches its providers on a REGISTRY_ONLY
// mesh. See docs/adr/2026-10-01-crd-inference-providers.md, Amendment 1.
package operator

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders/v1alpha1"
)

// serviceEntryGVK is Istio's, which is not in the operator's scheme.
var serviceEntryGVK = schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1", Kind: "ServiceEntry"}

// providerEndpoint is one host and port the gateway dials for a provider.
type providerEndpoint struct {
	host string
	port int
	// plain is true for an http:// endpoint, which the mesh routes as HTTP.
	plain bool
}

// providerEndpoints lists the hosts the gateway's connector for p dials. It
// mirrors the connectors in internal/inferencegw/providers.
func providerEndpoints(p *ip.InferenceProvider) ([]providerEndpoint, error) {
	switch p.Spec.Type {
	case ip.TypeBedrock:
		return bedrockEndpoints(p.Spec.Bedrock)
	case ip.TypeVertexAI:
		return vertexEndpoints(p.Spec.VertexAI)
	case ip.TypeAzure:
		if p.Spec.Azure == nil {
			return nil, fmt.Errorf("azure provider %q has no azure block", p.Name)
		}
		urls := []string{p.Spec.Azure.Endpoint}
		if p.Spec.Azure.AnthropicEndpoint != "" {
			urls = append(urls, p.Spec.Azure.AnthropicEndpoint)
		}
		return urlEndpoints(urls...)
	case ip.TypeCompatible:
		if p.Spec.Compatible == nil {
			return nil, fmt.Errorf("compatible provider %q has no compatible block", p.Name)
		}
		return urlEndpoints(p.Spec.Compatible.BaseURL)
	default:
		return nil, fmt.Errorf("provider %q has unknown type %q", p.Name, p.Spec.Type)
	}
}

// bedrockEndpoints covers the runtime, OpenAI-compatible and Anthropic calls
// (bedrock-runtime), model discovery (bedrock) and the Mantle endpoints. STS
// serves AssumeRole and IRSA's web identity exchange.
func bedrockEndpoints(b *ip.Bedrock) ([]providerEndpoint, error) {
	if b == nil || b.Region == "" {
		return nil, fmt.Errorf("bedrock provider has no region")
	}
	hosts := []string{
		"bedrock-runtime." + b.Region + ".amazonaws.com",
		"bedrock." + b.Region + ".amazonaws.com",
		"bedrock-mantle." + b.Region + ".api.aws",
	}
	if b.AssumeRoleARN != "" || b.Credentials == nil {
		hosts = append(hosts, "sts."+b.Region+".amazonaws.com")
	}
	return tlsEndpoints(hosts...), nil
}

// vertexEndpoints covers the regional OpenAI-compatible endpoint, the
// Anthropic SDK's endpoint for anthropicLocation, and global Model Garden
// discovery. A service account key exchanges its token at oauth2.googleapis.com.
func vertexEndpoints(v *ip.VertexAI) ([]providerEndpoint, error) {
	if v == nil || v.Location == "" {
		return nil, fmt.Errorf("vertexai provider has no location")
	}
	anthropicLocation := v.AnthropicLocation
	if anthropicLocation == "" {
		anthropicLocation = v.Location
	}
	hosts := []string{
		"aiplatform.googleapis.com",
		v.Location + "-aiplatform.googleapis.com",
		vertexAnthropicHost(anthropicLocation),
	}
	if v.CredentialsSecretRef != nil {
		hosts = append(hosts, "oauth2.googleapis.com")
	}
	return tlsEndpoints(hosts...), nil
}

// vertexAnthropicHost matches the base URL the Anthropic Vertex SDK picks.
func vertexAnthropicHost(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + location + ".rep.googleapis.com"
	default:
		return location + "-aiplatform.googleapis.com"
	}
}

func tlsEndpoints(hosts ...string) []providerEndpoint {
	out := make([]providerEndpoint, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, providerEndpoint{host: h, port: 443})
	}
	return out
}

func urlEndpoints(raw ...string) ([]providerEndpoint, error) {
	out := make([]providerEndpoint, 0, len(raw))
	for _, r := range raw {
		u, err := url.Parse(r)
		if err != nil {
			return nil, fmt.Errorf("parse provider URL %q: %w", r, err)
		}
		port, err := urlPort(u)
		if err != nil {
			return nil, err
		}
		if u.Hostname() == "" {
			return nil, fmt.Errorf("provider URL %q has no host", r)
		}
		out = append(out, providerEndpoint{host: u.Hostname(), port: port, plain: u.Scheme == schemeHTTP})
	}
	return out, nil
}

const schemeHTTP = "http"

// urlPort is the URL's port, or its scheme's default.
func urlPort(u *url.URL) (int, error) {
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("URL %q has an invalid port", u.String())
		}
		return n, nil
	}
	switch u.Scheme {
	case "https":
		return 443, nil
	case schemeHTTP:
		return 80, nil
	default:
		return 0, fmt.Errorf("URL %q has no port and scheme %q has no default", u.String(), u.Scheme)
	}
}

// providerServiceEntryName is deterministic, so a reconcile updates in place.
func providerServiceEntryName(p *ip.InferenceProvider) string {
	return "inference-provider-" + p.Name
}

// providerServiceEntry registers the provider's hosts with the mesh. exportTo
// "." keeps them visible to the system namespace only, where the gateway runs.
func providerServiceEntry(p *ip.InferenceProvider, endpoints []providerEndpoint) *unstructured.Unstructured {
	var hosts []string
	type portKey struct {
		number int
		plain  bool
	}
	var portKeys []portKey
	for _, e := range endpoints {
		if !slices.Contains(hosts, e.host) {
			hosts = append(hosts, e.host)
		}
		if k := (portKey{e.port, e.plain}); !slices.Contains(portKeys, k) {
			portKeys = append(portKeys, k)
		}
	}
	ports := make([]any, 0, len(portKeys))
	for _, k := range portKeys {
		name, protocol := fmt.Sprintf("tls-%d", k.number), "TLS"
		if k.plain {
			name, protocol = fmt.Sprintf("http-%d", k.number), "HTTP"
		}
		ports = append(ports, map[string]any{"number": int64(k.number), "name": name, "protocol": protocol})
	}
	hostList := make([]any, 0, len(hosts))
	for _, h := range hosts {
		hostList = append(hostList, h)
	}

	se := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":      providerServiceEntryName(p),
			"namespace": p.Namespace,
			"labels":    map[string]any{"app.kubernetes.io/managed-by": "orbit-operator"},
		},
		"spec": map[string]any{
			"hosts":      hostList,
			"ports":      ports,
			"location":   "MESH_EXTERNAL",
			"resolution": "DNS",
			"exportTo":   []any{"."},
		},
	}}
	se.SetGroupVersionKind(serviceEntryGVK)
	return se
}

// providerClient is what the reconciler calls on the API server.
type providerClient interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
}

// InferenceProviderReconciler keeps one ServiceEntry per InferenceProvider in
// the system namespace. It never writes the provider or its status; the
// ServiceEntry's owner reference has the garbage collector remove it.
type InferenceProviderReconciler struct {
	Client providerClient
	Scheme *runtime.Scheme
}

// Reconcile creates or updates the provider's ServiceEntry.
func (r *InferenceProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	p := &ip.InferenceProvider{}
	if err := r.Client.Get(ctx, req.NamespacedName, p); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get InferenceProvider %q: %w", req.NamespacedName, err)
	}
	endpoints, err := providerEndpoints(p)
	if err != nil {
		return ctrl.Result{}, err
	}
	se := providerServiceEntry(p, endpoints)
	if err := controllerutil.SetControllerReference(p, se, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("own ServiceEntry for %q: %w", p.Name, err)
	}
	createErr := r.Client.Create(ctx, se)
	if createErr == nil {
		return ctrl.Result{}, nil
	}
	if !apierrors.IsAlreadyExists(createErr) {
		return ctrl.Result{}, fmt.Errorf("create ServiceEntry %q: %w", se.GetName(), createErr)
	}
	if err := r.Client.Patch(ctx, se, client.Merge, client.FieldOwner("orbit-operator")); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch ServiceEntry %q: %w", se.GetName(), err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager reconciles on create and on spec changes.
func (r *InferenceProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ip.InferenceProvider{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
