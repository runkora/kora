package agents

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// RunEventSummary describes the event behind a trigger's message: who did what
// to which object, and a preview of that object. See docs/adr/2026-11-11-run-page-record.md.
type RunEventSummary struct {
	Actor   RunEventActor    `json:"actor"`
	Verb    string           `json:"verb"`
	Object  RunEventObject   `json:"object"`
	Quote   string           `json:"quote,omitempty"`
	Preview *RunEventPreview `json:"preview,omitempty"`
}

// RunEventActor is who caused the event. Kind is person, bot or system.
type RunEventActor struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
}

// RunEventObject is what the event acted on.
type RunEventObject struct {
	Label string `json:"label"`
	URL   string `json:"url,omitempty"`
}

// RunEventPreview is the object as it was when the event arrived.
type RunEventPreview struct {
	Title   string          `json:"title"`
	Facts   []string        `json:"facts,omitempty"`
	Added   *int            `json:"added,omitempty"`
	Removed *int            `json:"removed,omitempty"`
	Status  *RunEventStatus `json:"status,omitempty"`
}

// RunEventStatus is the object's state. Tone is ok or bad.
type RunEventStatus struct {
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

var (
	// ErrRunEventRefused is returned when a caller that is not a SERVICE principal sends an event summary.
	ErrRunEventRefused = errors.New("only a service principal may record an event summary")
	// ErrRunEventInvalid is returned when an event summary is malformed or over a limit.
	ErrRunEventInvalid = errors.New("invalid event summary")
)

// authorizeRunEvent validates the event summary of a start or join request. A
// summary is an audit record of why a run took a turn. Only a SERVICE
// principal may write it, the same rule as run sources.
func authorizeRunEvent(ctx context.Context, e *RunEventSummary) error {
	if e == nil {
		return nil
	}
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeService {
		return ErrRunEventRefused
	}
	for _, check := range []func(*RunEventSummary) error{checkRunEventShape, checkRunEventLengths, checkRunEventLinks} {
		if err := check(e); err != nil {
			return fmt.Errorf("%w: %w", ErrRunEventInvalid, err)
		}
	}
	return nil
}

// checkRunEventShape checks the required fields and the closed vocabularies.
func checkRunEventShape(e *RunEventSummary) error {
	switch e.Actor.Kind {
	case "person", "bot", "system":
	default:
		return errors.New("actor kind must be person, bot or system")
	}
	if e.Actor.Name == "" || e.Verb == "" || e.Object.Label == "" {
		return errors.New("actor name, verb and object label are required")
	}
	if p := e.Preview; p != nil && p.Status != nil && p.Status.Tone != "ok" && p.Status.Tone != "bad" {
		return errors.New("status tone must be ok or bad")
	}
	return nil
}

// checkRunEventLengths bounds every string and the number of facts.
func checkRunEventLengths(e *RunEventSummary) error {
	strs := []string{e.Actor.Name, e.Actor.URL, e.Verb, e.Object.Label, e.Object.URL}
	if p := e.Preview; p != nil {
		if len(p.Facts) > agentsclient.MaxEventSummaryFacts {
			return fmt.Errorf("at most %d facts", agentsclient.MaxEventSummaryFacts)
		}
		strs = append(append(strs, p.Title), p.Facts...)
		if p.Status != nil {
			strs = append(strs, p.Status.Label)
		}
	}
	for _, s := range strs {
		if utf8.RuneCountInString(s) > agentsclient.MaxEventSummaryStringRunes {
			return fmt.Errorf("strings are at most %d characters", agentsclient.MaxEventSummaryStringRunes)
		}
	}
	if utf8.RuneCountInString(e.Quote) > agentsclient.MaxEventSummaryQuoteRunes {
		return fmt.Errorf("quote is at most %d characters", agentsclient.MaxEventSummaryQuoteRunes)
	}
	return nil
}

// checkRunEventLinks refuses a link that is not an absolute http or https URL.
func checkRunEventLinks(e *RunEventSummary) error {
	for _, link := range []string{e.Actor.URL, e.Object.URL} {
		if link == "" {
			continue
		}
		if !agentsclient.ValidEventLink(link) {
			return errors.New("links must be absolute http or https urls")
		}
	}
	return nil
}
