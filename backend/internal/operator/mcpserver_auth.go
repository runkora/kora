// Static credentials for MCP servers: spec.auth validation, and the aggregated
// kora-system Secret the tool-gateway reads them from.
// See docs/adr/2026-10-22-mcp-server-static-credentials.md.
package operator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
)

// aggregatedMCPServerSecretsName is the kora-system Secret holding every
// MCPServer credential, keyed by mcpSecretKey.
const aggregatedMCPServerSecretsName = "kora-mcp-server-secrets"

var (
	// ErrMCPServerAuthWithForward is returned when spec.auth and
	// callerIdentity: forward are both set. Both would set Authorization.
	ErrMCPServerAuthWithForward = errors.New("spec.auth cannot be combined with callerIdentity: forward")
	// ErrMCPServerBadAuthHeader is returned for an invalid spec.auth header.
	ErrMCPServerBadAuthHeader = errors.New("invalid spec.auth header")
	// ErrMCPCredentialUnavailable is returned when a referenced Secret value
	// cannot be used. The server stays registered and its calls fail closed.
	ErrMCPCredentialUnavailable = errors.New("credential unavailable")
)

// validateMCPServerAuth checks spec.auth. Errors carry header names only.
func validateMCPServerAuth(spec MCPServerSpec) error {
	if spec.Auth == nil {
		return nil
	}
	if spec.CallerIdentity == CallerIdentityForward {
		return ErrMCPServerAuthWithForward
	}
	if n := len(spec.Auth.Headers); n == 0 || n > platformconns.MaxAPIKeyHeaders {
		return fmt.Errorf("%w: spec.auth.headers must list 1 to %d headers", ErrMCPServerBadAuthHeader, platformconns.MaxAPIKeyHeaders)
	}
	seen := make(map[string]bool, len(spec.Auth.Headers))
	for _, h := range spec.Auth.Headers {
		lower := strings.ToLower(h.Name)
		switch {
		case len(h.Name) > platformconns.MaxHeaderNameLength:
			return fmt.Errorf("%w: a header name is over %d characters", ErrMCPServerBadAuthHeader, platformconns.MaxHeaderNameLength)
		case !platformconns.IsHeaderName(h.Name):
			return fmt.Errorf("%w: %q must use only letters, digits and hyphens", ErrMCPServerBadAuthHeader, h.Name)
		case platformconns.IsReservedHeader(h.Name):
			return fmt.Errorf("%w: %q is reserved", ErrMCPServerBadAuthHeader, h.Name)
		case seen[lower]:
			return fmt.Errorf("%w: %q is listed twice", ErrMCPServerBadAuthHeader, h.Name)
		case len(h.Prefix) > platformconns.MaxHeaderPrefixLength:
			return fmt.Errorf("%w: the prefix of %q is over %d characters", ErrMCPServerBadAuthHeader, h.Name, platformconns.MaxHeaderPrefixLength)
		case !platformconns.IsHeaderValue([]byte(h.Prefix)):
			return fmt.Errorf("%w: the prefix of %q contains a control character", ErrMCPServerBadAuthHeader, h.Name)
		case len(validation.IsDNS1123Subdomain(h.SecretRef.Name)) > 0 || len(validation.IsConfigMapKey(h.SecretRef.Key)) > 0:
			return fmt.Errorf("%w: %q needs a valid secretRef.name and secretRef.key", ErrMCPServerBadAuthHeader, h.Name)
		}
		seen[lower] = true
	}
	return nil
}

// mcpSecretKey is the aggregated Secret's data key for one header. Keying on
// the header name keeps a reordered list from sending a value under another
// header. Namespaces and header names have no dots, and names have no
// underscores, so the key is unique.
func mcpSecretKey(namespace, name, header string) string {
	return namespace + "." + strings.ReplaceAll(name, ".", "_") + "." + strings.ToLower(header)
}

// maxMCPCredentialSize bounds one value. Every value shares one Secret, which
// Kubernetes caps at 1 MiB.
const maxMCPCredentialSize = 4 << 10

// readMCPCredential returns the value for ref in namespace. An unusable value
// wraps ErrMCPCredentialUnavailable. No error carries the value.
func readMCPCredential(ctx context.Context, c client.Reader, namespace string, ref MCPServerSecretRef) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: Secret %q not found", ErrMCPCredentialUnavailable, ref.Name)
		}
		return nil, fmt.Errorf("read Secret %q: %w", ref.Name, err)
	}
	// Operator-managed Secrets, such as the copied registry pull secret, hold
	// platform credentials a tenant must not send to its own endpoint.
	if secret.Labels["app.kubernetes.io/managed-by"] == "kora-operator" {
		return nil, fmt.Errorf("%w: Secret %q is managed by the operator", ErrMCPCredentialUnavailable, ref.Name)
	}
	v, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("%w: Secret %q has no key %q", ErrMCPCredentialUnavailable, ref.Name, ref.Key)
	}
	// Values written with echo or a heredoc end in a newline.
	v = bytes.TrimSuffix(bytes.TrimSuffix(v, []byte("\n")), []byte("\r"))
	if len(v) == 0 {
		return nil, fmt.Errorf("%w: Secret %q key %q is empty", ErrMCPCredentialUnavailable, ref.Name, ref.Key)
	}
	if len(v) > maxMCPCredentialSize {
		return nil, fmt.Errorf("%w: Secret %q key %q is over %d bytes", ErrMCPCredentialUnavailable, ref.Name, ref.Key, maxMCPCredentialSize)
	}
	if !platformconns.IsHeaderValue(v) {
		return nil, fmt.Errorf("%w: Secret %q key %q contains a control character", ErrMCPCredentialUnavailable, ref.Name, ref.Key)
	}
	return v, nil
}

// mcpCredentialStatus returns the first unavailable credential of srv, or nil.
func mcpCredentialStatus(ctx context.Context, c client.Reader, srv *MCPServer) error {
	if srv.Spec.Auth == nil {
		return nil
	}
	for _, h := range srv.Spec.Auth.Headers {
		if _, err := readMCPCredential(ctx, c, srv.Namespace, h.SecretRef); err != nil {
			return fmt.Errorf("header %q: %w", h.Name, err)
		}
	}
	return nil
}

// upsertMCPServerSecret creates or replaces the aggregated credentials Secret.
func (a *MCPServerAggregator) upsertMCPServerSecret(ctx context.Context, data map[string][]byte) error {
	existing := &corev1.Secret{}
	err := a.client.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: aggregatedMCPServerSecretsName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggregatedMCPServerSecretsName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kora-operator",
					"runkora.io/component":         "mcp-server-secrets",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if createErr := a.client.Create(ctx, s); createErr != nil {
			return fmt.Errorf("create %s Secret: %w", aggregatedMCPServerSecretsName, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s Secret: %w", aggregatedMCPServerSecretsName, err)
	case dataEqual(existing.Data, data):
		return nil
	}
	// Two controllers share this aggregator. The lock turns a stale writer into
	// a conflict and requeue.
	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	existing.Data = data
	if patchErr := a.client.Patch(ctx, existing, patch); patchErr != nil {
		return fmt.Errorf("patch %s Secret: %w", aggregatedMCPServerSecretsName, patchErr)
	}
	return nil
}
