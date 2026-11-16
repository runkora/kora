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
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
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
	// ErrPayloadType refuses an entity that is not the type's payload struct.
	ErrPayloadType = errors.New("entity is not the event type's payload struct")
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
	// Entity is the type's payload struct: the resource after the change, or
	// before it for a delete.
	Entity  any
	Changed []string
	Version int64
}

// Envelope is a CloudEvents 1.0 structured event with the kora extensions.
// The doc and enum tags describe it in the published schema.
type Envelope struct {
	SpecVersion     string   `json:"specversion" doc:"The CloudEvents version." enum:"cloudevents_version"`
	ID              string   `json:"id" doc:"The event id."`
	Source          string   `json:"source" doc:"The binary that recorded the event."`
	Type            Type     `json:"type" doc:"The event type."`
	DataSchema      string   `json:"dataschema,omitempty" doc:"The URL of the type's schema."`
	Subject         string   `json:"subject" doc:"The resource the event is about."`
	Time            string   `json:"time" doc:"When the event was recorded." format:"date-time"`
	DataContentType string   `json:"datacontenttype" doc:"The media type of data." enum:"datacontenttype_value"`
	Tenant          string   `json:"koratenant" doc:"The tenant the event belongs to."`
	Principal       string   `json:"koraprincipal" doc:"The principal that caused the event."`
	PrincipalType   string   `json:"koraprincipaltype" doc:"The type of the principal." enum:"event_principal_type"`
	PrincipalName   string   `json:"koraprincipalname" doc:"The principal's display name."`
	Delegator       string   `json:"koradelegator,omitempty" doc:"The person an agent acted for."`
	Run             string   `json:"korarun,omitempty" doc:"The run the event concerns."`
	ActingRun       string   `json:"koraactingrun,omitempty" doc:"The run an agent acted from when it caused the event."`
	Agent           string   `json:"koraagent,omitempty" doc:"The slug of the agent the event concerns."`
	CausedBy        string   `json:"koracausedby,omitempty" doc:"The event that led to this one."`
	ChainDepth      int      `json:"korachaindepth" doc:"How many trigger-caused steps led to the event."`
	Triggers        []string `json:"koratriggers,omitempty" doc:"The triggers that started or messaged the run."`
	TraceParent     string   `json:"traceparent,omitempty" doc:"The W3C trace the event belongs to."`
	Data            Data     `json:"data" doc:"The event payload."`
}

// Data is the event payload.
type Data struct {
	Entity        any      `json:"entity" doc:"The resource after the change, or before it for a delete."`
	Changed       []string `json:"changed,omitempty" doc:"The fields an update changed."`
	EntityVersion int64    `json:"entity_version,omitempty" doc:"The resource version after the change."`
	OccurredAt    string   `json:"occurred_at" doc:"When the change happened." format:"date-time"`
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
	principal, principalType, principalName := source, PrincipalSystem, source
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil {
		principal, principalType, principalName = claims.Subject, PrincipalTypeOf(claims), PrincipalNameOf(claims)
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
		PrincipalName:   principalName,
		Delegator:       delegator,
		Run:             e.RunID,
		ActingRun:       actingRun(ctx),
		Agent:           e.Agent,
		CausedBy:        e.CausedBy,
		ChainDepth:      e.ChainDepth,
		Triggers:        e.Triggers,
		TraceParent:     observability.TraceParent(ctx),
		Data:            Data{Entity: e.Entity, Changed: e.Changed, EntityVersion: e.Version, OccurredAt: when},
	}, nil
}

// actingRun is the run the agent caller acts for. A value that is not a run id
// is left out, so a bad run prefix cannot fail the producer's transaction.
func actingRun(ctx context.Context) string {
	id := mw.ActingRunID(ctx)
	if _, err := uuid.Parse(id); err != nil {
		return ""
	}
	return id
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
	case reflect.TypeOf(e.Entity) != catalog[e.Type].payload:
		return fmt.Errorf("%w: %s takes %s, got %T", ErrPayloadType, e.Type, catalog[e.Type].payload.Name(), e.Entity)
	}
	for _, v := range []string{e.RunID, e.CausedBy} {
		if _, err := uuid.Parse(v); v != "" && err != nil {
			return fmt.Errorf("%w: %q is not a UUID", ErrInvalidEvent, v)
		}
	}
	return nil
}

// PrincipalTypeOf maps a token principal to the event vocabulary. A PAT acts
// as its user. A SERVICE principal is a trigger when its subject is a trigger
// or webhook source principal, and the platform otherwise.
func PrincipalTypeOf(claims *token.PlatformClaims) string {
	switch claims.Kora.PrincipalType {
	case token.PrincipalTypeHuman, token.PrincipalTypePAT:
		return PrincipalUser
	case token.PrincipalTypeAgent:
		return PrincipalAgent
	case token.PrincipalTypeService:
		if strings.HasPrefix(claims.Subject, "trigger:") || strings.HasPrefix(claims.Subject, "trigger-source:") {
			return PrincipalTrigger
		}
		return PrincipalSystem
	default:
		return PrincipalSystem
	}
}

// PrincipalNameOf is the display name the token carries: a person's name, an
// agent's or trigger's slug, or a platform component. A person with no name
// falls back to their email, and any other principal to its subject.
func PrincipalNameOf(claims *token.PlatformClaims) string {
	switch {
	case claims.Kora.DisplayName != "":
		return claims.Kora.DisplayName
	case claims.Kora.Claims["email"] != "":
		return claims.Kora.Claims["email"]
	default:
		return claims.Subject
	}
}

func binaryName() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Path != "" {
		return path.Base(info.Path)
	}
	return "unknown"
}
