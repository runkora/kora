package toolgateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testLogger() logging.Logger { return logging.NewLogger() }

func TestLoadServers(t *testing.T) {
	ctx := context.Background()
	log := testLogger()

	t.Run("empty path → not mounted", func(t *testing.T) {
		servers, mounted, err := loadServers(ctx, "", log)
		if err != nil || mounted || servers != nil {
			t.Errorf("empty path → (%v,%v,%v), want (nil,false,nil)", servers, mounted, err)
		}
	})

	t.Run("absent file → not mounted", func(t *testing.T) {
		_, mounted, err := loadServers(ctx, filepath.Join(t.TempDir(), "nope.json"), log)
		if err != nil || mounted {
			t.Errorf("absent file → mounted=%v err=%v, want false,nil", mounted, err)
		}
	})

	t.Run("empty content → mounted, zero servers", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "servers.json")
		if err := os.WriteFile(p, []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
		servers, mounted, err := loadServers(ctx, p, log)
		if err != nil || !mounted || len(servers) != 0 {
			t.Errorf("empty array → (%v,%v,%v), want ([],true,nil)", servers, mounted, err)
		}
	})

	t.Run("parses entries", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "servers.json")
		body := `[{"tenant_id":"","name":"orbit","endpoint":"http://kora-mcp.svc/mcp","transport":"streamable-http","reintrospect_interval_seconds":900,"caller_identity":"forward"}]`
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		servers, mounted, err := loadServers(ctx, p, log)
		if err != nil || !mounted || len(servers) != 1 {
			t.Fatalf("parse → (%v,%v,%v)", servers, mounted, err)
		}
		s := servers[0]
		if s.Name != "orbit" || s.Endpoint != "http://kora-mcp.svc/mcp" || s.CallerIdentity != "forward" || s.ReintrospectIntervalSeconds != 900 {
			t.Errorf("parsed entry wrong: %+v", s)
		}
	})

	t.Run("malformed JSON → error", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "servers.json")
		if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadServers(ctx, p, log); err == nil {
			t.Error("malformed JSON should error")
		}
	})
}

// TestAllSeenOrRetained covers the prune-guard predicate: every listed server
// must be OK or Degraded-with-tools; a never-seen or Degraded-without-tools
// server defers the prune.
func TestAllSeenOrRetained(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())

	ok := Server{TenantID: "", Name: "ok"}
	degRetained := Server{TenantID: "", Name: "deg-retained"}
	degEmpty := Server{TenantID: "", Name: "deg-empty"}
	pending := Server{TenantID: "", Name: "pending"}

	// ok: introspected successfully with one tool.
	i.recordResult(ctx, ok, serverKey(ok.TenantID, ok.Name), []Tool{{Name: "t"}}, nil)
	// deg-retained: was OK once (has tools), then failed → degraded, tools kept.
	i.recordResult(ctx, degRetained, serverKey(degRetained.TenantID, degRetained.Name), []Tool{{Name: "t"}}, nil)
	i.recordResult(ctx, degRetained, serverKey(degRetained.TenantID, degRetained.Name), nil, errors.New("boom"))
	// deg-empty: failed on first contact → degraded, no tools.
	i.recordResult(ctx, degEmpty, serverKey(degEmpty.TenantID, degEmpty.Name), nil, errors.New("boom"))

	if !i.AllSeenOrRetained([]Server{ok}) {
		t.Error("ok server should satisfy prune-guard")
	}
	if !i.AllSeenOrRetained([]Server{ok, degRetained}) {
		t.Error("ok + degraded-with-tools should satisfy prune-guard")
	}
	if i.AllSeenOrRetained([]Server{ok, degEmpty}) {
		t.Error("degraded-without-tools should defer prune")
	}
	if i.AllSeenOrRetained([]Server{ok, pending}) {
		t.Error("never-seen server should defer prune")
	}

	// Degraded-with-tools must still expose its retained tools in the snapshot.
	for _, s := range i.Snapshot() {
		if s.Server.Name == "deg-retained" {
			if s.State != StateDegraded || len(s.Tools) != 1 {
				t.Errorf("deg-retained snapshot = state %q tools %d, want degraded + 1 retained", s.State, len(s.Tools))
			}
		}
	}
}

func TestMarshalSchema(t *testing.T) {
	if got := string(marshalSchema(nil)); got != "{}" {
		t.Errorf("nil schema = %q, want {}", got)
	}
	if got := string(marshalSchema(map[string]any{"type": "object"})); got != `{"type":"object"}` {
		t.Errorf("schema = %q", got)
	}
}
