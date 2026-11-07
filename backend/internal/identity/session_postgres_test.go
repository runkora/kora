package identity

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/storage"
)

// Postgres integration tests for the tenant switch transaction. They run only
// when KORA_TEST_DB_URL points at a migrated database; otherwise they skip.
// Each test isolates itself under a random principal and cleans up on exit.
//
//	KORA_TEST_DB_URL='postgres://kora:kora@localhost:5432/kora?sslmode=disable' \
//	  go test ./internal/identity/ -run Postgres

// testSwitchRepos connects the repositories under a random principal it deletes on cleanup.
func testSwitchRepos(t *testing.T) (*pgxpool.Pool, SessionRepository, RefreshTokenRepository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run identity Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	principal := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM identity_refresh_tokens WHERE session_id IN (SELECT id FROM identity_sessions WHERE principal_id = $1)`, principal)
		_, _ = pool.Exec(ctx, `DELETE FROM identity_sessions WHERE principal_id = $1`, principal)
		_, _ = pool.Exec(ctx, `DELETE FROM event_outbox WHERE envelope->>'koraprincipal' = $1`, principal)
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE principal = $1`, principal)
		pool.Close()
	})
	db := storage.NewPool(pool)
	return pool, NewPostgresSessionRepository(db), NewPostgresRefreshTokenRepository(db), principal
}

func testSessionRows(principal, tenant string) (*Session, *RefreshToken) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := &Session{
		ID: uuid.NewString(), JTI: uuid.NewString(), PrincipalID: principal, PrincipalType: PrincipalTypeHuman,
		TenantID: tenant, DisplayName: "Test", IdentitySource: IdentitySourceFederated,
		Claims: map[string]string{"issuer": "https://idp.example.com"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	rt := &RefreshToken{ID: uuid.NewString(), SessionID: s.ID, TokenHash: uuid.NewString(), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	return s, rt
}

// seedSwitchSession persists a live session with one refresh token.
func seedSwitchSession(t *testing.T, sessions SessionRepository, refresh RefreshTokenRepository, principal, tenant string) (*Session, *RefreshToken) {
	t.Helper()
	s, rt := testSessionRows(principal, tenant)
	if err := sessions.CreateSession(context.Background(), s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := refresh.CreateRefreshToken(context.Background(), rt); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	return s, rt
}

func liveRows(t *testing.T, pool *pgxpool.Pool, principal string) (sessions, refreshTokens int) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity_sessions WHERE principal_id = $1 AND revoked_at IS NULL`, principal).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity_refresh_tokens rt JOIN identity_sessions s ON s.id = rt.session_id WHERE s.principal_id = $1 AND rt.revoked_at IS NULL`, principal).Scan(&refreshTokens); err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	return sessions, refreshTokens
}

func TestPostgres_SwitchSession_CommitsTogether(t *testing.T) {
	_, sessions, refresh, principal := testSwitchRepos(t)
	ctx := context.Background()
	replaced, oldRT := seedSwitchSession(t, sessions, refresh, principal, "eng")
	next, nextRT := testSessionRows(principal, "sre")

	if err := sessions.SwitchSession(ctx, next, nextRT, oldRT, time.Now()); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if got, _ := sessions.GetSessionByID(ctx, replaced.ID); got.RevokedAt == nil {
		t.Error("replaced session is live")
	}
	if got, _ := refresh.GetRefreshTokenByHash(ctx, oldRT.TokenHash); got.RevokedAt == nil {
		t.Error("replaced refresh token is live")
	}
	if got, err := sessions.GetSessionByID(ctx, next.ID); err != nil || got.TenantID != "sre" || got.RevokedAt != nil {
		t.Errorf("next session = %+v, %v", got, err)
	}
	if got, err := refresh.GetRefreshTokenByHash(ctx, nextRT.TokenHash); err != nil || got.SessionID != next.ID {
		t.Errorf("next refresh token = %+v, %v", got, err)
	}
}

func TestPostgres_SwitchSession_RollsBack(t *testing.T) {
	pool, sessions, refresh, principal := testSwitchRepos(t)
	ctx := context.Background()
	replaced, oldRT := seedSwitchSession(t, sessions, refresh, principal, "eng")

	// Reusing the replaced session's ID makes the insert fail after both revokes ran.
	next, nextRT := testSessionRows(principal, "sre")
	next.ID = replaced.ID
	if err := sessions.SwitchSession(ctx, next, nextRT, oldRT, time.Now()); err == nil {
		t.Fatal("SwitchSession succeeded with a conflicting session ID")
	}
	if s, rt := liveRows(t, pool, principal); s != 1 || rt != 1 {
		t.Errorf("after a failed switch: %d live sessions, %d live refresh tokens, want 1 and 1", s, rt)
	}
}

func TestPostgres_SwitchSession_ConcurrentSwitchesFromOneSession(t *testing.T) {
	pool, sessions, refresh, principal := testSwitchRepos(t)
	_, oldRT := seedSwitchSession(t, sessions, refresh, principal, "eng")

	const racers = 5
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() {
			next, nextRT := testSessionRows(principal, "sre")
			errs[i] = sessions.SwitchSession(context.Background(), next, nextRT, oldRT, time.Now())
		})
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrRefreshTokenNotFound) && !errors.Is(err, ErrSessionNotFound):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d switches committed, want 1", won)
	}
	if s, rt := liveRows(t, pool, principal); s != 1 || rt != 1 {
		t.Errorf("%d live sessions, %d live refresh tokens, want 1 and 1", s, rt)
	}
}

func TestPostgres_SwitchSession_RefusesReplacedNotLive(t *testing.T) {
	tests := []struct {
		name string
		end  func(ctx context.Context, sessions SessionRepository, refresh RefreshTokenRepository, replaced *Session, rt *RefreshToken) error
		want error
	}{
		{"refresh consumed the token", func(ctx context.Context, _ SessionRepository, refresh RefreshTokenRepository, _ *Session, rt *RefreshToken) error {
			return refresh.RevokeRefreshToken(ctx, rt.ID, time.Now())
		}, ErrRefreshTokenNotFound},
		{"session revoked", func(ctx context.Context, sessions SessionRepository, _ RefreshTokenRepository, replaced *Session, _ *RefreshToken) error {
			return sessions.RevokeSession(ctx, replaced.ID, time.Now())
		}, ErrSessionNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, sessions, refresh, principal := testSwitchRepos(t)
			ctx := context.Background()
			replaced, oldRT := seedSwitchSession(t, sessions, refresh, principal, "eng")
			if err := tc.end(ctx, sessions, refresh, replaced, oldRT); err != nil {
				t.Fatalf("end the replaced row: %v", err)
			}
			liveSessions, liveTokens := liveRows(t, pool, principal)

			next, nextRT := testSessionRows(principal, "sre")
			if err := sessions.SwitchSession(ctx, next, nextRT, oldRT, time.Now()); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if _, err := sessions.GetSessionByID(ctx, next.ID); !errors.Is(err, ErrSessionNotFound) {
				t.Errorf("next session was written: %v", err)
			}
			if s, rt := liveRows(t, pool, principal); s != liveSessions || rt != liveTokens {
				t.Errorf("live sessions %d to %d, live refresh tokens %d to %d, want no change", liveSessions, s, liveTokens, rt)
			}
		})
	}
}

func TestPostgres_SwitchSession_RacesRefresh(t *testing.T) {
	pool, sessions, refresh, principal := testSwitchRepos(t)
	_, oldRT := seedSwitchSession(t, sessions, refresh, principal, "eng")

	var switchErr, refreshErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		next, nextRT := testSessionRows(principal, "sre")
		switchErr = sessions.SwitchSession(context.Background(), next, nextRT, oldRT, time.Now())
	})
	wg.Go(func() {
		refreshErr = refresh.RevokeRefreshToken(context.Background(), oldRT.ID, time.Now())
	})
	wg.Wait()

	if (switchErr == nil) == (refreshErr == nil) {
		t.Fatalf("switch err = %v, refresh err = %v, want exactly one to succeed", switchErr, refreshErr)
	}
	if switchErr == nil {
		if s, _ := liveRows(t, pool, principal); s != 1 {
			t.Errorf("%d live sessions after the switch won, want 1", s)
		}
	} else if !errors.Is(switchErr, ErrRefreshTokenNotFound) {
		t.Errorf("switch err = %v, want ErrRefreshTokenNotFound", switchErr)
	}
}
