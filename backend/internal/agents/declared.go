package agents

import (
	"context"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

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
// tenant from the item and records the resource's UID and generation on the row.
func withDeclaredWrite(ctx context.Context, item declarative.Item) context.Context {
	return context.WithValue(ctx, declaredWriteKey{}, declaredWrite{
		tenantID:    item.TenantID,
		createdBy:   "resource:" + item.Namespace + "/" + item.Name,
		resourceUID: item.UID,
		generation:  item.Generation,
	})
}

func declaredFromContext(ctx context.Context) (declaredWrite, bool) {
	d, ok := ctx.Value(declaredWriteKey{}).(declaredWrite)
	return d, ok
}

// refuseAPIWrite refuses an API write in a locked tenant, or to an item a resource owns.
// A declared write passes.
func refuseAPIWrite(ctx context.Context, sw authoringSwitch, tenantID, source string) error {
	if _, ok := declaredFromContext(ctx); ok {
		return nil
	}
	if !sw.APIAuthoringEnabled(tenantID) {
		return declarative.ErrAPIAuthoringDisabled
	}
	if source == declarative.SourceCRD {
		return declarative.ErrManagedByResource
	}
	return nil
}

// sourceFor is the source, resource UID and resource generation a new row takes.
func sourceFor(ctx context.Context) (string, string, int64) {
	if d, ok := declaredFromContext(ctx); ok {
		return declarative.SourceCRD, d.resourceUID, d.generation
	}
	return declarative.SourceAPI, "", 0
}
