package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

type alertmanagerAdapter struct{}

func (a *alertmanagerAdapter) Provider() string { return ProviderAlertmanager }

// Verify authenticates an inbound Alertmanager delivery. Alertmanager's
// webhook_config has no body-signature scheme; authentication is delegated to
// http_config, which supports bearer tokens on Authorization. See
// docs/adr/2026-07-21-alertmanager-webhook-adapter.md for the rationale and
// alternatives considered.
//
// The comparison uses hmac.Equal for constant-time comparison of equal-length
// slices. hmac.Equal returns false immediately on a length mismatch, which
// leaks the length of one operand via timing — this is acceptable here because
// generateSecret() produces a fixed-length (43-char base64) secret, so its
// length is not itself a secret.
func (a *alertmanagerAdapter) Verify(secret string, headers http.Header, _ []byte) error {
	auth := headers.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ErrSignatureInvalid
	}
	token := strings.TrimPrefix(auth, prefix)
	if !hmac.Equal([]byte(token), []byte(secret)) {
		return ErrSignatureInvalid
	}
	return nil
}

// DeliveryID derives a content-hash dedup key for an Alertmanager delivery:
// sha256(receiver || 0 || groupKey || 0 || status || 0 || body), hex-encoded.
// Alertmanager emits no per-delivery id, so redeliveries triggered by
// repeat_interval re-post identical bodies and dedupe automatically; a
// firing → resolved transition changes status and produces a distinct hash.
//
// The receiver field is included so two Alertmanager instances that happen to
// share a groupKey (uncommon but possible in shared-tenant setups) do not
// collide across senders.
//
// The 0-byte delimiters are anti-collision separators: without them,
// (receiver="a", groupKey="bc") would hash the same as (receiver="ab",
// groupKey="c") — the delimiters make each field's boundary observable to the
// hash.
//
// Returns "" if the top-level groupKey/status cannot be parsed, in which case
// Ingest rejects the delivery fail-closed (ErrMissingDeliveryID) — preserving
// the platform's replay defense.
func (a *alertmanagerAdapter) DeliveryID(_ http.Header, body []byte) string {
	var p struct {
		GroupKey string `json:"groupKey"`
		Status   string `json:"status"`
		Receiver string `json:"receiver"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	if p.GroupKey == "" || p.Status == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(p.Receiver))
	h.Write([]byte{0})
	h.Write([]byte(p.GroupKey))
	h.Write([]byte{0})
	h.Write([]byte(p.Status))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// EventType derives the event type from the payload's top-level `status`,
// namespaced as "alert.firing" or "alert.resolved" so it never collides with
// other providers' event-type spaces. Only the two Alertmanager-documented
// statuses are surfaced; anything else (including missing/empty status)
// returns "" and matches no trigger. This is a deliberate allow-list — an
// unknown status must not silently be exposed as its own event type where a
// trigger author could accidentally gate on it.
func (a *alertmanagerAdapter) EventType(_ http.Header, body []byte) string {
	var p struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	switch p.Status {
	case "firing", "resolved":
		return "alert." + p.Status
	default:
		return ""
	}
}

//go:embed schemas/alertmanager.json
var alertmanagerSchema []byte

// Schema returns the hand-written Alertmanager webhook schema.
func (a *alertmanagerAdapter) Schema() []byte { return alertmanagerSchema }
