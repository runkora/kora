package storage

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// fakeRedis answers the few RESP commands the blocklist uses.
type fakeRedis struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (f *fakeRedis) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeRedis) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	rd := bufio.NewReader(conn)
	for {
		args, err := readCommand(rd)
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte(f.reply(args)))
	}
}

func (f *fakeRedis) reply(args []string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return "-ERR unknown command 'HELLO'\r\n"
	case "PING":
		return "+PONG\r\n"
	case "SET":
		f.keys[args[1]] = true
		return "+OK\r\n"
	case "EXISTS":
		if f.keys[args[1]] {
			return ":1\r\n"
		}
		return ":0\r\n"
	default:
		return "+OK\r\n"
	}
}

// readCommand reads one RESP array of bulk strings.
func readCommand(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("bad command header %q", line)
	}
	args := make([]string, 0, n)
	for range n {
		if _, err := rd.ReadString('\n'); err != nil {
			return nil, err
		}
		arg, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		args = append(args, strings.TrimRight(arg, "\r\n"))
	}
	return args, nil
}

// The blocklist starts with Redis down, fails open with ErrRedisUnavailable,
// then enforces revocation once Redis comes up.
func TestJTIBlocklist_RedisDownAtStart(t *testing.T) {
	old := redisProbeInterval
	redisProbeInterval = 20 * time.Millisecond
	t.Cleanup(func() { redisProbeInterval = old })

	// Reserve a port, then free it so the first probe finds nothing listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx := t.Context()
	cfg := &RedisConfig{Host: "127.0.0.1", Port: port, DialTimeoutSeconds: 1, ReadTimeoutSeconds: 1, WriteTimeoutSeconds: 1, PoolSize: 2}
	rc, err := NewRedis(ctx, cfg, logging.NewLogger())
	if err != nil {
		t.Fatalf("NewRedis must not fail on an unreachable Redis: %v", err)
	}
	defer func() { _ = rc.Close() }()
	bl := NewJTIBlocklist(rc)

	if rc.Available() {
		t.Fatal("Available() = true with nothing listening")
	}
	if blocked, err := bl.IsBlocked(ctx, "jti-1"); blocked || !errors.Is(err, ErrRedisUnavailable) {
		t.Fatalf("IsBlocked while down = (%v, %v), want (false, ErrRedisUnavailable)", blocked, err)
	}

	up, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on reserved port: %v", err)
	}
	defer func() { _ = up.Close() }()
	go (&fakeRedis{keys: map[string]bool{}}).serve(up)

	deadline := time.Now().Add(5 * time.Second)
	for !rc.Available() {
		if time.Now().After(deadline) {
			t.Fatal("client never noticed Redis coming up")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := bl.BlockJTI(ctx, "jti-1", time.Minute); err != nil {
		t.Fatalf("BlockJTI: %v", err)
	}
	cases := []struct {
		jti  string
		want bool
	}{
		{"jti-1", true},
		{"jti-2", false},
	}
	for _, tc := range cases {
		got, err := bl.IsBlocked(ctx, tc.jti)
		if err != nil || got != tc.want {
			t.Errorf("IsBlocked(%s) = (%v, %v), want (%v, nil)", tc.jti, got, err, tc.want)
		}
	}
}
