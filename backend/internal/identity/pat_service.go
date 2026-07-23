package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// CreatePAT issues a new Personal Access Token for the authenticated user.
// The requested scopes are validated against the user's current effective scopes.
// A corresponding policy grant is created as part of PAT issuance, but this is
// not atomic with PAT persistence; if a later step fails, cleanup is best-effort.
// The raw JWT is returned exactly once in CreatePATResponse.Token — prefixed
// with "orb_" for visual identification. Only the SHA-256 hash is persisted.
func (s *Service) CreatePAT(ctx context.Context, tenantID, userID string, userClaims *PlatformClaims, req CreatePATRequest) (*CreatePATResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.CreatePAT",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("user_id", userID),
			attribute.String("pat.name", req.Name),
		),
	)
	defer span.End()

	if len(req.Scopes) == 0 {
		return nil, ErrPATScopesRequired
	}

	if err := s.validateScopesAgainstUser(ctx, userClaims, req.Scopes); err != nil {
		return nil, err
	}

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

	rawJWT, err := s.signToken(principal, jti, "", id, now, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATSigningFailed.Error())
		return nil, fmt.Errorf("sign PAT: %w", err)
	}

	// Create the policy grant for this PAT. The grant ID is stored on the PAT
	// record so it can be deleted on revocation or scope update.
	//
	// Known limitation: grant creation happens before PAT persistence. If the PAT
	// persist fails and the subsequent grant cleanup also fails, the grant becomes
	// orphaned. This is an acceptable trade-off: an orphaned grant (no matching
	// PAT row) is inert — the PAT JWT was never issued to the caller, so no token
	// exists that could exercise the grant. The policy dashboard may show a stale
	// entry until the next cleanup cycle.
	grantID, err := s.policyClient.CreateGrant(ctx, tenantID, string(token.PrincipalTypePAT), id, req.Scopes, nil, false)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "grant creation failed")
		return nil, fmt.Errorf("create PAT grant: %w", err)
	}

	pat := &PAT{
		ID:        id,
		TenantID:  tenantID,
		UserID:    userID,
		Name:      req.Name,
		JTI:       jti,
		Scopes:    req.Scopes,
		GrantID:   &grantID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := s.pats.CreatePAT(ctx, pat, hashToken(rawJWT)); err != nil {
		// Grant was created but PAT persist failed — attempt cleanup.
		if delErr := s.policyClient.DeleteGrant(ctx, grantID); delErr != nil {
			span.RecordError(delErr)
			s.logger.Error(ctx, "PAT persist failed and grant cleanup failed",
				logging.Field{Key: "pat.id", Value: id},
				logging.Field{Key: "grant_id", Value: grantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
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
		Token:     PatTokenPrefix + rawJWT,
		Scopes:    req.Scopes,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		CreatedAt: now.Format(time.RFC3339),
	}, nil
}

// ListPATs returns all non-expired PATs for the given user, including revoked ones.
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
		scopes := p.Scopes
		if scopes == nil {
			// Defensive: JSONB DEFAULT '[]' prevents NULL in new rows, but rows
			// created before the migration may have a NULL scopes column.
			scopes = []string{}
		}
		var revokedAt *string
		if p.RevokedAt != nil {
			s := p.RevokedAt.Format(time.RFC3339)
			revokedAt = &s
		}
		summaries = append(summaries, PATSummary{
			ID:        p.ID,
			Name:      p.Name,
			Scopes:    scopes,
			ExpiresAt: p.ExpiresAt.Format(time.RFC3339),
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
			RevokedAt: revokedAt,
		})
	}

	span.SetStatus(codes.Ok, "PATs listed")
	return &ListPATsResponse{Tokens: summaries}, nil
}

// UpdatePATScopes replaces the scope set on an existing PAT.
// The new scopes must be a subset of the calling user's current effective scopes.
// The PAT must belong to the calling user — returns ErrPATNotFound otherwise.
func (s *Service) UpdatePATScopes(ctx context.Context, tenantID, userID, patID string, userClaims *PlatformClaims, req UpdatePATScopesRequest) error {
	ctx, span := s.tracer.Start(ctx, "identity.UpdatePATScopes",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("user_id", userID),
			attribute.String("pat.id", patID),
		),
	)
	defer span.End()

	if len(req.Scopes) == 0 {
		return ErrPATScopesRequired
	}

	pat, err := s.pats.GetByID(ctx, tenantID, userID, patID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATNotFound.Error())
		return fmt.Errorf("get PAT: %w", err)
	}

	err = s.validateScopesAgainstUser(ctx, userClaims, req.Scopes)
	if err != nil {
		return err
	}

	// Create new grant first, then persist the PAT update, then remove old grant.
	// This ordering ensures the PAT is never left without a valid grant:
	//   - If CreateGrant fails:    old grant still active, no change.
	//   - If UpdateScopes fails:   new grant is cleaned up, old grant still referenced.
	//   - If old grant deletion fails: it becomes an inert orphan — the PAT now
	//     references the new grant so no live token can exercise the old grant.
	newGrantID, err := s.policyClient.CreateGrant(ctx, tenantID, string(token.PrincipalTypePAT), patID, req.Scopes, nil, false)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create new grant failed")
		return fmt.Errorf("create new PAT grant: %w", err)
	}

	oldGrantID := pat.GrantID

	if err := s.pats.UpdateScopes(ctx, tenantID, userID, patID, req.Scopes, newGrantID); err != nil {
		// DB update failed — clean up the new grant; old grant remains active.
		if delErr := s.policyClient.DeleteGrant(ctx, newGrantID); delErr != nil {
			span.RecordError(delErr)
			s.logger.Error(ctx, "PAT scope update failed and new grant cleanup failed",
				logging.Field{Key: "pat.id", Value: patID},
				logging.Field{Key: "grant_id", Value: newGrantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrPATUpdateFailed.Error())
		return fmt.Errorf("update PAT scopes: %w", err)
	}

	// PAT now references newGrantID. Remove the old grant — best-effort; failure
	// leaves an inert orphan (no live token can exercise it).
	if oldGrantID != nil {
		if delErr := s.policyClient.DeleteGrant(ctx, *oldGrantID); delErr != nil && !errors.Is(delErr, policyclient.ErrGrantNotFound) {
			span.RecordError(delErr)
			s.logger.Error(ctx, "failed to delete old PAT grant after scope update",
				logging.Field{Key: "pat.id", Value: patID},
				logging.Field{Key: "grant_id", Value: *oldGrantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
	}

	span.SetStatus(codes.Ok, "PAT scopes updated")
	s.logger.Info(ctx, "PAT scopes updated",
		logging.Field{Key: "pat.id", Value: patID},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "user_id", Value: userID},
	)
	return nil
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
	remaining := pat.ExpiresAt.Sub(now)
	if remaining > 0 {
		if err := s.blocklist.BlockJTI(ctx, pat.JTI, remaining); err != nil {
			s.logger.Error(ctx, "failed to blocklist revoked PAT JTI",
				logging.Field{Key: "pat.id", Value: patID},
				logging.Field{Key: "jti", Value: pat.JTI},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}

	// Delete the policy grant. JTI blocklisting is the primary revocation
	// mechanism (already effective above); grant deletion is a cleanup step
	// that keeps the policy dashboard accurate.
	if pat.GrantID != nil {
		if err := s.policyClient.DeleteGrant(ctx, *pat.GrantID); err != nil && !errors.Is(err, policyclient.ErrGrantNotFound) {
			s.logger.Error(ctx, "failed to delete PAT grant on revocation",
				logging.Field{Key: "pat.id", Value: patID},
				logging.Field{Key: "grant_id", Value: *pat.GrantID},
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

// validateScopesAgainstUser checks that every requested scope is covered by the
// user's current effective scopes (bundle-consistent, not IdP-consistent).
// Coverage uses the same prefix-match semantics as scope_covers in authz.rego:
// a coarse scope like inference:anthropic:use covers the finer
// inference:anthropic:claude-sonnet-4-6:use, so a PAT can always be scoped
// narrower than the holder's grant without being rejected here.
func (s *Service) validateScopesAgainstUser(ctx context.Context, claims *PlatformClaims, requested []string) error {
	effective, err := s.policyEval.Scopes(ctx, policy.ScopesInput{Claims: claims})
	if err != nil {
		return fmt.Errorf("resolve effective scopes: %w", err)
	}

	var forbidden []string
	for _, sc := range requested {
		if !policy.AnyScopeCovers(effective, sc) {
			forbidden = append(forbidden, sc)
		}
	}
	if len(forbidden) > 0 {
		return &ErrPATScopesForbiddenError{Scopes: forbidden}
	}
	return nil
}
