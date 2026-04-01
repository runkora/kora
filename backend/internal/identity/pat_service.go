package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// CreatePAT issues a new Personal Access Token for the authenticated user.
// The raw JWT is returned exactly once in CreatePATResponse.Token — prefixed
// with "orb_" for visual identification. Only the SHA-256 hash is persisted.
func (s *Service) CreatePAT(ctx context.Context, tenantID, userID string, req CreatePATRequest) (*CreatePATResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.CreatePAT",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("user_id", userID),
			attribute.String("pat.name", req.Name),
		),
	)
	defer span.End()

	ttlDays := req.ExpiresInDays
	if ttlDays == 0 {
		ttlDays = s.cfg.Token.PATDefaultTTLDays
	}

	id := uuid.NewString()
	jti := uuid.NewString()
	now := time.Now().UTC()
	ttl := time.Duration(ttlDays) * 24 * time.Hour
	expiresAt := now.Add(ttl)

	principal := &Principal{
		ID:             userID,
		Type:           token.PrincipalTypePAT,
		TenantID:       tenantID,
		IdentitySource: IdentitySourcePlatform,
	}

	rawJWT, err := s.signToken(principal, jti, id, now, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATSigningFailed.Error())
		return nil, fmt.Errorf("sign PAT: %w", err)
	}

	pat := &PAT{
		ID:        id,
		TenantID:  tenantID,
		UserID:    userID,
		Name:      req.Name,
		JTI:       jti,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := s.pats.CreatePAT(ctx, pat, hashToken(rawJWT)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATPersistFailed.Error())
		return nil, fmt.Errorf("persist PAT: %w", err)
	}

	span.SetAttributes(
		attribute.String("pat.id", id),
		attribute.String("jti", jti),
		attribute.Int64("pat.ttl_days", int64(ttlDays)),
	)
	span.SetStatus(codes.Ok, "PAT created")

	s.logger.Info(ctx, "PAT created",
		logging.Field{Key: "pat.id", Value: id},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "user_id", Value: userID},
	)

	return &CreatePATResponse{
		ID:        id,
		Name:      req.Name,
		Token:     "orb_" + rawJWT,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		CreatedAt: now.Format(time.RFC3339),
	}, nil
}

// ListPATs returns all active (non-revoked, non-expired) PATs for the given user.
func (s *Service) ListPATs(ctx context.Context, tenantID, userID string) (*ListPATsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.ListPATs",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("user_id", userID),
		),
	)
	defer span.End()

	pats, err := s.pats.ListByUser(ctx, tenantID, userID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATListFailed.Error())
		return nil, fmt.Errorf("list PATs: %w", err)
	}

	summaries := make([]PATSummary, 0, len(pats))
	for _, p := range pats {
		summaries = append(summaries, PATSummary{
			ID:        p.ID,
			Name:      p.Name,
			ExpiresAt: p.ExpiresAt.Format(time.RFC3339),
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
		})
	}

	span.SetStatus(codes.Ok, "PATs listed")
	return &ListPATsResponse{Tokens: summaries}, nil
}

// RevokePAT revokes a PAT by ID, scoped to the owning user.
// Returns ErrPATNotFound if the PAT does not exist or belongs to a different user.
func (s *Service) RevokePAT(ctx context.Context, tenantID, userID, patID string) error {
	ctx, span := s.tracer.Start(ctx, "identity.RevokePAT",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("user_id", userID),
			attribute.String("pat.id", patID),
		),
	)
	defer span.End()

	// Fetch first to get the JTI for blocklisting.
	pat, err := s.pats.GetByID(ctx, tenantID, userID, patID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATNotFound.Error())
		return fmt.Errorf("get PAT for revocation: %w", err)
	}

	now := time.Now().UTC()
	if err := s.pats.Revoke(ctx, tenantID, userID, patID, now); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATRevokeFailed.Error())
		return fmt.Errorf("revoke PAT: %w", err)
	}

	// Add JTI to the Redis blocklist for fast rejection on in-flight requests.
	// TTL = remaining lifetime so the blocklist entry self-expires.
	remaining := pat.ExpiresAt.Sub(now)
	if remaining > 0 {
		if err := s.blocklist.BlockJTI(ctx, pat.JTI, remaining); err != nil {
			// Non-fatal: the DB record is already revoked. Log and continue.
			s.logger.Error(ctx, "failed to blocklist revoked PAT JTI",
				logging.Field{Key: "pat.id", Value: patID},
				logging.Field{Key: "jti", Value: pat.JTI},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}

	span.SetStatus(codes.Ok, "PAT revoked")

	s.logger.Info(ctx, "PAT revoked",
		logging.Field{Key: "pat.id", Value: patID},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "user_id", Value: userID},
	)

	return nil
}
