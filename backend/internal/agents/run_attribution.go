package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/platform/token"
)

// Trigger attribution: the run half of the trigger↔run backlink
// (docs/adr/2026-06-20-extended-trigger-types.md, US-004).
//
// A trigger-started run executes under the trigger's own SERVICE principal, so
// created_by names that principal and nothing else — an opaque id to a person
// reading the run. The attribution carried here is what turns that into "started
// by trigger Nightly review, delivery abc123".

// ErrTriggerAttributionRefused is returned when a caller supplies trigger
// attribution it is not entitled to claim.
//
// The run's own record of why it exists has to be trustworthy: a person who
// could label their run as trigger-started could manufacture evidence that an
// automated policy-governed path did something they did it themselves. Only a
// SERVICE principal — which is what a trigger runs as — may make the claim.
var ErrTriggerAttributionRefused = errors.New("only a SERVICE principal may attribute a run to a trigger")

// ErrTriggerAttributionIncomplete is returned when attribution names a delivery
// but no trigger. A delivery id alone is a dangling reference: nothing can be
// rendered from it, and it would quietly become a run that claims automation
// without naming what automated it.
var ErrTriggerAttributionIncomplete = errors.New("trigger attribution requires a trigger_id")

// authorizeTriggerAttribution validates a start request's trigger attribution
// and returns what should be stored, or nil when the request claims none.
//
// It deliberately mirrors requireDelegatorPrincipal: both answer "may this
// caller assert this about the run it is starting?", and both fail closed.
func authorizeTriggerAttribution(ctx context.Context, req StartRunRequest) (*StartedByTrigger, error) {
	triggerID := strings.TrimSpace(req.TriggerID)
	deliveryID := strings.TrimSpace(req.DeliveryID)
	name := strings.TrimSpace(req.TriggerName)

	if triggerID == "" {
		if deliveryID != "" || name != "" {
			return nil, ErrTriggerAttributionIncomplete
		}
		// The ordinary case: a person started this run.
		return nil, nil
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil, ErrTriggerAttributionRefused
	}
	if claims.Orbit.PrincipalType != token.PrincipalTypeService {
		return nil, fmt.Errorf("%w (principal type %s)", ErrTriggerAttributionRefused, claims.Orbit.PrincipalType)
	}

	return &StartedByTrigger{
		TriggerID:   triggerID,
		TriggerName: name,
		DeliveryID:  deliveryID,
	}, nil
}
