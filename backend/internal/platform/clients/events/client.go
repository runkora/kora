// Package events is the typed client for the kora-events internal stream,
// which kora-triggers reads to fire event triggers.
//
// See docs/adr/2026-11-06-event-spine.md, section 7.
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

// Stream calls handle with each event after seq, in seq order, until the
// stream ends, ctx ends or handle fails. It returns handle's error unwrapped.
// A stream kora-events closes at token expiry returns nil.
func (c *Client) Stream(ctx context.Context, after int64, handle func(Record) error) error {
	tok, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errToken, err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+tok)
	header.Set("Accept", "text/event-stream")
	_, span, resp, err := c.Do(ctx, httpclient.Request{
		Method: http.MethodGet, BaseURL: c.baseURL, Path: "/internal/v1/events/stream",
		Query: url.Values{"after": {strconv.FormatInt(after, 10)}}, Header: header,
	})
	defer span.End()
	if err != nil {
		return fmt.Errorf("open event stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
