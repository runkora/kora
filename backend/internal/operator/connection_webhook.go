package operator

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

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
