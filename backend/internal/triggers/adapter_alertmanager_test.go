package triggers

import (
	"errors"
	"net/http"
	"testing"
)

// firingAlertmanagerBody is a canonical Alertmanager v4 payload for a firing
// alert group, cribbed from the Prometheus docs (webhook_config). Used across
// several tests.
const firingAlertmanagerBody = `{
	"version":"4",
	"groupKey":"{}:{alertname=\"HighCPU\"}",
	"status":"firing",
	"receiver":"kora",
	"externalURL":"https://alertmanager.example.com",
	"groupLabels":{"alertname":"HighCPU"},
	"commonLabels":{"alertname":"HighCPU","severity":"critical","service":"api"},
	"commonAnnotations":{"summary":"CPU above 90%","description":"api pod has been >90% CPU for 5m"},
	"alerts":[{"status":"firing","labels":{"alertname":"HighCPU","instance":"api-0"},"annotations":{"summary":"CPU above 90%"},"startsAt":"2026-07-21T10:00:00Z","endsAt":"0001-01-01T00:00:00Z","generatorURL":"https://prometheus.example.com/g?e=1","fingerprint":"abc123"}]
}`

func TestAlertmanagerAdapter_Verify(t *testing.T) {
	a := &alertmanagerAdapter{}
	secret := "shh-alerts"
	body := []byte(firingAlertmanagerBody)

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer " + secret}}
		if err := a.Verify(secret, h, body); err != nil {
			t.Fatalf("expected valid bearer, got %v", err)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		if err := a.Verify(secret, http.Header{}, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong scheme", func(t *testing.T) {
		h := http.Header{"Authorization": {"Basic " + secret}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer nope"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer "}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong length token", func(t *testing.T) {
		// hmac.Equal returns false immediately on length mismatch. That leaks the
		// stored secret's length via timing, which is acceptable here because
		// generateSecret() emits a fixed-length token, so the length is not
		// itself a secret. A shorter or longer token must still be rejected.
		h := http.Header{"Authorization": {"Bearer " + secret + "x"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})
}

func TestAlertmanagerAdapter_HeaderExtraction(t *testing.T) {
	a := &alertmanagerAdapter{}
	firing := []byte(firingAlertmanagerBody)

	if got := a.Provider(); got != ProviderAlertmanager {
		t.Errorf("Provider = %q, want %q", got, ProviderAlertmanager)
	}
	if got := a.EventType(nil, firing); got != "alert.firing" {
		t.Errorf("EventType(firing) = %q, want alert.firing", got)
	}
	resolved := []byte(`{"groupKey":"g","status":"resolved","alerts":[]}`)
	if got := a.EventType(nil, resolved); got != "alert.resolved" {
		t.Errorf("EventType(resolved) = %q, want alert.resolved", got)
	}
	// Allow-list guard: unknown statuses do NOT become their own event type.
	// A trigger author who accidentally typed status:"unknown" (or a future
	// Alertmanager release that adds a new status) must not silently gate on
	// "alert.unknown".
	if got := a.EventType(nil, []byte(`{"status":"unknown"}`)); got != "" {
		t.Errorf("EventType(unknown) = %q, want empty", got)
	}
	if got := a.EventType(nil, []byte(`{"status":"pending"}`)); got != "" {
		t.Errorf("EventType(pending) = %q, want empty", got)
	}
	if got := a.EventType(nil, []byte(`{"groupKey":"g"}`)); got != "" {
		t.Errorf("EventType(no status) = %q, want empty", got)
	}
	if got := a.EventType(nil, []byte(`not json`)); got != "" {
		t.Errorf("EventType(garbage) = %q, want empty", got)
	}
}

func TestAlertmanagerAdapter_DeliveryID(t *testing.T) {
	a := &alertmanagerAdapter{}
	body1 := []byte(firingAlertmanagerBody)
	body2 := []byte(firingAlertmanagerBody) // byte-identical replay
	// Same body → same id (dedupes repeat_interval reminders).
	if a.DeliveryID(nil, body1) != a.DeliveryID(nil, body2) {
		t.Error("expected identical bodies to hash identically")
	}
	// A resolved payload with the same groupKey must produce a different id.
	resolved := []byte(`{"version":"4","groupKey":"{}:{alertname=\"HighCPU\"}","status":"resolved","receiver":"kora","alerts":[]}`)
	if a.DeliveryID(nil, body1) == a.DeliveryID(nil, resolved) {
		t.Error("firing and resolved for the same groupKey must hash differently")
	}
	// Two Alertmanager instances sharing groupKey+status+body but with
	// different receiver values must NOT collide (the whole point of including
	// receiver in the hash). This is the multi-sender-per-source case flagged
	// in the ADR's B1 Cons treatment.
	senderA := []byte(`{"groupKey":"g","status":"firing","receiver":"team-a","alerts":[]}`)
	senderB := []byte(`{"groupKey":"g","status":"firing","receiver":"team-b","alerts":[]}`)
	if a.DeliveryID(nil, senderA) == a.DeliveryID(nil, senderB) {
		t.Error("deliveries from different receivers must hash differently even with identical groupKey+status")
	}
	// A body with no groupKey/status must return "" so Ingest fails closed.
	if got := a.DeliveryID(nil, []byte(`{"status":"firing"}`)); got != "" {
		t.Errorf("DeliveryID(no groupKey) = %q, want empty", got)
	}
	if got := a.DeliveryID(nil, []byte(`{"groupKey":"g"}`)); got != "" {
		t.Errorf("DeliveryID(no status) = %q, want empty", got)
	}
	if got := a.DeliveryID(nil, []byte(`not json`)); got != "" {
		t.Errorf("DeliveryID(garbage) = %q, want empty", got)
	}
}

// The schema's paths resolve against a real Alertmanager body.
func TestAlertmanagerAdapter_SchemaPathsResolve(t *testing.T) {
	c, err := BuildCatalog((&alertmanagerAdapter{}).Schema())
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	doc := decodeBody([]byte(firingAlertmanagerBody))
	want := map[string]string{
		"status":                    "firing",
		"receiver":                  "kora",
		"commonLabels.severity":     "critical",
		"commonAnnotations.summary": "CPU above 90%",
		"alerts.0.status":           "firing",
	}
	for path, value := range want {
		if got := resolvePath(doc, path); got != value {
			t.Errorf("resolve %s = %q, want %q", path, got, value)
		}
	}
	paths := map[string]Field{}
	for _, f := range c.Fields("alert.firing") {
		paths[f.Path] = f
	}
	for _, p := range []string{"status", "receiver", "alerts.0.status", "alerts.0.fingerprint"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("catalog is missing %s", p)
		}
	}
	for _, p := range []string{"commonLabels.<name>", "alerts.0.annotations.<name>"} {
		if !paths[p].Pattern {
			t.Errorf("%s should be a pattern field", p)
		}
	}
}
