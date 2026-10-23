package triggers

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Discord message types that carry a user's text: default and reply.
const (
	discordMessageDefault = 0
	discordMessageReply   = 19
)

// discordAdapter reads the Gateway messages the GatewayManager wraps as
// {d, kora}. It exposes no HTTP ingest. See docs/adr/2026-10-23-discord-gateway-chat-source.md.
type discordAdapter struct{}

// discordEnvelope is the part of the stored body the adapter reads.
type discordEnvelope struct {
	D struct {
		ID        string `json:"id"`
		Type      int    `json:"type"`
		GuildID   string `json:"guild_id"`
		WebhookID string `json:"webhook_id"`
		Author    struct {
			Bot bool `json:"bot"`
		} `json:"author"`
		Mentions []struct {
			ID string `json:"id"`
		} `json:"mentions"`
	} `json:"d"`
	Kora koraContext `json:"kora"`
}

func parseDiscordEnvelope(body []byte) (e discordEnvelope, ok bool) {
	return e, json.Unmarshal(body, &e) == nil
}

func (a *discordAdapter) Provider() string { return providerDiscord }

// Verify always fails: a Discord message arrives over the Gateway, never as a signed request.
func (a *discordAdapter) Verify(string, http.Header, []byte) error { return ErrSignatureInvalid }

// DeliveryID returns the message id.
func (a *discordAdapter) DeliveryID(_ http.Header, body []byte) string {
	e, _ := parseDiscordEnvelope(body)
	return e.D.ID
}

// EventType returns "" for every bot and webhook message, so the agent's own replies never match.
func (a *discordAdapter) EventType(_ http.Header, body []byte) string {
	e, ok := parseDiscordEnvelope(body)
	d := e.D
	if !ok || d.Author.Bot || d.WebhookID != "" || (d.Type != discordMessageDefault && d.Type != discordMessageReply) {
		return ""
	}
	if d.GuildID == "" {
		return "direct_message"
	}
	for _, m := range d.Mentions {
		if e.Kora.BotUserID != "" && m.ID == e.Kora.BotUserID {
			return "mention"
		}
	}
	return ""
}

// Schema is empty: the registry gives a chat adapter the shared chat catalog.
func (a *discordAdapter) Schema() []byte { return nil }

func (a *discordAdapter) providerIssuesSecret() {}

// discordConversationTypes maps a channel type to conversation.type.
var discordConversationTypes = map[string]string{"0": "channel", "5": "channel", "1": "dm", "3": "group"}

// ChatEvent maps a stored body to the chat document. A message outside a thread is its own thread.
func (a *discordAdapter) ChatEvent(_ string, doc any) (map[string]any, bool) {
	user := a.NormalizeID(resolvePath(doc, "d.author.id"))
	msgID := resolvePath(doc, "d.id")
	guild := resolvePath(doc, "d.guild_id")
	conversation, thread, startedBy := resolvePath(doc, "d.channel_id"), msgID, user
	channelType := resolvePath(doc, "d.channel_type")
	if t := resolvePath(doc, "kora.thread.id"); t != "" {
		conversation = resolvePath(doc, "kora.thread.parent_id")
		thread = t
		startedBy = a.NormalizeID(resolvePath(doc, "kora.thread.owner_id"))
		channelType = resolvePath(doc, "kora.thread.parent_type")
	} else if guild == "" {
		channelType = "1"
	}
	if user == "" || conversation == "" || thread == "" {
		return nil, false
	}
	return map[string]any{
		"text":         resolvePath(doc, "d.content"),
		"user":         map[string]any{"id": user},
		"conversation": map[string]any{"id": conversation, "type": discordConversationTypes[channelType]},
		"thread":       map[string]any{"id": thread, "started_by": map[string]any{"id": startedBy}},
		"message":      map[string]any{"id": msgID},
		"workspace":    map[string]any{"id": guild},
		"platform":     providerDiscord,
	}, true
}

// Sender reads d.author.id, the Discord user snowflake.
func (a *discordAdapter) Sender(_ string, body any) (Sender, bool) {
	id := a.NormalizeID(resolvePath(body, "d.author.id"))
	if id == "" {
		return Sender{}, false
	}
	return Sender{ID: id}, true
}

// NormalizeID trims space: a snowflake has no case.
func (a *discordAdapter) NormalizeID(id string) string { return strings.TrimSpace(id) }

// Roles is empty: Discord roles are not offered in the sender restriction.
func (a *discordAdapter) Roles() []SenderRole { return []SenderRole{} }
