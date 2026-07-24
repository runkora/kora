package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Delegation repository interface ─────────────────────────────────────────

// DelegationRepository defines the PostgreSQL-backed storage contract for
// delegation issuance records (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 3). Records are the durable audit trail of delegation events; the
// signed token itself is never stored.
type DelegationRepository interface {
	// CreateIssuance persists a new issuance record. Returns
	// ErrDelegationRunActive when an active (non-revoked) issuance already
	// exists for the same run.
	CreateIssuance(ctx context.Context, d *DelegationIssuance) error

	// GetActiveByRunID retrieves the active (non-revoked) issuance for a run.
	// Returns ErrDelegationNotFound when none exists — tombstoned issuances
	// are never returned, so retrieval fails closed after revocation.
	GetActiveByRunID(ctx context.Context, runID string) (*DelegationIssuance, error)

	// GetByJTI retrieves an issuance (active or tombstoned) by its JTI.
	// Returns ErrDelegationNotFound when no record exists.
	GetByJTI(ctx context.Context, jti string) (*DelegationIssuance, error)

	// Revoke tombstones an active issuance. Returns ErrDelegationNotFound when
	// no active record with the JTI exists (already revoked or never issued).
	Revoke(ctx context.Context, jti string, revokedAt time.Time) error

	// RevokeByPAT tombstones every active issuance minted with the given PAT and
	// returns their JTIs, so revoking a PAT cascades to the delegations it
	// minted (the caller blocklists the returned JTIs). Returns an empty slice
	// when the PAT minted no live delegations.
	RevokeByPAT(ctx context.Context, patID string, revokedAt time.Time) ([]string, error)

	// ListActiveByDelegator returns the active issuances a principal has minted
	// within a tenant, newest first.
	ListActiveByDelegator(ctx context.Context, tenantID, delegatorPrincipalID string) ([]DelegationIssuance, error)
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresDelegationRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresDelegationRepository creates a DelegationRepository backed by PostgreSQL.
func NewPostgresDelegationRepository(pool *pgxpool.Pool) DelegationRepository {
	return &postgresDelegationRepository{pool: pool}
}

const delegationIssuanceColumns = `jti, run_id, tenant_id, delegator_principal_id,
	delegator_principal_type, delegator_pat_id, agent_principal_id, created_at, revoked_at`

func (r *postgresDelegationRepository) CreateIssuance(ctx context.Context, d *DelegationIssuance) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO delegation_issuances
			(jti, run_id, tenant_id, delegator_principal_id, delegator_principal_type,
			 delegator_pat_id, agent_principal_id, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		d.JTI,
		d.RunID,
		d.TenantID,
		d.DelegatorPrincipalID,
		string(d.DelegatorPrincipalType),
		d.DelegatorPATID,
		d.AgentPrincipalID,
		d.CreatedAt,
	)
	if err != nil {
		if isDelegationUniqueViolation(err) {
			return ErrDelegationRunActive
		}
		return fmt.Errorf("create delegation issuance: %w", err)
	}
	return nil
}

func (r *postgresDelegationRepository) GetActiveByRunID(ctx context.Context, runID string) (*DelegationIssuance, error) {
	return r.scanIssuance(r.pool.QueryRow(ctx, `
		SELECT `+delegationIssuanceColumns+`
		FROM   delegation_issuances
		WHERE  run_id = $1
		  AND  revoked_at IS NULL
	`, runID))
}

func (r *postgresDelegationRepository) GetByJTI(ctx context.Context, jti string) (*DelegationIssuance, error) {
	return r.scanIssuance(r.pool.QueryRow(ctx, `
		SELECT `+delegationIssuanceColumns+`
		FROM   delegation_issuances
		WHERE  jti = $1
	`, jti))
}

func (r *postgresDelegationRepository) Revoke(ctx context.Context, jti string, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE delegation_issuances
		SET    revoked_at = $1
		WHERE  jti = $2
		  AND  revoked_at IS NULL
	`, revokedAt, jti)
	if err != nil {
		return fmt.Errorf("revoke delegation issuance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDelegationNotFound
	}
	return nil
}

func (r *postgresDelegationRepository) RevokeByPAT(ctx context.Context, patID string, revokedAt time.Time) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE delegation_issuances
		SET    revoked_at = $1
		WHERE  delegator_pat_id = $2
		  AND  revoked_at IS NULL
		RETURNING jti
	`, revokedAt, patID)
	if err != nil {
		return nil, fmt.Errorf("revoke delegation issuances by PAT: %w", err)
	}
	defer rows.Close()

	var jtis []string
	for rows.Next() {
		var jti string
		if scanErr := rows.Scan(&jti); scanErr != nil {
			return nil, fmt.Errorf("scan revoked delegation jti: %w", scanErr)
		}
		jtis = append(jtis, jti)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate revoked delegation jtis: %w", rows.Err())
	}
	return jtis, nil
}

func (r *postgresDelegationRepository) ListActiveByDelegator(ctx context.Context, tenantID, delegatorPrincipalID string) ([]DelegationIssuance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+delegationIssuanceColumns+`
		FROM   delegation_issuances
		WHERE  tenant_id = $1
		  AND  delegator_principal_id = $2
		  AND  revoked_at IS NULL
		ORDER BY created_at DESC
	`, tenantID, delegatorPrincipalID)
	if err != nil {
		return nil, fmt.Errorf("list delegation issuances: %w", err)
	}
	defer rows.Close()

	var issuances []DelegationIssuance
	for rows.Next() {
		d, scanErr := scanIssuanceRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		issuances = append(issuances, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delegation issuances: %w", err)
	}
	return issuances, nil
}

// scanIssuance hydrates a single-row query, mapping pgx.ErrNoRows to
// ErrDelegationNotFound.
func (r *postgresDelegationRepository) scanIssuance(row pgx.Row) (*DelegationIssuance, error) {
	d, err := scanIssuanceRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDelegationNotFound
		}
		return nil, err
	}
	return d, nil
}

// scanIssuanceRow scans one delegation_issuances row in column order
// delegationIssuanceColumns.
func scanIssuanceRow(row pgx.Row) (*DelegationIssuance, error) {
	var d DelegationIssuance
	var principalType string
	if err := row.Scan(
		&d.JTI,
		&d.RunID,
		&d.TenantID,
		&d.DelegatorPrincipalID,
		&principalType,
		&d.DelegatorPATID,
		&d.AgentPrincipalID,
		&d.CreatedAt,
		&d.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan delegation issuance: %w", err)
	}
	d.DelegatorPrincipalType = PrincipalType(principalType)
	return &d, nil
}

// isDelegationUniqueViolation reports whether err is a Postgres
// unique_violation (SQLSTATE 23505) — the partial unique index on
// (run_id) WHERE revoked_at IS NULL.
func isDelegationUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// ─── Noop implementation ─────────────────────────────────────────────────────

// noopDelegationRepository is the nil-safe default used when no repository is
// injected (mirrors noopUserProfileRepository). Every operation reports the
// feature as absent rather than panicking.
type noopDelegationRepository struct{}

func (noopDelegationRepository) CreateIssuance(context.Context, *DelegationIssuance) error {
	return errors.New("delegation repository not configured")
}

func (noopDelegationRepository) GetActiveByRunID(context.Context, string) (*DelegationIssuance, error) {
	return nil, ErrDelegationNotFound
}

func (noopDelegationRepository) GetByJTI(context.Context, string) (*DelegationIssuance, error) {
	return nil, ErrDelegationNotFound
}

func (noopDelegationRepository) Revoke(context.Context, string, time.Time) error {
	return ErrDelegationNotFound
}

func (noopDelegationRepository) RevokeByPAT(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}

func (noopDelegationRepository) ListActiveByDelegator(context.Context, string, string) ([]DelegationIssuance, error) {
	return nil, nil
}
