package declarative

import (
	"context"
	"errors"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

var (
	// ErrManagedByResource refuses an API write to an item projected from a resource.
	ErrManagedByResource = errors.New("this item is managed by a declarative resource; change the resource instead")
	// ErrAPIAuthoringDisabled refuses an API write while authoring through the API is off.
	ErrAPIAuthoringDisabled = errors.New("authoring through the API is disabled; change the declarative resources instead")
)

// WriteRefusal answers err with 409 managed_by_resource or 403
// api_authoring_disabled when it wraps one of the sentinels. It reports
// whether it wrote a response.
func WriteRefusal(ctx context.Context, w http.ResponseWriter, logger logging.Logger, err error) bool {
	switch {
	case errors.Is(err, ErrManagedByResource):
		api.ErrorResponse(ctx, w, logger, api.ConflictError(api.ErrCodeManagedByResource, ErrManagedByResource.Error(), nil), nil)
	case errors.Is(err, ErrAPIAuthoringDisabled):
		api.ErrorResponse(ctx, w, logger, api.ForbiddenError(api.ErrCodeAPIAuthoringDisabled, ErrAPIAuthoringDisabled.Error(), nil), nil)
	default:
		return false
	}
	return true
}
