// Package events is the typed client for the kora-events internal stream.
// kora-triggers reads it to fire event triggers, and kora-agents reads it for
// the live runs list.
//
// See docs/adr/2026-11-06-event-spine.md, section 7, and
// docs/adr/2026-11-25-runs-list-live-stream.md, decision 4.
package events

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const tracerName = "github.com/runkora/kora/backend/internal/platform/clients/events"

// maxEventBytes bounds one SSE line. An envelope is far smaller.
const maxEventBytes = 4 << 20

var (
	// ErrUnexpectedStatus is a stream request kora-events did not accept.
	ErrUnexpectedStatus = errors.New("unexpected status from kora-events")
	// ErrEventRemoved is a resume seq whose event the tenant no longer holds.
	// The caller reloads its state and streams live.
	ErrEventRemoved = errors.New("event removed from the log")
	// ErrBadEvent is an SSE message that does not decode.
	ErrBadEvent = errors.New("event stream message does not decode")
	errToken    = errors.New("no token for kora-events")
)

// Record is one event in the log: its seq and its CloudEvents envelope.
type Record struct {
	Seq   int64           `json:"seq"`
	Event json.RawMessage `json:"event"`
}

// Client reads the internal event stream as this binary's SYSTEM identity.
type Client struct {
	*httpclient.Base
	baseURL string
	token   func(context.Context) (string, error)
}

// NewClient streams from baseURL with the platform token token returns.
// The stream has no client timeout. The caller's context ends it.
func NewClient(baseURL string, token func(context.Context) (string, error), logger logging.Logger) *Client {
	return &Client{Base: httpclient.New(tracerName, logger, 0), baseURL: baseURL, token: token}
}

// Query selects the events a stream reads.
type Query struct {
	// After is the seq the stream resumes after. Live ignores it.
	After int64
	// Live starts the stream at kora-events' current position with no replay.
	Live bool
	// Tenant limits the stream to one tenant. Empty reads every tenant.
	Tenant string
	// Categories limits the stream to these event categories, such as agents.run.
	Categories []string
}

// Stream calls handle with each event q selects, in seq order, until the
// stream ends, ctx ends or handle fails. It returns handle's error unwrapped.
// A stream kora-events closes at token expiry returns nil. With a tenant, it
// returns ErrEventRemoved when retention removed the tenant's event at q.After.
func (c *Client) Stream(ctx context.Context, q Query, handle func(Record) error) error {
	tok, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errToken, err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+tok)
	header.Set("Accept", "text/event-stream")
	query := url.Values{"category": q.Categories}
	if !q.Live {
		query.Set("after", strconv.FormatInt(q.After, 10))
	}
	if q.Tenant != "" {
		query.Set("tenant", q.Tenant)
	}
	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/internal/v1/events/stream",
		Query: query, Header: header,
	})
	defer span.End()
	if err != nil {
		return fmt.Errorf("open event stream: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusGone:
		return c.HandleErrorResponse(span, resp, ErrEventRemoved)
	default:
		return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxEventBytes)
	var data []byte
	for scanner.Scan() {
		line := scanner.Bytes()
		switch {
		case bytes.HasPrefix(line, []byte("data:")):
			data = append(data, bytes.TrimSpace(line[len("data:"):])...)
		case len(line) == 0 && len(data) > 0:
			var rec Record
			err := json.Unmarshal(data, &rec)
			data = data[:0]
			// A message that never decodes would stop the stream at the same seq forever.
			if err != nil {
				c.Logger().Error(ctx, "skipping an event stream message that does not decode",
					logging.Field{Key: "error", Value: fmt.Errorf("%w: %w", ErrBadEvent, err).Error()})
				continue
			}
			if err := handle(rec); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read event stream: %w", err)
	}
	return nil
}
