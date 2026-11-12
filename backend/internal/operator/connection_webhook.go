package operator

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/runkora/kora/backend/internal/credentials/adapters/apikey"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
)

// ConnectionValidator rejects a Connection the egress gateway could not tell
// apart from another Connection in the same namespace. An update is rejected
// only for a conflict its previous version did not have. See
// docs/adr/2026-10-15-same-host-connection-selection.md.
type ConnectionValidator struct {
	// Client is the manager's cached client. TenantReconciler watches every
	// Connection, so listing reads the informer cache.
	Client  client.Reader
	Decoder admission.Decoder
}

// Handle is called for every Connection CREATE and UPDATE in a tenant namespace.
func (v *ConnectionValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	conn := &Connection{}
	if err := v.Decoder.Decode(req, conn); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := validateAPIKeyConfig(conn); err != nil {
		return admission.Denied(fmt.Sprintf("connection %q: %v", cmp.Or(conn.Name, conn.GenerateName), err))
	}
	list := &ConnectionList{}
	if err := v.Client.List(ctx, list, client.InNamespace(req.Namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("list connections: %w", err))
	}
	var old *Connection
	if req.Operation == admissionv1.Update {
		old = &Connection{}
		if err := v.Decoder.DecodeRaw(req.OldObject, old); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name == conn.Name || !connectionsConflict(conn, other) {
			continue
		}
		if old != nil && connectionsConflict(old, other) {
			continue
		}
		return admission.Denied(fmt.Sprintf(
			"connection %q conflicts with existing connection %q: the egress gateway cannot tell them apart. github_app connections on one host must each declare a different config.owner, and other connections must use different base hosts.",
			cmp.Or(conn.Name, conn.GenerateName), other.Name))
	}
	return admission.Allowed("")
}

// connectionsConflict reports whether the egress gateway cannot tell a and b apart.
func connectionsConflict(a, b *Connection) bool {
	return platformconns.Conflict(connectionIdentity(a), connectionIdentity(b))
}

// connectionIdentity is what the conflict rule compares for one Connection.
func connectionIdentity(c *Connection) platformconns.Identity {
	return platformconns.Identity{
		Adapter:  c.Spec.Adapter,
		BaseHost: c.Spec.BaseHost,
		Owner:    platformconns.ConfigOwner(c.Spec.Config.Raw),
	}
}

// createdBefore orders connections by creation timestamp, then by name. The
// aggregation backstop keeps the earlier of a conflicting pair.
func createdBefore(a, b *Connection) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return a.Name < b.Name
}

// validateAPIKeyConfig checks the config of an apikey Connection: the headers
// list and the other keys through the adapter's own validator (so admission and
// the gateway agree), and config.placeholderEnv. Config is a RawExtension with
// preserve-unknown-fields, so CEL in the CRD cannot read it; the webhook does.
// Other adapters are not checked here.
func validateAPIKeyConfig(c *Connection) error {
	if c.Spec.Adapter != platformconns.AdapterAPIKey || len(c.Spec.Config.Raw) == 0 {
		return nil
	}
	if err := apikey.ValidateConfig(c.Spec.Config.Raw); err != nil {
		return fmt.Errorf("apikey connections: invalid config: %w", err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(c.Spec.Config.Raw, &cfg); err != nil {
		return nil
	}
	raw, ok := cfg["placeholderEnv"]
	if !ok {
		return nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err != nil || !platformconns.ValidPlaceholderEnvName(name) {
		return fmt.Errorf("apikey connections: config.placeholderEnv must be a plain environment variable name (letters, digits, underscore; at most 128 characters; not PATH, HOME, LD_*, DYLD_*, KORA_* or another reserved name)")
	}
	return nil
}
