package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// ─── PAT repository interface ─────────────────────────────────────────────────

// PATRepository defines the PostgreSQL-backed PAT storage contract.
type PATRepository interface {
	// CreatePAT persists a new PAT record. TokenHash is the SHA-256 hex of the
	// raw JWT; it is stored as a security property and is never queried for auth.
	CreatePAT(ctx context.Context, pat *PAT, tokenHash string) error

	// ListByUser returns all non-expired PATs for a user, including revoked ones.
	ListByUser(ctx context.Context, tenantID, userID string) ([]PAT, error)

	// GetByID retrieves a PAT by its primary ID, scoped to the owning user.
	// Returns ErrPATNotFound if no matching active record exists.
	GetByID(ctx context.Context, tenantID, userID, id string) (*PAT, error)

	// GetOwnersByIDs returns a map of pat_id → PATOwnerInfo for the given PAT IDs
	// within a tenant. IDs not found in the table are simply omitted.
	GetOwnersByIDs(ctx context.Context, tenantID string, ids []string) (map[string]PATOwnerInfo, error)

	// UpdateScopes replaces the scopes and grant_id on an existing PAT.
	UpdateScopes(ctx context.Context, tenantID, userID, id string, scopes []string, grantID string) error

	// Revoke marks a PAT as revoked. Returns ErrPATNotFound if the PAT does not
	// exist or belongs to a different user (prevents cross-user information leakage).
	Revoke(ctx context.Context, tenantID, userID, id string, revokedAt time.Time) error
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresPATRepository struct {
	pool *storage.Pool
}

// NewPostgresPATRepository creates a PATRepository backed by PostgreSQL.
func NewPostgresPATRepository(pool *storage.Pool) PATRepository {
	return &postgresPATRepository{pool: pool}
}

func (r *postgresPATRepository) CreatePAT(ctx context.Context, pat *PAT, tokenHash string) error {
	scopesJSON, err := json.Marshal(pat.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO user_pat_tokens
				(id, tenant_id, user_id, name, token_hash, jti, scopes, grant_id, expires_at, created_at)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`,
			pat.ID,
			pat.TenantID,
			pat.UserID,
			pat.Name,
			tokenHash,
			pat.JTI,
			scopesJSON,
			pat.GrantID,
			pat.ExpiresAt,
			pat.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("create PAT: %w", err)
		}
		return events.Publish(ctx, r.pool, patEvent(events.PATCreated, pat, nil))
	})
}

func (r *postgresPATRepository) ListByUser(ctx context.Context, tenantID, userID string) ([]PAT, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, jti, scopes, grant_id, expires_at, created_at, revoked_at
		FROM   user_pat_tokens
		WHERE  tenant_id  = $1
		  AND  user_id    = $2
		  AND  expires_at > now()
		ORDER BY created_at DESC
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list PATs: %w", err)
	}
	defer rows.Close()

	var pats []PAT
	for rows.Next() {
		var p PAT
		var scopesJSON []byte
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.UserID, &p.Name,
			&p.JTI, &scopesJSON, &p.GrantID, &p.ExpiresAt, &p.CreatedAt, &p.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan PAT row: %w", err)
		}
		if err := json.Unmarshal(scopesJSON, &p.Scopes); err != nil {
			return nil, fmt.Errorf("unmarshal scopes: %w", err)
		}
		pats = append(pats, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PAT rows: %w", err)
	}
	return pats, nil
}

func (r *postgresPATRepository) GetByID(ctx context.Context, tenantID, userID, id string) (*PAT, error) {
	var p PAT
	var scopesJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, name, jti, scopes, grant_id, expires_at, created_at, revoked_at
		FROM   user_pat_tokens
		WHERE  id        = $1
		  AND  tenant_id = $2
		  AND  user_id   = $3
		  AND  revoked_at IS NULL
	`, id, tenantID, userID).Scan(
		&p.ID, &p.TenantID, &p.UserID, &p.Name,
		&p.JTI, &scopesJSON, &p.GrantID, &p.ExpiresAt, &p.CreatedAt, &p.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPATNotFound
		}
		return nil, fmt.Errorf("get PAT by ID: %w", err)
	}
	if err := json.Unmarshal(scopesJSON, &p.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}
	return &p, nil
}

func (r *postgresPATRepository) GetOwnersByIDs(ctx context.Context, tenantID string, ids []string) (map[string]PATOwnerInfo, error) {
	if len(ids) == 0 {
		return map[string]PATOwnerInfo{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name
		FROM   user_pat_tokens
		WHERE  tenant_id = $1
		  AND  id        = ANY($2)
	`, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("get PAT owners: %w", err)
	}
	defer rows.Close()

	owners := make(map[string]PATOwnerInfo, len(ids))
	for rows.Next() {
		var patID string
		var info PATOwnerInfo
		if err := rows.Scan(&patID, &info.UserID, &info.PATName); err != nil {
			return nil, fmt.Errorf("scan PAT owner row: %w", err)
		}
		owners[patID] = info
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PAT owner rows: %w", err)
	}
	return owners, nil
}

func (r *postgresPATRepository) UpdateScopes(ctx context.Context, tenantID, userID, id string, scopes []string, grantID string) error {
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		before, err := r.lockLive(ctx, tenantID, userID, id)
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `
			UPDATE user_pat_tokens
			SET    scopes   = $1,
			       grant_id = $2
			WHERE  id = $3
		`, scopesJSON, grantID, id); err != nil {
			return fmt.Errorf("update PAT scopes: %w", err)
		}
		after := *before
		after.Scopes, after.GrantID = scopes, &grantID
		changed := events.Changed(newPATEntity(before), newPATEntity(&after))
		if len(changed) == 0 {
			return nil
		}
		return events.Publish(ctx, r.pool, patEvent(events.PATUpdated, &after, changed))
	})
}

// lockLive reads an unrevoked PAT of the user and locks it until the transaction ends.
func (r *postgresPATRepository) lockLive(ctx context.Context, tenantID, userID, id string) (*PAT, error) {
	var p PAT
	var scopesJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, name, jti, scopes, grant_id, expires_at, created_at, revoked_at
		FROM   user_pat_tokens
		WHERE  id = $1 AND tenant_id = $2 AND user_id = $3 AND revoked_at IS NULL
		FOR UPDATE
	`, id, tenantID, userID).Scan(
		&p.ID, &p.TenantID, &p.UserID, &p.Name,
		&p.JTI, &scopesJSON, &p.GrantID, &p.ExpiresAt, &p.CreatedAt, &p.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPATNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock PAT: %w", err)
	}
	if err := json.Unmarshal(scopesJSON, &p.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}
	return &p, nil
}

func (r *postgresPATRepository) Revoke(ctx context.Context, tenantID, userID, id string, revokedAt time.Time) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		pat, err := r.lockLive(ctx, tenantID, userID, id)
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `UPDATE user_pat_tokens SET revoked_at = $1 WHERE id = $2`, revokedAt, id); err != nil {
			return fmt.Errorf("revoke PAT: %w", err)
		}
		pat.RevokedAt = &revokedAt
		return events.Publish(ctx, r.pool, patEvent(events.PATRevoked, pat, nil))
	})
}
