package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
// trigger's own principal may make the claim, and only about itself.
var ErrTriggerAttributionRefused = errors.New("only a trigger's own principal may attribute a run to that trigger")

// ErrTriggerAttributionIncomplete is returned when attribution names a delivery
// but no trigger. A delivery id alone is a dangling reference: nothing can be
// rendered from it, and it would quietly become a run that claims automation
// without naming what automated it.
var ErrTriggerAttributionIncomplete = errors.New("trigger attribution requires a trigger_id")

// Principal-id prefixes the triggers feature mints its SERVICE principals under
// (internal/triggers/service.go). They are the wire contract this package checks
// attribution against: orbit-agents never reads the triggers database, so the
// shape of the caller's own principal id is the only evidence available here
// without a cross-feature lookup.
const (
	// triggerPrincipalPrefix is a scheduled or manual trigger's own principal:
	// "trigger:<trigger id>". The id is the trigger's, so the claim is
	// self-verifying — it must name itself and nothing else.
	triggerPrincipalPrefix = "trigger:"
	// triggerSourcePrincipalPrefix is a webhook source's principal:
	// "trigger-source:<source id>". A source owns many triggers and the
	// binding lives in the triggers database, so the trigger id it names is
	// not derivable here.
	triggerSourcePrincipalPrefix = "trigger-source:"
)

// authorizeTriggerAttribution validates a start request's trigger attribution
// and returns what should be stored, or nil when the request claims none.
//
// It deliberately mirrors requireDelegatorPrincipal: both answer "may this
// caller assert this about the run it is starting?", and both fail closed.
//
// What this guarantees, precisely:
//
//   - A SERVICE principal named "trigger:<id>" may attribute a run to trigger
//     <id> and to no other trigger: the principal id IS the trigger id, so the
//     claim carries its own proof.
//   - A SERVICE principal named "trigger-source:<id>" may attribute a run to a
//     trigger without naming which, because a source fans one delivery out to
//     the several triggers hanging off it and the binding is only knowable in
//     the triggers database. Tenancy is still enforced: the run is created in
//     the tenant on the caller's own claims (StartRun derives tenant_id from
//     the token, never the body), so a source can only ever point at a trigger
//     id inside its own tenant.
//   - Every other principal — a person, a PAT, a session-manager or any other
//     in-mesh SERVICE workload — is refused outright.
//
// Residual, stated rather than hidden: a webhook source principal could name a
// sibling trigger id in its own tenant. Closing that needs a triggers-database
// lookup from orbit-agents, which the feature boundary deliberately forbids;
// the alternative is a trigger id stamped into the minted token's claims, which
// is the durable fix (see the ADR follow-ups).
// authorizeAttribution is the span-aware wrapper StartRun calls, mirroring
// authorizeDelegatedStart so the two gates on a start are shaped alike and each
// owns the span status for its own refusal.
func (s *RunService) authorizeAttribution(ctx context.Context, span trace.Span, req StartRunRequest) (*StartedByTrigger, error) {
	startedBy, err := authorizeTriggerAttribution(ctx, req)
	if err != nil {
		span.RecordError(err)
		// Neutral description: this covers both a refused claim and an
		// incomplete one, so it must not name either sentinel. RecordError
		// above carries which it was.
		span.SetStatus(codes.Error, "authorize trigger attribution")
		return nil, err
	}
	return startedBy, nil
}

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
	if err := triggerPrincipalMayClaim(claims.Subject, triggerID); err != nil {
		return nil, err
	}

	return &StartedByTrigger{
		TriggerID:   triggerID,
		TriggerName: name,
		DeliveryID:  deliveryID,
	}, nil
}

// triggerPrincipalMayClaim decides whether principal may attribute a run to
// triggerID. It is the whole of the check: SERVICE alone is not enough, because
// every in-mesh workload is a SERVICE principal and any of them could otherwise
// stamp a run as automation that a person can no longer tell from the real
// thing.
func triggerPrincipalMayClaim(principal, triggerID string) error {
	switch {
	case strings.HasPrefix(principal, triggerSourcePrincipalPrefix):
		// A source cannot be checked against the trigger id without reading the
		// triggers database. Tenancy is the bound that still holds.
		return nil
	case principal == triggerPrincipalPrefix+triggerID:
		return nil
	default:
		return fmt.Errorf("%w (principal %q may not claim trigger %q)", ErrTriggerAttributionRefused, principal, triggerID)
	}
}
