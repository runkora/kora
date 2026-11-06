package identity

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

// identityEvent is the part of an outbox row an identity test asserts.
type identityEvent struct {
	Type          events.Type `json:"type"`
	Tenant        string      `json:"koratenant"`
	Principal     string      `json:"koraprincipal"`
	PrincipalType string      `json:"koraprincipaltype"`
	Data          struct {
		Entity  map[string]any `json:"entity"`
		Changed []string       `json:"changed"`
	} `json:"data"`
}

func TestPostgres_IdentityEvents(t *testing.T) {
	raw, sessions, refresh, principal := testSwitchRepos(t)
	pool := storage.NewPool(raw)
	pats := NewPostgresPATRepository(pool)
	delegations := NewPostgresDelegationRepository(pool)
	tenant := "test-" + uuid.NewString()
	signIn, signInRT := testSessionRows(principal, tenant)
	_, oldRT := seedSwitchSession(t, sessions, refresh, principal, tenant)

	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := sessions.StartSession(ctx, signIn, signInRT); err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if got, err := refresh.GetRefreshTokenByHash(ctx, signInRT.TokenHash); err != nil || got.SessionID != signIn.ID {
			t.Errorf("sign-in refresh token = %+v, %v", got, err)
		}
		// A platform component's session has no tenant. It starts and records no event.
		system, _ := testSessionRows(principal, "")
		system.PrincipalType = PrincipalTypeSystem
		if err := sessions.StartSession(ctx, system, nil); err != nil {
			t.Fatalf("StartSession for a system principal: %v", err)
		}
		// A refresh writes a session with no event.
		refreshed, _ := testSessionRows(principal, tenant)
		if err := sessions.CreateSession(ctx, refreshed); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		next, nextRT := testSessionRows(principal, "other-"+tenant)
		if err := sessions.SwitchSession(ctx, next, nextRT, oldRT, time.Now()); err != nil {
			t.Fatalf("SwitchSession: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		pat := &PAT{
			ID: uuid.NewString(), TenantID: tenant, UserID: principal, Name: "ci", JTI: uuid.NewString(),
			Scopes: []string{"agents:read"}, GrantID: new("grant-1"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}
		if err := pats.CreatePAT(ctx, pat, "token-hash-"+uuid.NewString()); err != nil {
			t.Fatalf("CreatePAT: %v", err)
		}
		if err := pats.UpdateScopes(ctx, tenant, principal, pat.ID, []string{"agents:read", "runs:read"}, "grant-2"); err != nil {
			t.Fatalf("UpdateScopes: %v", err)
		}
		if err := pats.Revoke(ctx, tenant, principal, pat.ID, now); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if err := delegations.CreateIssuance(ctx, &DelegationIssuance{
			JTI: uuid.NewString(), RunID: uuid.NewString(), TenantID: tenant, DelegatorPrincipalID: principal,
			DelegatorPrincipalType: PrincipalTypeHuman, AgentPrincipalID: "spiffe://runkora.test/agent", CreatedAt: now,
		}); err != nil {
			t.Fatalf("CreateIssuance: %v", err)
		}

		rows, err := pool.Query(ctx, `SELECT envelope FROM event_outbox WHERE tenant_id = ANY($1) ORDER BY position`, []string{tenant, next.TenantID, ""})
		if err != nil {
			t.Fatal(err)
		}
		bodies, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
		if err != nil {
			t.Fatal(err)
		}
		var got []identityEvent
		for _, b := range bodies {
			var e identityEvent
			if err := json.Unmarshal(b, &e); err != nil {
				t.Fatal(err)
			}
			got = append(got, e)
			if strings.Contains(string(b), pat.JTI) || strings.Contains(string(b), "token-hash") {
				t.Errorf("%s carries a token value: %s", e.Type, b)
			}
		}
		var typesSeen []events.Type
		for _, e := range got {
			typesSeen = append(typesSeen, e.Type)
		}
		want := []events.Type{
			events.SessionCreated, events.TenantSwitched, events.TenantSwitched,
			events.PATCreated, events.PATUpdated, events.PATRevoked, events.DelegationIssued,
		}
		if !slices.Equal(typesSeen, want) {
			t.Fatalf("events = %v, want %v", typesSeen, want)
		}
		if got[0].PrincipalType != events.PrincipalUser || got[0].Tenant != tenant {
			t.Errorf("sign-in event = %+v, want the user in their tenant", got[0])
		}
		// Each tenant records its own side of the switch, and neither names the other.
		if got[1].Tenant != tenant || got[1].Data.Entity["direction"] != "out" || strings.Contains(string(bodies[1]), next.TenantID) {
			t.Errorf("switch-out event = %s", bodies[1])
		}
		if got[2].Tenant != next.TenantID || got[2].Data.Entity["direction"] != "in" || strings.Contains(string(bodies[2]), `"`+tenant+`"`) {
			t.Errorf("switch-in event = %s", bodies[2])
		}
		if got[6].Principal != principal || got[6].PrincipalType != events.PrincipalUser {
			t.Errorf("delegation event = %+v, want the delegator as principal", got[6])
		}
		if !slices.Equal(got[4].Data.Changed, []string{"grant_id", "scopes"}) {
			t.Errorf("PAT update changed = %v", got[4].Data.Changed)
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}

func TestPostgres_PATRefusedOrUnchangedWriteRecordsNothing(t *testing.T) {
	raw, _, _, principal := testSwitchRepos(t)
	pool := storage.NewPool(raw)
	pats := NewPostgresPATRepository(pool)
	tenant := "test-" + uuid.NewString()
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = raw.Exec(ctx, `DELETE FROM user_pat_tokens WHERE tenant_id = $1`, tenant)
		_, _ = raw.Exec(ctx, `DELETE FROM event_outbox WHERE tenant_id = $1`, tenant)
		_, _ = raw.Exec(ctx, `DELETE FROM events WHERE tenant_id = $1`, tenant)
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	pat := &PAT{
		ID: uuid.NewString(), TenantID: tenant, UserID: principal, Name: "ci", JTI: uuid.NewString(),
		Scopes: []string{"agents:read"}, GrantID: new("grant-1"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := pats.CreatePAT(ctx, pat, "hash-"+uuid.NewString()); err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if _, err := raw.Exec(ctx, `DELETE FROM event_outbox WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}
	// The same id violates the primary key, and the refused write leaves no event.
	if err := pats.CreatePAT(ctx, pat, "hash-"+uuid.NewString()); err == nil {
		t.Fatal("CreatePAT with a taken id succeeded")
	}
	// The same scopes and grant change nothing and record nothing.
	if err := pats.UpdateScopes(ctx, tenant, principal, pat.ID, pat.Scopes, *pat.GrantID); err != nil {
		t.Fatalf("UpdateScopes: %v", err)
	}
	var n int
	if err := raw.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a refused or unchanged write recorded %d events", n)
	}
}
