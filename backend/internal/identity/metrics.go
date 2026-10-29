package identity

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// activeSessionsQueryTimeout bounds the sessions-active gauge's DB read. The
// Prometheus exporter invokes this callback at scrape time with
// context.TODO() — no deadline of its own — so without a timeout a slow
// database hangs the whole scrape indefinitely. Mirrors the short DB-query
// budgets in internal/platform/storage/db.go (ping/health checks).
const activeSessionsQueryTimeout = 5 * time.Second

// Metrics is the identity feature's instrument set.
//
// It is injected, never global, so a nil *Metrics is fully usable and records
// nothing: tests and any binary that has not wired a meter behave exactly as
// they did before instrumentation existed.
//
// What is measured answers the questions an operator has about the token
// authority: how many sessions are live right now, whether delegation
// issuance/revocation is flowing, and — by a bounded, enum-backed reason —
// why a token or refresh exchange is being rejected.
type Metrics struct {
	delegationsIssued  metric.Int64Counter
	delegationsRevoked metric.Int64Counter
	authFailures       metric.Int64Counter

	// registration keeps the observable callback alive, and lets Close
	// unregister it.
	registration metric.Registration
}

// activeSessionsReader is the one read the sessions-active gauge takes.
type activeSessionsReader interface {
	CountActiveSessions(ctx context.Context) (map[string]int64, error)
}

// Fixed, bounded reason labels for kora_identity_auth_failures_total. Never
// derived from an arbitrary string — only from the sentinel error the caller
// already returns.
const (
	authFailureReasonRefreshTokenNotFound = "refresh_token_not_found"
	authFailureReasonRefreshTokenRevoked  = "refresh_token_revoked"
	authFailureReasonRefreshTokenExpired  = "refresh_token_expired"
	authFailureReasonTokenRevoked         = "token_revoked"
	authFailureReasonInvalidToken         = "invalid_token"
)

// NewMetrics builds the instrument set on the given meter.
//
// repo is read by the sessions-active callback, which is why this takes one:
// an observable gauge is only honest if it reports the live count at
// collection time rather than a counter the code remembered to keep updated.
func NewMetrics(meter metric.Meter, repo activeSessionsReader, logger logging.Logger) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.delegationsIssued, err = meter.Int64Counter(
		"kora.identity.delegations_issued",
		metric.WithDescription("Delegation issuances minted"),
		metric.WithUnit("{delegation}"),
	); err != nil {
		return nil, fmt.Errorf("create delegations-issued counter: %w", err)
	}

	if m.delegationsRevoked, err = meter.Int64Counter(
		"kora.identity.delegations_revoked",
		metric.WithDescription("Delegation issuances revoked"),
		metric.WithUnit("{delegation}"),
	); err != nil {
		return nil, fmt.Errorf("create delegations-revoked counter: %w", err)
	}

	if m.authFailures, err = meter.Int64Counter(
		"kora.identity.auth_failures",
		metric.WithDescription("Authentication and token exchange failures, by reason"),
		metric.WithUnit("{failure}"),
	); err != nil {
		return nil, fmt.Errorf("create auth-failures counter: %w", err)
	}

	sessionsActive, err := meter.Int64ObservableGauge(
		"kora.identity.sessions.active",
		metric.WithDescription("Sessions that are neither revoked nor expired"),
		metric.WithUnit("{session}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create sessions-active gauge: %w", err)
	}

	m.registration, err = meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			ctx, cancel := context.WithTimeout(ctx, activeSessionsQueryTimeout)
			defer cancel()

			counts, countErr := repo.CountActiveSessions(ctx)
			if countErr != nil {
				// Reported, not returned: a failed scrape of this gauge must
				// not abort the whole collection cycle and take the other
				// instruments down with it.
				logger.Error(ctx, "failed to read active session count for metrics",
					logging.Field{Key: "error", Value: countErr.Error()})
				return nil
			}
			for tenantID, count := range counts {
				o.ObserveInt64(sessionsActive, count, metric.WithAttributes(attribute.String("tenant_id", tenantID)))
			}
			return nil
		},
		sessionsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("register sessions-active callback: %w", err)
	}

	return m, nil
}

// Close unregisters the observable callback. Safe on a nil *Metrics.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	if err := m.registration.Unregister(); err != nil {
		return fmt.Errorf("unregister identity metrics callback: %w", err)
	}
	return nil
}

// recordDelegationIssued counts a successfully minted delegation issuance,
// tagged by the tenant the delegation belongs to.
func (m *Metrics) recordDelegationIssued(ctx context.Context, tenantID string) {
	if m == nil {
		return
	}
	m.delegationsIssued.Add(ctx, 1, metric.WithAttributes(attribute.String("tenant_id", tenantID)))
}

// recordDelegationRevoked counts a successfully revoked delegation issuance,
// tagged by the tenant the delegation belongs to.
func (m *Metrics) recordDelegationRevoked(ctx context.Context, tenantID string) {
	if m == nil {
		return
	}
	m.delegationsRevoked.Add(ctx, 1, metric.WithAttributes(attribute.String("tenant_id", tenantID)))
}

// recordAuthFailure counts an authentication/token-exchange rejection, tagged
// by a fixed reason drawn from the authFailureReason* constants and, where
// resolvable at the call site, the tenant involved. tenantID is "" when a
// failure occurs before any tenant is known (e.g. a malformed token) — a
// real, bounded value, not an omission.
func (m *Metrics) recordAuthFailure(ctx context.Context, reason, tenantID string) {
	if m == nil {
		return
	}
	m.authFailures.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
		attribute.String("tenant_id", tenantID),
	))
}
