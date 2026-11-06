// Package events records platform events in the event outbox. Producers call
// Publish in the transaction of the state change the event describes, and the
// kora-events relay copies outbox rows into the event log.
//
// See docs/adr/2026-11-06-event-spine.md.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// OutboxChannel is notified in the producer transaction. The relay listens on it.
const OutboxChannel = "kora_events_outbox"

// Principal types recorded in koraprincipaltype.
const (
	PrincipalUser    = "USER"
	PrincipalAgent   = "AGENT"
	PrincipalTrigger = "TRIGGER"
	PrincipalSystem  = "SYSTEM"
)

var (
	// ErrNoTenant refuses an event that belongs to no tenant.
	ErrNoTenant = errors.New("event has no tenant")
	// ErrInvalidEvent refuses an event the relay could not record.
	ErrInvalidEvent = errors.New("invalid event")
)

// onceNamespace is the UUIDv5 namespace of PublishOnce ids.
var onceNamespace = uuid.MustParse("6f1d2c3a-8b4e-5f60-9a7b-0c1d2e3f4a5b")

// source is the producing binary, the name of its cmd directory.
var source = binaryName()

// Event is what a producer records. The tenant, run, agent, cause and depth
// come from the row the producer changed.
type Event struct {
	Type       Type
	Tenant     string
	Subject    string
	RunID      string
	Agent      string
	CausedBy   string
	ChainDepth int
	// Triggers lists every trigger that started or messaged the run.
	Triggers []string
	// Entity is the resource after the change, or before it for a delete.
	Entity  any
	Changed []string
	Version int64
}

// Envelope is a CloudEvents 1.0 structured event with the kora extensions.
type Envelope struct {
	SpecVersion     string   `json:"specversion"`
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	Type            Type     `json:"type"`
	Subject         string   `json:"subject"`
	Time            string   `json:"time"`
	DataContentType string   `json:"datacontenttype"`
	Tenant          string   `json:"koratenant"`
	Principal       string   `json:"koraprincipal"`
	PrincipalType   string   `json:"koraprincipaltype"`
	Delegator       string   `json:"koradelegator,omitempty"`
	Run             string   `json:"korarun,omitempty"`
	Agent           string   `json:"koraagent,omitempty"`
	CausedBy        string   `json:"koracausedby,omitempty"`
	ChainDepth      int      `json:"korachaindepth"`
	Triggers        []string `json:"koratriggers,omitempty"`
	TraceParent     string   `json:"traceparent,omitempty"`
	Data            Data     `json:"data"`
}

// Data is the event payload.
type Data struct {
	Entity        any      `json:"entity"`
	Changed       []string `json:"changed,omitempty"`
	EntityVersion int64    `json:"entity_version,omitempty"`
	OccurredAt    string   `json:"occurred_at"`
}

// Publish writes the event to event_outbox through the transaction in ctx.
// Outside a transaction it writes a standalone row.
func Publish(ctx context.Context, pool *storage.Pool, e Event) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("event id: %w", err)
	}
	return write(ctx, pool, id, e)
}

// PublishOnce writes a row whose id derives from the type and key. A repeated
// key records nothing while the earlier event is retained.
func PublishOnce(ctx context.Context, pool *storage.Pool, key string, e Event) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidEvent)
	}
	return write(ctx, pool, uuid.NewSHA1(onceNamespace, []byte(string(e.Type)+"\n"+key)), e)
}

func write(ctx context.Context, pool *storage.Pool, id uuid.UUID, e Event) error {
	env, err := newEnvelope(ctx, id, e, time.Now())
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", e.Type, err)
	}
	// The notify sends at commit, and only when the row was inserted.
	_, err = pool.Exec(ctx, `
		WITH inserted AS (
			INSERT INTO event_outbox (id, tenant_id, type, envelope)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
			RETURNING id
		)
		SELECT pg_notify($5, '') FROM inserted`,
		id, e.Tenant, string(e.Type), body, OutboxChannel)
	if err != nil {
		return fmt.Errorf("write event %s: %w", e.Type, err)
	}
	return nil
}

// newEnvelope builds the envelope of e. The principal, delegator and trace
// come from ctx. A context with no claims records SYSTEM and the binary.
func newEnvelope(ctx context.Context, id uuid.UUID, e Event, at time.Time) (Envelope, error) {
	if err := validate(e); err != nil {
		return Envelope{}, err
	}
	principal, principalType := source, PrincipalSystem
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil {
		principal, principalType = claims.Subject, principalTypeOf(claims.Kora.PrincipalType)
	}
	var delegator string
	if d := policy.DelegationFromContext(ctx); d != nil {
		delegator = d.Sub
	}
	when := at.UTC().Format(time.RFC3339Nano)
	return Envelope{
		SpecVersion:     "1.0",
		ID:              id.String(),
		Source:          source,
		Type:            e.Type,
		Subject:         e.Subject,
		Time:            when,
		DataContentType: "application/json",
		Tenant:          e.Tenant,
		Principal:       principal,
		PrincipalType:   principalType,
		Delegator:       delegator,
		Run:             e.RunID,
		Agent:           e.Agent,
		CausedBy:        e.CausedBy,
		ChainDepth:      e.ChainDepth,
		Triggers:        e.Triggers,
		TraceParent:     observability.TraceParent(ctx),
		Data:            Data{Entity: e.Entity, Changed: e.Changed, EntityVersion: e.Version, OccurredAt: when},
	}, nil
}

// validate refuses what the relay could not copy into the log's typed columns.
func validate(e Event) error {
	switch {
	case e.Tenant == "":
		return ErrNoTenant
	case !e.Type.Known():
		return fmt.Errorf("%w: unknown type %q", ErrInvalidEvent, e.Type)
	case e.Subject == "":
		return fmt.Errorf("%w: %s has no subject", ErrInvalidEvent, e.Type)
	case e.ChainDepth < 0:
		return fmt.Errorf("%w: negative chain depth", ErrInvalidEvent)
	}
	for _, v := range []string{e.RunID, e.CausedBy} {
		if _, err := uuid.Parse(v); v != "" && err != nil {
			return fmt.Errorf("%w: %q is not a UUID", ErrInvalidEvent, v)
		}
	}
	return nil
}

// principalTypeOf maps a token principal type to the event vocabulary. A PAT
// acts as its user, and a SERVICE principal is a trigger.
func principalTypeOf(t token.PrincipalType) string {
	switch t {
	case token.PrincipalTypeHuman, token.PrincipalTypePAT:
		return PrincipalUser
	case token.PrincipalTypeAgent:
		return PrincipalAgent
	case token.PrincipalTypeService:
		return PrincipalTrigger
	default:
		return PrincipalSystem
	}
}

func binaryName() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Path != "" {
		return path.Base(info.Path)
	}
	return "unknown"
}
