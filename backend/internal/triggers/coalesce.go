package triggers

import (
	"fmt"
	"strings"
	"time"
)

// Coalescing bounds. The window is capped so a fire is never held for longer
// than a person would still call it a reaction to the event. The template cap
// matches the rendered key's, since a template with no placeholders renders
// to itself.
const (
	maxCoalesceWindowSeconds  = 300
	maxCoalesceKeyTemplateLen = MaxCoalesceKeyBytes
)

// Enabled reports whether fires of this trigger are held and coalesced. Both
// halves are required: a key with no window, or a window with no key, is off.
func (c *CoalesceConfig) Enabled() bool {
	return c != nil && c.Key != "" && c.WindowSeconds > 0
}

// Window is how long a fire with a key waits before it can dispatch.
func (c *CoalesceConfig) Window() time.Duration {
	return time.Duration(c.WindowSeconds) * time.Second
}

// normalizeCoalesce validates a coalesce setting and returns what to store: nil
// when it is off, so an empty object clears the setting rather than persisting
// a no-op.
func normalizeCoalesce(c *CoalesceConfig) (*CoalesceConfig, error) {
	if c == nil || (c.Key == "" && c.WindowSeconds == 0) {
		return nil, nil
	}
	switch {
	case c.Key == "":
		return nil, fmt.Errorf("%w: coalesce.key is required when coalesce.window_seconds is set", ErrValidation)
	case c.WindowSeconds == 0:
		return nil, fmt.Errorf("%w: coalesce.window_seconds is required when coalesce.key is set", ErrValidation)
	case c.WindowSeconds < 0 || c.WindowSeconds > maxCoalesceWindowSeconds:
		return nil, fmt.Errorf("%w: coalesce.window_seconds must be between 1 and %d", ErrValidation, maxCoalesceWindowSeconds)
	case len(c.Key) > maxCoalesceKeyTemplateLen:
		return nil, fmt.Errorf("%w: coalesce.key must be %d bytes or fewer", ErrValidation, maxCoalesceKeyTemplateLen)
	}
	if err := validateFieldKeys(c.Key, nil); err != nil {
		return nil, err
	}
	return &CoalesceConfig{Key: c.Key, WindowSeconds: c.WindowSeconds}, nil
}

// validateCoalesceOverflow refuses coalescing under an overflow mode that would
// fight it. Replace and refuse already decide what happens to a waiting fire,
// and a second rule on top of either would have two answers for one fire.
func validateCoalesceOverflow(c *CoalesceConfig, overflow string) error {
	if c.Enabled() && overflow != OverflowWait {
		return fmt.Errorf("%w: coalesce requires overflow %q, not %q", ErrValidation, OverflowWait, overflow)
	}
	return nil
}

// coalesceKeyTemplate is the key template for referencedKeys, or "" when
// coalescing is off.
func (c *CoalesceConfig) keyTemplate() string {
	if !c.Enabled() {
		return ""
	}
	return c.Key
}

// renderCoalesceKey renders the trigger's key for one fire.
//
// ok is false when the fire must not coalesce: coalescing is off, the render is
// empty, or the render is over the cap. tooLong tells the last of those apart,
// because it is the one worth recording. The render is trimmed first, so a
// blank render is empty and a partial render such as "acme/widgets#" is a key.
func renderCoalesceKey(t Trigger, fields map[string]string) (key string, ok, tooLong bool) {
	if !t.Coalesce.Enabled() {
		return "", false, false
	}
	key = strings.TrimSpace(renderTemplate(t.Coalesce.Key, fields))
	switch {
	case key == "":
		return "", false, false
	case len(key) > MaxCoalesceKeyBytes:
		return "", false, true
	}
	return key, true, false
}
