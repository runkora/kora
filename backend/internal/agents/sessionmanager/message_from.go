package sessionmanager

import (
	"strings"
	"unicode"
)

// maxFromFieldLen caps each field of a message header.
const maxFromFieldLen = 200

// MessageFrom is who sent a message, as kora-agents reports it.
// Kind is "person", "agent" or "trigger".
type MessageFrom struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	DeliveryID string `json:"delivery_id"`
}

// header is the line, led by the product name, the agent reads before the message, ending in a blank line.
// It is empty when there is no sender. Fields are cleaned so a display name
// cannot end the line or forge a second header.
func (f *MessageFrom) header(product string) string {
	if f == nil {
		return ""
	}
	id, name, email := cleanFromField(f.ID), cleanFromField(f.Name), cleanFromField(f.Email)
	var b strings.Builder
	b.WriteString("[" + product + ": from ")
	switch f.Kind {
	case "person":
		b.WriteString(name)
		if email != "" {
			b.WriteString(" <" + email + ">")
		}
		b.WriteString(" (user id " + id + ")")
	case "agent":
		b.WriteString("agent " + name + " (id " + id + ")")
	case "trigger":
		if id == "" && name == "" {
			b.WriteString("trigger (unidentified)")
			break
		}
		b.WriteString("trigger " + name + " (id " + id + ")")
		if d := cleanFromField(f.DeliveryID); d != "" {
			b.WriteString(", delivery " + d)
		}
	default:
		return ""
	}
	b.WriteString("]\n\n")
	return b.String()
}

// cleanFromField drops control, format and line or paragraph separator characters and the
// delimiters the header uses (brackets, parentheses, angle brackets and commas), trims the
// result and cuts it to maxFromFieldLen runes.
func cleanFromField(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("[]()<>,", r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxFromFieldLen {
		s = string(r[:maxFromFieldLen])
	}
	return s
}
