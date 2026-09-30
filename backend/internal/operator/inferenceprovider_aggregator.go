package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Reasons on the InferenceProvider Aggregated condition.
const (
	reasonProviderAggregated = "Aggregated"
	reasonWrongNamespace     = "WrongNamespace"
	reasonInvalidSpec        = "InvalidSpec"
	reasonSecretNotFound     = "SecretNotFound"
	reasonSecretKeyNotFound  = "SecretKeyNotFound"
)

// inferenceProviderStore is what the aggregator reads and writes.
type inferenceProviderStore interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
}

// providerResult is the outcome for one resource. It becomes its status.
type providerResult struct {
	providerName string
	reason       string
	message      string
}

func (r providerResult) ok() bool { return r.reason == reasonProviderAggregated }

// errProviderRejected carries the status reason for a resource left out of the
// aggregate.
type errProviderRejected struct{ reason, message string }

func (e *errProviderRejected) Error() string { return e.message }

func rejected(reason, format string, args ...any) error {
	return &errProviderRejected{reason: reason, message: fmt.Sprintf(format, args...)}
}

// InferenceProviderAggregator renders every InferenceProvider in the system
// namespace into the ConfigMap and Secret the inference gateway mounts.
type InferenceProviderAggregator struct {
	store  inferenceProviderStore
	logger logging.Logger
}

// NewInferenceProviderAggregator creates an aggregator.
func NewInferenceProviderAggregator(store inferenceProviderStore, logger logging.Logger) *InferenceProviderAggregator {
	return &InferenceProviderAggregator{store: store, logger: logger}
}

// Sync renders the aggregate and returns the outcome per resource. A resource
// that cannot be used is left out; it never blocks the others.
func (a *InferenceProviderAggregator) Sync(ctx context.Context) (map[types.NamespacedName]providerResult, error) {
	list := &InferenceProviderList{}
	if err := a.store.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list InferenceProviders: %w", err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })

	results := make(map[types.NamespacedName]providerResult, len(list.Items))
	entries := make([]inferenceproviders.Entry, 0, len(list.Items))
	secretData := map[string][]byte{}
	secrets := map[string]*corev1.Secret{}

	for i := range list.Items {
		p := &list.Items[i]
		key := types.NamespacedName{Namespace: p.Namespace, Name: p.Name}
		entry, data, err := a.render(ctx, p, secrets)
		if err != nil {
			var rej *errProviderRejected
			if !errors.As(err, &rej) {
				return nil, err
			}
			results[key] = providerResult{reason: rej.reason, message: rej.message}
			a.logger.Warn(ctx, "InferenceProvider left out of the aggregate",
				logging.Field{Key: "inferenceprovider", Value: key.String()},
				logging.Field{Key: "reason", Value: rej.reason},
				logging.Field{Key: "message", Value: rej.message},
			)
			continue
		}
		entries = append(entries, entry)
		maps.Copy(secretData, data)
		results[key] = providerResult{
			providerName: entry.Name,
			reason:       reasonProviderAggregated,
			message:      "rendered into the " + inferenceproviders.ConfigMapName + " ConfigMap",
		}
	}

	registry, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshal inference providers: %w", err)
	}
	if err := a.upsertConfigMap(ctx, string(registry)); err != nil {
		return nil, err
	}
	if err := a.upsertSecret(ctx, secretData); err != nil {
		return nil, err
	}
	return results, nil
}

// render turns one resource into its entry and the credential values it needs.
func (a *InferenceProviderAggregator) render(ctx context.Context, p *InferenceProvider, cache map[string]*corev1.Secret) (inferenceproviders.Entry, map[string][]byte, error) {
	if p.Namespace != systemNamespace {
		return inferenceproviders.Entry{}, nil, rejected(reasonWrongNamespace,
			"InferenceProvider must be in the %s namespace", systemNamespace)
	}
	if !inferenceproviders.ValidResourceName(p.Name) {
		return inferenceproviders.Entry{}, nil, rejected(reasonInvalidSpec, "name %q is not a DNS label", p.Name)
	}
	t := inferenceproviders.Type(p.Spec.Type)
	entry := inferenceproviders.Entry{
		Name:        inferenceproviders.ProviderName(t, p.Name),
		Resource:    p.Name,
		Type:        t,
		DisplayName: p.Spec.DisplayName,
		Models:      p.Spec.Models,
	}
	data := map[string][]byte{}
	copyKey := func(ref SecretKeyRef, field string, required bool) (string, error) {
		return a.copySecretKey(ctx, cache, data, p.Name, ref, field, required)
	}

	var err error
	switch {
	case p.Spec.Bedrock != nil:
		entry.Bedrock, err = renderBedrock(p.Spec.Bedrock, copyKey)
	case p.Spec.VertexAI != nil:
		entry.VertexAI, err = renderVertexAI(p.Spec.VertexAI, copyKey)
	case p.Spec.Azure != nil:
		entry.Azure, err = renderAzure(p.Spec.Azure, copyKey)
	case p.Spec.OpenAI != nil:
		entry.OpenAI, err = renderOpenAI(p.Spec.OpenAI, copyKey)
	case p.Spec.Anthropic != nil:
		entry.Anthropic, err = renderAnthropic(p.Spec.Anthropic, copyKey)
	}
	if err != nil {
		return inferenceproviders.Entry{}, nil, err
	}
	if vErr := entry.Validate(); vErr != nil {
		return inferenceproviders.Entry{}, nil, rejected(reasonInvalidSpec, "%s", vErr.Error())
	}
	return entry, data, nil
}

type copyKeyFunc func(ref SecretKeyRef, field string, required bool) (string, error)

func renderBedrock(s *BedrockProviderSpec, copyKey copyKeyFunc) (*inferenceproviders.Bedrock, error) {
	if s.Region == "" {
		return nil, rejected(reasonInvalidSpec, "bedrock.region is required")
	}
	out := &inferenceproviders.Bedrock{Region: s.Region, AssumeRoleARN: s.AssumeRoleARN, ExternalID: s.ExternalID}
	if s.Credentials == nil {
		return out, nil
	}
	name := s.Credentials.SecretRef.Name
	var err error
	if out.AccessKeyIDKey, err = copyKey(SecretKeyRef{Name: name, Key: inferenceproviders.FieldAccessKeyID}, inferenceproviders.FieldAccessKeyID, true); err != nil {
		return nil, err
	}
	if out.SecretAccessKeyKey, err = copyKey(SecretKeyRef{Name: name, Key: inferenceproviders.FieldSecretAccessKey}, inferenceproviders.FieldSecretAccessKey, true); err != nil {
		return nil, err
	}
	if out.SessionTokenKey, err = copyKey(SecretKeyRef{Name: name, Key: inferenceproviders.FieldSessionToken}, inferenceproviders.FieldSessionToken, false); err != nil {
		return nil, err
	}
	return out, nil
}

func renderVertexAI(s *VertexAIProviderSpec, copyKey copyKeyFunc) (*inferenceproviders.VertexAI, error) {
	if s.ProjectID == "" || s.Location == "" {
		return nil, rejected(reasonInvalidSpec, "vertexai.projectID and vertexai.location are required")
	}
	out := &inferenceproviders.VertexAI{ProjectID: s.ProjectID, Location: s.Location, AnthropicLocation: s.AnthropicLocation}
	if s.CredentialsSecretRef == nil {
		return out, nil
	}
	var err error
	out.CredentialsKey, err = copyKey(*s.CredentialsSecretRef, inferenceproviders.FieldCredentialsJSON, true)
	return out, err
}

// requireHTTPS rejects a plain HTTP upstream. The gateway pod's identity-proxy
// replaces the Authorization header of every plain HTTP call with the
// gateway's platform token: the upstream would lose its key and receive that
// token.
func requireHTTPS(field, url string) error {
	if url != "" && !strings.HasPrefix(url, "https://") {
		return rejected(reasonInvalidSpec, "%s must be an https:// URL", field)
	}
	return nil
}

func renderAzure(s *AzureProviderSpec, copyKey copyKeyFunc) (*inferenceproviders.Azure, error) {
	if s.Endpoint == "" || s.APIVersion == "" {
		return nil, rejected(reasonInvalidSpec, "azure.endpoint and azure.apiVersion are required")
	}
	if err := requireHTTPS("azure.endpoint", s.Endpoint); err != nil {
		return nil, err
	}
	if err := requireHTTPS("azure.anthropicEndpoint", s.AnthropicEndpoint); err != nil {
		return nil, err
	}
	out := &inferenceproviders.Azure{Endpoint: s.Endpoint, APIVersion: s.APIVersion, AnthropicEndpoint: s.AnthropicEndpoint}
	var err error
	out.APIKeyKey, err = copyKey(s.APIKeySecretRef, inferenceproviders.FieldAPIKey, true)
	return out, err
}

func renderOpenAI(s *OpenAIProviderSpec, copyKey copyKeyFunc) (*inferenceproviders.OpenAI, error) {
	if err := requireHTTPS("openai.baseURL", s.BaseURL); err != nil {
		return nil, err
	}
	out := &inferenceproviders.OpenAI{BaseURL: s.BaseURL, CACert: s.CACert}
	if out.BaseURL == "" {
		out.BaseURL = inferenceproviders.DefaultOpenAIBaseURL
	}
	if s.APIKeySecretRef == nil {
		return out, nil
	}
	var err error
	out.APIKeyKey, err = copyKey(*s.APIKeySecretRef, inferenceproviders.FieldAPIKey, true)
	return out, err
}

func renderAnthropic(s *AnthropicProviderSpec, copyKey copyKeyFunc) (*inferenceproviders.Anthropic, error) {
	if err := requireHTTPS("anthropic.baseURL", s.BaseURL); err != nil {
		return nil, err
	}
	out := &inferenceproviders.Anthropic{BaseURL: s.BaseURL, CACert: s.CACert}
	if out.BaseURL == "" {
		out.BaseURL = inferenceproviders.DefaultAnthropicBaseURL
	}
	var err error
	out.APIKeyKey, err = copyKey(s.APIKeySecretRef, inferenceproviders.FieldAPIKey, true)
	return out, err
}

// copySecretKey copies one key of a referenced Secret into data and returns the
// aggregate key it was stored under. An absent optional key returns "".
func (a *InferenceProviderAggregator) copySecretKey(
	ctx context.Context,
	cache map[string]*corev1.Secret,
	data map[string][]byte,
	resource string,
	ref SecretKeyRef,
	field string,
	required bool,
) (string, error) {
	if ref.Name == "" || ref.Key == "" {
		return "", rejected(reasonInvalidSpec, "a secret reference needs a name and a key")
	}
	secret, ok := cache[ref.Name]
	if !ok {
		secret = &corev1.Secret{}
		err := a.store.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: ref.Name}, secret)
		switch {
		case apierrors.IsNotFound(err):
			secret = nil
		case err != nil:
			return "", fmt.Errorf("get Secret %s/%s: %w", systemNamespace, ref.Name, err)
		}
		cache[ref.Name] = secret
	}
	if secret == nil {
		return "", rejected(reasonSecretNotFound, "Secret %q not found in %s", ref.Name, systemNamespace)
	}
	value, ok := secret.Data[ref.Key]
	if !ok || len(value) == 0 {
		if !required {
			return "", nil
		}
		return "", rejected(reasonSecretKeyNotFound, "Secret %q has no key %q", ref.Name, ref.Key)
	}
	key := inferenceproviders.SecretKey(resource, field)
	data[key] = value
	return key, nil
}

func inferenceProviderLabels(component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "orbit-operator",
		"orbit.io/component":           component,
	}
}

// upsertConfigMap writes providers.json. Unchanged content is not written.
func (a *InferenceProviderAggregator) upsertConfigMap(ctx context.Context, registry string) error {
	existing := &corev1.ConfigMap{}
	err := a.store.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: inferenceproviders.ConfigMapName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      inferenceproviders.ConfigMapName,
				Namespace: systemNamespace,
				Labels:    inferenceProviderLabels("inference-providers"),
			},
			Data: map[string]string{inferenceproviders.ConfigMapKey: registry},
		}
		if createErr := a.store.Create(ctx, cm); createErr != nil {
			return fmt.Errorf("create %s ConfigMap: %w", inferenceproviders.ConfigMapName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s ConfigMap: %w", inferenceproviders.ConfigMapName, err)
	}
	if existing.Data[inferenceproviders.ConfigMapKey] == registry {
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	existing.Data[inferenceproviders.ConfigMapKey] = registry
	if err := a.store.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s ConfigMap: %w", inferenceproviders.ConfigMapName, err)
	}
	a.logger.Info(ctx, "updated inference providers ConfigMap")
	return nil
}

// upsertSecret writes the credential values. Unchanged content is not written.
func (a *InferenceProviderAggregator) upsertSecret(ctx context.Context, data map[string][]byte) error {
	existing := &corev1.Secret{}
	err := a.store.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: inferenceproviders.SecretName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      inferenceproviders.SecretName,
				Namespace: systemNamespace,
				Labels:    inferenceProviderLabels("inference-provider-secrets"),
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if createErr := a.store.Create(ctx, s); createErr != nil {
			return fmt.Errorf("create %s Secret: %w", inferenceproviders.SecretName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s Secret: %w", inferenceproviders.SecretName, err)
	}
	if dataEqual(existing.Data, data) {
		return nil
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Data = data
	if err := a.store.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s Secret: %w", inferenceproviders.SecretName, err)
	}
	a.logger.Info(ctx, "updated inference provider secrets")
	return nil
}
