package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// slackSignatureWindow is how far a request timestamp may be from the server
// clock, in either direction, before the request is refused as a replay.
const slackSignatureWindow = 5 * time.Minute

// slackAdapter receives Slack's HTTP Events API. See
// docs/adr/2026-10-20-slack-trigger-source.md.
type slackAdapter struct {
	// now is the clock for the timestamp window; nil means time.Now.
	now func() time.Time
}

// slackEnvelope is the part of an Events API body the adapter reads.
type slackEnvelope struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	EventID   string `json:"event_id"`
	Event     struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		ChannelType string `json:"channel_type"`
		// BotID is any so a value of an unexpected type still marks an app message.
		BotID any `json:"bot_id"`
	} `json:"event"`
}

func parseSlackEnvelope(body []byte) slackEnvelope {
	var e slackEnvelope
	_ = json.Unmarshal(body, &e)
	return e
}

func (a *slackAdapter) Provider() string { return ProviderSlack }

// Verify implements Slack's v0 signing scheme: HMAC-SHA256 over
// "v0:{timestamp}:{body}", compared in constant time with X-Slack-Signature.
func (a *slackAdapter) Verify(secret string, headers http.Header, body []byte) error {
	ts := headers.Get("X-Slack-Request-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrSignatureInvalid
	}
	now := time.Now
	if a.now != nil {
		now = a.now
	}
	if age := now().Sub(time.Unix(sec, 0)); age > slackSignatureWindow || age < -slackSignatureWindow {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(headers.Get("X-Slack-Signature"))) {
		return ErrSignatureInvalid
	}
	return nil
}

// Handshake answers the url_verification request Slack sends to confirm a request URL.
func (a *slackAdapter) Handshake(body []byte) (map[string]string, bool) {
	e := parseSlackEnvelope(body)
	if e.Type != "url_verification" {
		return nil, false
	}
	return map[string]string{"challenge": e.Challenge}, true
}

// DeliveryID returns event_id, which Slack keeps across its retries of an event.
func (a *slackAdapter) DeliveryID(_ http.Header, body []byte) string {
	return parseSlackEnvelope(body).EventID
}

// EventType names the two subscribed events and returns "" for anything else,
// including every message an app posted, so the agent's own replies never match a trigger.
func (a *slackAdapter) EventType(_ http.Header, body []byte) string {
	e := parseSlackEnvelope(body)
	if e.Type != "event_callback" || e.Event.BotID != nil || e.Event.Subtype == "bot_message" {
		return ""
	}
	switch {
	case e.Event.Type == "app_mention":
		return "app_mention"
	case e.Event.Type == "message" && e.Event.ChannelType == "im" &&
		(e.Event.Subtype == "" || e.Event.Subtype == "file_share"):
		return "message.im"
	}
	return ""
}

//go:embed schemas/slack.json
var slackSchema []byte

// Schema returns the hand-written Slack Events API schema.
func (a *slackAdapter) Schema() []byte { return slackSchema }

func (a *slackAdapter) providerIssuesSecret() {}

// DerivedFields returns thread: event.thread_ts for a reply, else event.ts,
// so a routing join_key can name a thread from either kind of message.
func (a *slackAdapter) DerivedFields(_ string, doc any) map[string]string {
	thread := resolvePath(doc, "event.thread_ts")
	if thread == "" {
		thread = resolvePath(doc, "event.ts")
	}
	if thread == "" {
		return nil
	}
	return map[string]string{"thread": thread}
}

// Sender reads event.user, the Slack user id.
func (a *slackAdapter) Sender(_ string, body any) (Sender, bool) {
	user := resolvePath(body, "event.user")
	if user == "" {
		return Sender{}, false
	}
	return Sender{ID: a.NormalizeID(user)}, true
}

// NormalizeID uppercases a user id, the form Slack issues them in.
func (a *slackAdapter) NormalizeID(id string) string { return strings.ToUpper(id) }

// Roles is empty: Slack reports no role a restriction could name.
func (a *slackAdapter) Roles() []SenderRole { return []SenderRole{} }
