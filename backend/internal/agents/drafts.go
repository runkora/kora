package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ── Agent type drafts ─────────────────────────────────────────────────────────

// ErrDraftNotFound is returned when a draft does not exist — or exists but
// belongs to another caller. Ownership mismatches deliberately surface as
// not-found rather than forbidden to avoid leaking draft existence.
var ErrDraftNotFound = errors.New("draft not found")

// ErrDraftPayloadInvalid is returned when a draft payload is not valid JSON.
// Handlers map it to a 400.
var ErrDraftPayloadInvalid = errors.New("draft payload is not valid JSON")

// ErrDraftPayloadTooLarge is returned when a draft payload exceeds
// maxDraftPayloadBytes. Handlers map it to a 400.
var ErrDraftPayloadTooLarge = errors.New("draft payload too large")

// errNoVerifiedClaims is the static span-status sentinel for requests whose
// context carries no verified tenant + subject.
var errNoVerifiedClaims = errors.New("no verified claims in context")

// maxDraftPayloadBytes caps a draft payload at 64KB. A draft is a
// partially-filled CreateAgentTypeRequest; anything larger is not a form.
const maxDraftPayloadBytes = 64 * 1024

// AgentTypeDraft is a saved, unfinished agent-builder form. It is NOT an agent
// type: it has no state, no slug uniqueness, and — structurally — can never
// hold a grant or reach Kubernetes, because grant reconciliation reads
// agent_types and a draft is not there. Payload is a partially-filled
// CreateAgentTypeRequest stored opaquely; a draft has no invariants by design.
// See docs/adr/2026-07-17-agent-builder.md §2.
type AgentTypeDraft struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	CreatedBy string          `json:"created_by"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ── Request types ─────────────────────────────────────────────────────────────

// CreateDraftRequest is the create payload. Tenant and creator are derived
// from the verified claims, never the body — a draft is personal.
type CreateDraftRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// UpdateDraftRequest fully replaces the draft payload — the builder always
// submits the complete form state.
type UpdateDraftRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// ── Service ───────────────────────────────────────────────────────────────────

// DraftServiceConfig holds runtime parameters for DraftService.
type DraftServiceConfig struct {
	// ReaperInterval controls how often abandoned drafts are swept.
	// Defaults to 1 hour if zero.
	ReaperInterval time.Duration
	// Retention is how long an untouched draft survives before the reaper
	// deletes it. Defaults to 30 days if zero.
	Retention time.Duration
}

// DraftService manages agent type draft CRUD. Every operation is scoped to
// the caller's verified tenant AND subject: a draft is unfinished personal
// work, not a shared artifact. Payloads are validated only as JSON within the
// size cap and otherwise stored opaquely — a draft has no invariants. Writes
// additionally require create authority for at least one identity mode
// (checkAnyAgentTypeWrite): the draft routes are authenticated-only at the
// route layer so a delegated-only creator can still use the builder.
type DraftService struct {
	repo   DraftRepository
	authz  *Authz
	cfg    DraftServiceConfig
	logger logging.Logger
}

// NewDraftService constructs a DraftService.
func NewDraftService(repo DraftRepository, authz *Authz, cfg DraftServiceConfig, logger logging.Logger) *DraftService {
	return &DraftService{repo: repo, authz: authz, cfg: cfg, logger: logger}
}

// callerFromContext returns the verified tenant and subject from the platform
// claims in ctx. Both must be present: a draft's owner columns come from the
// token, never from the request body.
func callerFromContext(ctx context.Context) (tenantID, createdBy string, err error) {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims.Kora.TenantID == "" || claims.Subject == "" {
		return "", "", errNoVerifiedClaims
	}
	return claims.Kora.TenantID, claims.Subject, nil
}

// normalizeDraftPayload validates a draft payload: valid JSON, at most
// maxDraftPayloadBytes. A nil/empty payload normalizes to {} — the builder
// creates the draft on the user's first change, which may be empty. No field
// validation happens here: a draft has no invariants by design.
func normalizeDraftPayload(payload json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(payload) > maxDraftPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds the %d byte cap", ErrDraftPayloadTooLarge, len(payload), maxDraftPayloadBytes)
	}
	if !json.Valid(payload) {
		return nil, ErrDraftPayloadInvalid
	}
	return payload, nil
}

// CreateDraft saves a new draft for the caller. Tenant and creator come from
// the verified claims in ctx; the request body cannot set them.
func (s *DraftService) CreateDraft(ctx context.Context, req CreateDraftRequest) (*AgentTypeDraft, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.CreateDraft")
	defer span.End()

	tenantID, createdBy, err := callerFromContext(ctx)
	if err != nil {
		span.SetStatus(codes.Error, errNoVerifiedClaims.Error())
		return nil, err
	}
	// Drafting requires create authority for at least one identity mode. The
	// draft route is authenticated-only at the route layer so a delegated-only
	// creator can use the builder; this is the authoritative check.
	if err = s.authz.checkAnyAgentTypeWrite(ctx); err != nil {
		return nil, err
	}
	payload, err := normalizeDraftPayload(req.Payload)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	draft := AgentTypeDraft{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		CreatedBy: createdBy,
		Payload:   payload,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.CreateDraft(ctx, draft); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create draft")
		return nil, fmt.Errorf("create draft: %w", err)
	}
	return &draft, nil
}

// GetDraft returns a draft by ID. A draft owned by another tenant or another
// user surfaces as ErrDraftNotFound, never forbidden.
func (s *DraftService) GetDraft(ctx context.Context, id string) (*AgentTypeDraft, error) {
	return s.ownedDraft(ctx, id)
}

// ListDrafts returns the caller's drafts, most recently touched first. Drafts
// are personal: listing filters to the verified tenant AND subject.
func (s *DraftService) ListDrafts(ctx context.Context) ([]AgentTypeDraft, error) {
	tenantID, createdBy, err := callerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	drafts, err := s.repo.ListDrafts(ctx, tenantID, createdBy)
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	return drafts, nil
}

// UpdateDraft fully replaces a draft's payload. The draft must belong to the
// caller; anyone else's draft surfaces as ErrDraftNotFound.
func (s *DraftService) UpdateDraft(ctx context.Context, id string, req UpdateDraftRequest) (*AgentTypeDraft, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.UpdateDraft",
		trace.WithAttributes(attribute.String("draft_id", id)))
	defer span.End()

	if err := s.authz.checkAnyAgentTypeWrite(ctx); err != nil {
		return nil, err
	}
	draft, err := s.ownedDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	payload, err := normalizeDraftPayload(req.Payload)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDraft(ctx, id, payload); err != nil {
		if errors.Is(err, ErrDraftNotFound) {
			return nil, ErrDraftNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "update draft")
		return nil, fmt.Errorf("update draft: %w", err)
	}
	draft.Payload = payload
	draft.UpdatedAt = time.Now().UTC()
	return draft, nil
}

// DeleteDraft removes a draft. The draft must belong to the caller; anyone
// else's draft surfaces as ErrDraftNotFound.
func (s *DraftService) DeleteDraft(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.DeleteDraft",
		trace.WithAttributes(attribute.String("draft_id", id)))
	defer span.End()

	if err := s.authz.checkAnyAgentTypeWrite(ctx); err != nil {
		return err
	}
	if _, err := s.ownedDraft(ctx, id); err != nil {
		return err
	}
	if err := s.repo.DeleteDraft(ctx, id); err != nil {
		if errors.Is(err, ErrDraftNotFound) {
			return ErrDraftNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete draft")
		return fmt.Errorf("delete draft: %w", err)
	}
	return nil
}

// ownedDraft loads a draft and verifies it belongs to the caller (tenant AND
// created_by from the verified claims). Any mismatch returns ErrDraftNotFound
// so another user's draft is indistinguishable from a nonexistent one.
func (s *DraftService) ownedDraft(ctx context.Context, id string) (*AgentTypeDraft, error) {
	tenantID, createdBy, err := callerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.repo.GetDraft(ctx, id)
	if err != nil {
		if errors.Is(err, ErrDraftNotFound) {
			return nil, ErrDraftNotFound
		}
		return nil, fmt.Errorf("get draft: %w", err)
	}
	if draft.TenantID != tenantID || draft.CreatedBy != createdBy {
		return nil, ErrDraftNotFound
	}
	return draft, nil
}

// StartReaper runs a background goroutine that periodically deletes drafts
// whose updated_at is older than the retention window — an abandoned draft is
// unfinished personal work, not a record worth keeping. It exits when ctx is
// canceled. The parallel to RunService.StartReaper, which reaps runs.
func (s *DraftService) StartReaper(ctx context.Context) {
	retention := s.cfg.Retention
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	interval := s.cfg.ReaperInterval
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().UTC().Add(-retention)
			deleted, err := s.repo.DeleteDraftsOlderThan(ctx, cutoff)
			if err != nil {
				s.logger.Error(ctx, "draft reaper failed", logging.Field{Key: "error", Value: err.Error()})
				continue
			}
			if deleted == 0 {
				continue
			}
			s.logger.Info(ctx, "reaped abandoned drafts",
				logging.Field{Key: "deleted_count", Value: deleted},
				logging.Field{Key: "cutoff", Value: cutoff},
			)
		}
	}
}
