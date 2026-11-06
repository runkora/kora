// Package koraevents is the kora-events service: the relay from event_outbox
// into the event log, the read API, the event streams and retention.
//
// See docs/adr/2026-11-06-event-spine.md.
package koraevents

import (
	"encoding/json"
	"errors"
	"time"
)

// LogChannel is notified with the last seq after each relayed batch.
const LogChannel = "kora_events"

// ErrNotFound reports an event that does not exist in the caller's tenant.
var ErrNotFound = errors.New("event not found")

// Record is one event in the log.
type Record struct {
	Seq        int64           `json:"seq"`
	RecordedAt time.Time       `json:"recorded_at"`
	Event      json.RawMessage `json:"event"`

	tenant    string
	category  string
	eventType string
	agent     string
	principal string
	runID     string
}

// Filter selects events. An empty field matches every event. An empty
// tenant matches every tenant and is used only by the internal stream.
type Filter struct {
	Tenant    string
	Category  string
	Type      string
	Agent     string
	Principal string
	RunID     string
}

func (f Filter) matches(r Record) bool {
	return (f.Tenant == "" || f.Tenant == r.tenant) &&
		(f.Category == "" || f.Category == r.category) &&
		(f.Type == "" || f.Type == r.eventType) &&
		(f.Agent == "" || f.Agent == r.agent) &&
		(f.Principal == "" || f.Principal == r.principal) &&
		(f.RunID == "" || f.RunID == r.runID)
}

// Bound limits a page by seq or by occurred time. Zero values are unset.
type Bound struct {
	Seq  int64
	Time time.Time
}

// ListQuery is one page of the list API, newest first.
type ListQuery struct {
	Filter Filter
	Before Bound
	After  Bound
	Limit  int
}

// Page is a page of events and the cursor of the next one.
type Page struct {
	Events     []Record `json:"events"`
	NextCursor *string  `json:"next_cursor"`
}
