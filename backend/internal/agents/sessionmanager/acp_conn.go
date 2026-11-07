package sessionmanager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// defaultACPMaxLine is the longest JSON-RPC line acpConn will parse. A tool
// result larger than the old 1 MiB scanner buffer must not kill the read loop;
// a line past this cap is dropped and the loop carries on.
const defaultACPMaxLine = 16 << 20

// errACPClosed fails calls that were still waiting when the agent's stdout ended.
var errACPClosed = errors.New("acp: connection closed")

// acpRPCError is a JSON-RPC 2.0 error object.
type acpRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *acpRPCError) Error() string {
	return fmt.Sprintf("acp rpc error %d: %s", e.Code, e.Message)
}

// acpMsg is one JSON-RPC 2.0 message. It is a request if it has an ID and a
// method, a notification if it has a method only, and a response otherwise.
type acpMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}

// acpHandlers receive what the agent sends. Notify and Closed run on the read
// loop, in wire order, so a handler sees every notification that came before a
// response. Request runs on its own goroutine, so it may block.
type acpHandlers struct {
	Request func(id json.RawMessage, method string, params json.RawMessage)
	Notify  func(method string, params json.RawMessage)
	Closed  func()
	Logf    func(msg string, err error)
}

type acpCall struct {
	ch chan acpCallResult // handshake calls
	cb func(json.RawMessage, error)
}

type acpCallResult struct {
	result json.RawMessage
	err    error
}

// acpConn is newline-delimited JSON-RPC 2.0 over an agent's stdio: request and
// response correlation, agent-to-client requests, and notifications.
type acpConn struct {
	w       io.Writer
	wmu     sync.Mutex
	maxLine int
	h       acpHandlers

	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]*acpCall
	closed  bool
	done    chan struct{}
}

func newACPConn(w io.Writer, maxLine int, h acpHandlers) *acpConn {
	if maxLine <= 0 {
		maxLine = defaultACPMaxLine
	}
	return &acpConn{w: w, maxLine: maxLine, h: h, pending: make(map[int64]*acpCall), done: make(chan struct{})}
}

// start runs the read loop on r until it ends.
func (c *acpConn) start(r io.Reader) { go c.readLoop(r) }

// Done is closed after the read loop has ended and Closed has run.
func (c *acpConn) Done() <-chan struct{} { return c.done }

func (c *acpConn) write(msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal jsonl: %w", err)
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(line, '\n'))
	return err
}

func (c *acpConn) register(call *acpCall) (int64, error) {
	id := c.nextID.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errACPClosed
	}
	c.pending[id] = call
	return id, nil
}

func (c *acpConn) unregister(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *acpConn) send(id int64, method string, params any) error {
	return c.write(acpMsg{JSONRPC: "2.0", ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: mustMarshal(params)})
}

// call sends a request and waits for its response, or for ctx.
func (c *acpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	call := &acpCall{ch: make(chan acpCallResult, 1)}
	id, err := c.register(call)
	if err != nil {
		return nil, err
	}
	if err := c.send(id, method, params); err != nil {
		c.unregister(id)
		return nil, fmt.Errorf("write rpc request %q: %w", method, err)
	}
	select {
	case res := <-call.ch:
		if res.err != nil {
			return nil, fmt.Errorf("rpc %q: %w", method, res.err)
		}
		return res.result, nil
	case <-ctx.Done():
		c.unregister(id)
		return nil, fmt.Errorf("rpc %q: %w", method, ctx.Err())
	}
}

// callAsync sends a request and returns once it is written. cb runs on the read
// loop when the response arrives, after every notification that preceded it.
// If the connection ends first, cb gets errACPClosed.
func (c *acpConn) callAsync(method string, params any, cb func(json.RawMessage, error)) error {
	id, err := c.register(&acpCall{cb: cb})
	if err != nil {
		return err
	}
	if err := c.send(id, method, params); err != nil {
		c.unregister(id)
		return fmt.Errorf("write rpc request %q: %w", method, err)
	}
	return nil
}

func (c *acpConn) notify(method string, params any) error {
	return c.write(acpMsg{JSONRPC: "2.0", Method: method, Params: mustMarshal(params)})
}

func (c *acpConn) respond(id json.RawMessage, result any) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}

func (c *acpConn) respondError(id json.RawMessage, code int, message string) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *acpRPCError    `json:"error"`
	}{"2.0", id, &acpRPCError{Code: code, Message: message}})
}

func (c *acpConn) logf(msg string, err error) {
	if c.h.Logf != nil {
		c.h.Logf(msg, err)
	}
}

func (c *acpConn) readLoop(r io.Reader) {
	defer func() {
		c.mu.Lock()
		c.closed = true
		calls := c.pending
		c.pending = nil
		c.mu.Unlock()
		for _, call := range calls {
			c.deliver(call, nil, errACPClosed)
		}
		if c.h.Closed != nil {
			c.h.Closed()
		}
		close(c.done)
	}()

	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong && len(chunk) > 0 {
			buf = append(buf, chunk...)
			if len(buf) > c.maxLine {
				tooLong, buf = true, nil
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if tooLong {
			c.logf("acp: dropped oversized line", fmt.Errorf("longer than %d bytes", c.maxLine))
			tooLong = false
		} else if line := bytes.TrimSpace(buf); len(line) > 0 {
			c.handleLine(line)
		}
		buf = buf[:0]
		if err != nil {
			return
		}
	}
}

func (c *acpConn) handleLine(line []byte) {
	var msg acpMsg
	if err := json.Unmarshal(line, &msg); err != nil {
		c.logf("acp: malformed line", err)
		return
	}
	hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
	switch {
	case msg.Method != "" && hasID:
		if c.h.Request != nil {
			go c.h.Request(msg.ID, msg.Method, msg.Params)
		}
	case msg.Method != "":
		if c.h.Notify != nil {
			c.h.Notify(msg.Method, msg.Params)
		}
	case hasID:
		id, err := strconv.ParseInt(string(msg.ID), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		call := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if call == nil {
			return
		}
		if msg.Error != nil {
			c.deliver(call, nil, msg.Error)
		} else {
			c.deliver(call, msg.Result, nil)
		}
	default:
		c.logf("acp: message with no id and no method", nil)
	}
}

func (c *acpConn) deliver(call *acpCall, result json.RawMessage, err error) {
	if call.cb != nil {
		call.cb(result, err)
		return
	}
	call.ch <- acpCallResult{result: result, err: err}
}
