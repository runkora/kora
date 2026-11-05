package skills

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/storage"
)

// respServer answers the RESP commands the cache uses and records each SET's
// TTL, like the fake in internal/platform/storage.
type respServer struct {
	mu   sync.Mutex
	vals map[string]string
	ttls map[string]time.Duration
}

func (f *respServer) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = conn.Close() }()
			rd := bufio.NewReader(conn)
			for {
				args, err := readRESP(rd)
				if err != nil {
					return
				}
				_, _ = conn.Write([]byte(f.reply(args)))
			}
		}()
	}
}

func (f *respServer) reply(args []string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return "-ERR unknown command 'HELLO'\r\n"
	case "PING":
		return "+PONG\r\n"
	case "GET":
		v, ok := f.vals[args[1]]
		if !ok {
			return "$-1\r\n"
		}
		return fmt.Sprintf("$%d\r\n%s\r\n", len(v), v)
	case "SET":
		f.vals[args[1]] = args[2]
		f.ttls[args[1]] = 0
		for i := 3; i+1 < len(args); i += 2 {
			n, _ := strconv.Atoi(args[i+1])
			switch strings.ToUpper(args[i]) {
			case "EX":
				f.ttls[args[1]] = time.Duration(n) * time.Second
			case "PX":
				f.ttls[args[1]] = time.Duration(n) * time.Millisecond
			}
		}
		return "+OK\r\n"
	default:
		return "+OK\r\n"
	}
}

func readRESP(rd *bufio.Reader) ([]string, error) {
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
		head, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(head, "$")))
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

// newTestCache returns a Cache on a fake Redis, and the fake.
func newTestCache(t *testing.T, ttl, failureTTL time.Duration) (*Cache, *respServer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	fake := &respServer{vals: map[string]string{}, ttls: map[string]time.Duration{}}
	go fake.serve(ln)
	return newCacheAt(t, ln.Addr().(*net.TCPAddr).Port, ttl, failureTTL), fake
}

func newCacheAt(t *testing.T, port int, ttl, failureTTL time.Duration) *Cache {
	t.Helper()
	cfg := &storage.RedisConfig{Host: "127.0.0.1", Port: port, MaxRetries: -1, DialTimeoutSeconds: 1, ReadTimeoutSeconds: 1, WriteTimeoutSeconds: 1, PoolSize: 2}
	rc, err := storage.NewRedis(context.Background(), cfg, discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return NewCache(rc, ttl, failureTTL)
}

func TestCache_KeysAndTTLs(t *testing.T) {
	ctx := context.Background()
	cache, fake := newTestCache(t, time.Hour, time.Minute)
	m := testMarketplace(t, "tenant-a", "team-skills")

	list := skillList{Source: m.source(), SHA: testSHA, Skills: []skillRecord{{Path: "skills/pdf", Name: "pdf"}}}
	if err := cache.SetList(ctx, m, list); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetFailure(ctx, m, crawlFailure{Source: m.source(), Transient: true}); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetFile(ctx, m, testSHA, "skills/pdf/forms.md", "forms"); err != nil {
		t.Fatal(err)
	}

	want := map[string]time.Duration{
		"skills:tenant-a:team-skills:list":                                     listRetention * time.Hour,
		"skills:tenant-a:team-skills:failure":                                  time.Minute,
		"skills:tenant-a:team-skills:file:" + testSHA + ":skills/pdf/forms.md": time.Hour,
	}
	fake.mu.Lock()
	for key, ttl := range want {
		if got, ok := fake.ttls[key]; !ok || got != ttl {
			t.Errorf("%s TTL = %v (set %v), want %v", key, got, ok, ttl)
		}
	}
	if len(fake.ttls) != len(want) {
		t.Errorf("keys = %v, want only the tenant-scoped keys", fake.ttls)
	}
	fake.mu.Unlock()

	got, ok, err := cache.GetList(ctx, m)
	if err != nil || !ok || got.SHA != testSHA || len(got.Skills) != 1 {
		t.Errorf("GetList = %+v, %v, %v", got, ok, err)
	}
	f, ok, err := cache.GetFailure(ctx, m)
	if err != nil || !ok || !f.Transient {
		t.Errorf("GetFailure = %+v, %v, %v", f, ok, err)
	}
	content, ok, err := cache.GetFile(ctx, m, testSHA, "skills/pdf/forms.md")
	if err != nil || !ok || content != "forms" {
		t.Errorf("GetFile = %q, %v, %v", content, ok, err)
	}

	other := testMarketplace(t, "tenant-b", "team-skills")
	if _, ok, _ := cache.GetList(ctx, other); ok {
		t.Error("another tenant read tenant-a's list")
	}
}

func TestCache_CorruptEntryIsAMiss(t *testing.T) {
	cache, fake := newTestCache(t, time.Hour, time.Minute)
	m := testMarketplace(t, "tenant-a", "team-skills")
	fake.mu.Lock()
	fake.vals["skills:tenant-a:team-skills:list"] = "{not json"
	fake.mu.Unlock()
	if _, ok, err := cache.GetList(context.Background(), m); ok || err != nil {
		t.Errorf("corrupt entry = %v, %v, want a miss", ok, err)
	}
}

func TestCache_RedisErrorIsWrapped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cache := newCacheAt(t, port, time.Hour, time.Minute)
	m := testMarketplace(t, "tenant-a", "team-skills")

	if _, _, err := cache.GetList(context.Background(), m); !errors.Is(err, storage.ErrRedisUnavailable) {
		t.Errorf("GetList with Redis down = %v, want ErrRedisUnavailable", err)
	}
	if err := cache.SetList(context.Background(), m, skillList{}); !errors.Is(err, storage.ErrRedisUnavailable) {
		t.Errorf("SetList with Redis down = %v, want ErrRedisUnavailable", err)
	}
}
