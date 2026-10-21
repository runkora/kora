package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
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
// docs/adr/2026-10-20-chat-command-triggers.md.
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

// EventType maps the two subscribed events to the chat event types and returns
// "" for anything else, including every app message, so the agent's own replies never match.
func (a *slackAdapter) EventType(_ http.Header, body []byte) string {
	e := parseSlackEnvelope(body)
	if e.Type != "event_callback" || e.Event.BotID != nil || e.Event.Subtype == "bot_message" {
		return ""
	}
	switch {
	case e.Event.Type == "app_mention":
		return "mention"
	case e.Event.Type == "message" && e.Event.ChannelType == "im" &&
		(e.Event.Subtype == "" || e.Event.Subtype == "file_share"):
		return "direct_message"
	}
	return ""
}

// Schema is empty: the registry gives a chat adapter the shared chat catalog.
func (a *slackAdapter) Schema() []byte { return nil }

func (a *slackAdapter) providerIssuesSecret() {}

// slackConversationTypes maps event.channel_type to conversation.type.
var slackConversationTypes = map[string]string{"channel": "channel", "group": "private", "im": "dm", "mpim": "group"}

// ChatEvent maps a Slack event to the chat document. A top-level message is its own thread. message.id is the event's own ts.
func (a *slackAdapter) ChatEvent(_ string, doc any) (map[string]any, bool) {
	user := a.NormalizeID(resolvePath(doc, "event.user"))
	conversation := resolvePath(doc, "event.channel")
	thread := resolvePath(doc, "event.thread_ts")
	if thread == "" {
		thread = resolvePath(doc, "event.ts")
	}
	// A thread root carries thread_ts without parent_user_id, so the sender started it.
	startedBy := a.NormalizeID(resolvePath(doc, "event.parent_user_id"))
	if startedBy == "" {
		startedBy = user
	}
	// app_mention carries no channel_type; a D channel id is a DM, and C and G ids cannot tell public from private.
	convType := slackConversationTypes[resolvePath(doc, "event.channel_type")]
	if convType == "" && strings.HasPrefix(conversation, "D") {
		convType = "dm"
	}
	if user == "" || conversation == "" || thread == "" {
		return nil, false
	}
	return map[string]any{
		"text":         resolvePath(doc, "event.text"),
		"user":         map[string]any{"id": user},
		"conversation": map[string]any{"id": conversation, "type": convType},
		"thread":       map[string]any{"id": thread, "started_by": map[string]any{"id": startedBy}},
		"message":      map[string]any{"id": resolvePath(doc, "event.ts")},
		"workspace":    map[string]any{"id": resolvePath(doc, "team_id")},
		"platform":     ProviderSlack,
	}, true
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
