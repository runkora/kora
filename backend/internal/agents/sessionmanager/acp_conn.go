package sessionmanager

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
)

// acpSDKMaxLine is the longest line acp-go-sdk reads, newline included. A longer
// line closes its connection, so the line filter keeps them away from it.
const acpSDKMaxLine = 10 << 20

// acpEOFMethod is a notification the line filter appends when the adapter's
// stdout ends. The SDK handles notifications in order, so its handler runs after
// every update the adapter wrote before exiting.
const acpEOFMethod = "_runner/eof"

// acpOversizedMethod replaces an agent request the filter had to drop, so the
// SDK answers its id with an error from the runner's handler.
const acpOversizedMethod = "_runner/oversized"

// acpInternalErrorCode is JSON-RPC's internal error, for responses the runner
// makes up in place of ones it could not read.
const acpInternalErrorCode = -32603

const (
	acpReadBuffer   = 64 << 10 // grows by accumulation; most lines fit one read
	acpEnvelopeHead = 64 << 10 // kept from a discarded line to find its id and method
)

var acpEOFLine = []byte(`{"jsonrpc":"2.0","method":"` + acpEOFMethod + `"}` + "\n")

var (
	errACPLineTooLong    = errors.New("acp line exceeds the line cap")
	errACPLineNotShrunk  = errors.New("acp line has no tool output to truncate")
	errACPLineStillLarge = errors.New("acp line still too large after truncating tool output")
)

// acpLineFilter sits between the adapter's stdout and the SDK. Lines that fit
// the SDK pass unchanged; longer ones up to maxLine have their tool output
// truncated. Anything else is dropped so the session survives, with an error
// in its place when it was a response or a request someone waits on.
type acpLineFilter struct {
	src     *bufio.Reader
	pass    int // lines shorter than this pass byte for byte
	maxLine int
	warn    func(msg string, err error)
	// ready holds the first read until the SDK connection has its logger,
	// which the SDK sets without a lock.
	ready chan struct{}

	out   []byte
	err   error
	ended atomic.Bool
}

func newACPLineFilter(r io.Reader, warn func(string, error)) *acpLineFilter {
	return &acpLineFilter{src: bufio.NewReaderSize(r, acpReadBuffer), pass: acpSDKMaxLine, maxLine: maxAgentLineBytes, warn: warn, ready: make(chan struct{})}
}

func (f *acpLineFilter) Read(p []byte) (int, error) {
	<-f.ready
	for len(f.out) == 0 {
		if f.err != nil {
			return 0, f.err
		}
		line, head, err := f.readLine()
		if len(line) > 0 || head != nil {
			f.out = f.filter(line, head)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				f.warn("ACP adapter stdout read failed", err)
			}
			f.out = append(f.out, acpEOFLine...)
			f.err = err
			f.ended.Store(true)
		}
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

// readLine returns the next line with its newline. Past maxLine it keeps only
// the line's head and discards the rest.
func (f *acpLineFilter) readLine() (line, head []byte, err error) {
	for {
		chunk, err := f.src.ReadSlice('\n')
		if head == nil {
			line = append(line, chunk...)
			if len(line) > f.maxLine {
				head, line = line[:min(len(line), acpEnvelopeHead)], nil
			}
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if len(line) > 0 && line[len(line)-1] != '\n' {
				line = append(line, '\n')
			}
			return line, head, err
		}
	}
}

// filter passes, shrinks or replaces one line; head is set for a line past maxLine.
func (f *acpLineFilter) filter(line, head []byte) []byte {
	if head == nil && len(line) < f.pass {
		return line
	}
	var err error
	if head != nil {
		err = fmt.Errorf("%w: longer than %d bytes", errACPLineTooLong, f.maxLine)
	} else {
		var out []byte
		if out, err = shrinkACPLine(line, f.pass); err == nil {
			f.warn("ACP tool output truncated to fit the SDK line limit", nil)
			return out
		}
		err = fmt.Errorf("%d bytes: %w", len(line), err)
		head = line
	}
	f.warn("ACP line dropped", err)
	return acpDroppedLine(head)
}

// acpDroppedLine stands in for a dropped line: an error response for a
// response, so the waiting request ends, and an oversized-request marker for a
// request, so the SDK answers it. A notification is dropped outright.
func acpDroppedLine(head []byte) []byte {
	id, method := acpEnvelope(head)
	if len(id) == 0 || string(id) == "null" {
		return nil
	}
	var msg any = map[string]any{"jsonrpc": "2.0", "id": id, "method": acpOversizedMethod}
	if method == "" {
		msg = map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": acpInternalErrorCode, "message": "response exceeded the line cap"}}
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return append(b, '\n')
}

// acpEnvelope reads the top-level id and method of a JSON-RPC line, stopping at
// the first value it cannot read, as in a truncated head.
func acpEnvelope(b []byte) (id json.RawMessage, method string) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, ""
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return id, method
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return id, method
		}
		switch tok {
		case "id":
			id = v
		case "method":
			_ = json.Unmarshal(v, &method)
		}
	}
	return id, method
}

// shrinkACPLine truncates the tool output of a tool_call or tool_call_update
// line (content and rawOutput strings) until it is shorter than limit.
func shrinkACPLine(line []byte, limit int) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var msg map[string]any
	if err := dec.Decode(&msg); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	params, _ := msg["params"].(map[string]any)
	update, _ := params["update"].(map[string]any)
	kind, _ := update["sessionUpdate"].(string)
	if kind != "tool_call" && kind != "tool_call_update" {
		return nil, errACPLineNotShrunk
	}
	content, rawOutput := update["content"], update["rawOutput"]
	for limitText := limit / 2; limitText >= 1<<10; limitText /= 2 {
		if content != nil {
			update["content"] = capText(content, limitText)
		}
		if rawOutput != nil {
			update["rawOutput"] = capText(rawOutput, limitText)
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(msg); err != nil {
			return nil, fmt.Errorf("encode: %w", err)
		}
		if buf.Len() < limit {
			return buf.Bytes(), nil
		}
	}
	return nil, errACPLineStillLarge
}

// capText returns v with every string longer than limit truncated and marked.
func capText(v any, limit int) any {
	switch t := v.(type) {
	case string:
		if len(t) > limit {
			return strings.ToValidUTF8(t[:limit], "") + fmt.Sprintf("\n[truncated %d bytes]", len(t)-limit)
		}
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = capText(e, limit)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = capText(e, limit)
		}
		return out
	}
	return v
}
