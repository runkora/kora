package agents

import (
	"context"
	"errors"
)

// Source says where an agent type or environment is authored.
type Source string

const (
	// SourceAPI is an item created through the API or the dashboard.
	SourceAPI Source = "api"
	// SourceCRD is an item projected from a Kubernetes resource.
	SourceCRD Source = "crd"
)

// ErrManagedByResource is returned when an API write targets an item a resource owns. Handlers map it to a 409.
var ErrManagedByResource = errors.New("this item is managed by a declarative resource; change the resource instead")

// ErrAPIAuthoringDisabled is returned when an API write targets a tenant locked to its resources. Handlers map it to a 403.
var ErrAPIAuthoringDisabled = errors.New("API authoring is disabled for this tenant; change its declarative resources instead")

// authoringSwitch reports whether a tenant accepts API writes.
type authoringSwitch interface {
	APIAuthoringEnabled(tenantID string) bool
}

type declaredWriteKey struct{}

// declaredWrite is a write an Applier makes for a resource the operator sent. It has no caller.
type declaredWrite struct {
	tenantID    string
	createdBy   string
	resourceUID string
	generation  int64
}

// withDeclaredWrite marks ctx as a declared write. The service skips the caller checks, takes the
// tenant from tenantID and records the resource's UID and generation on the row.
func withDeclaredWrite(ctx context.Context, tenantID, namespace, name, uid string, generation int64) context.Context {
	return context.WithValue(ctx, declaredWriteKey{}, declaredWrite{
		tenantID:    tenantID,
		createdBy:   "resource:" + namespace + "/" + name,
		resourceUID: uid,
		generation:  generation,
	})
}

func declaredFromContext(ctx context.Context) (declaredWrite, bool) {
	d, ok := ctx.Value(declaredWriteKey{}).(declaredWrite)
	return d, ok
}

// refuseAPIWrite refuses an API write in a locked tenant, or to an item a resource owns.
// A declared write passes.
func refuseAPIWrite(ctx context.Context, sw authoringSwitch, tenantID string, source Source) error {
	if _, ok := declaredFromContext(ctx); ok {
		return nil
	}
	if sw != nil && !sw.APIAuthoringEnabled(tenantID) {
		return ErrAPIAuthoringDisabled
	}
	if source == SourceCRD {
		return ErrManagedByResource
	}
	return nil
}

// sourceFor is the source, resource UID and resource generation a new row takes.
func sourceFor(ctx context.Context) (Source, string, int64) {
	if d, ok := declaredFromContext(ctx); ok {
		return SourceCRD, d.resourceUID, d.generation
	}
	return SourceAPI, "", 0
}

// sourceOrAPI reads an unset source as SourceAPI.
func sourceOrAPI(s Source) Source {
	if s == "" {
		return SourceAPI
	}
	return s
}
