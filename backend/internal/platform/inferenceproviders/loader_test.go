package inferenceproviders

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	cfg, sec := t.TempDir(), t.TempDir()

	got, err := Load(cfg, sec)
	if err != nil || got != nil {
		t.Fatalf("absent file: got %v, %v; want nothing", got, err)
	}

	write(t, cfg, ConfigMapKey, `[{"name":"openai:router","resource":"router","type":"openai","openai":{"base_url":"http://x","api_key_key":"router.apiKey"}}]`)
	write(t, sec, "router.apiKey", "sk-test\n")
	got, err = Load(cfg, sec)
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %v, %v", got, err)
	}
	key, err := got[0].Secret(got[0].OpenAI.APIKeyKey)
	if err != nil || key != "sk-test" {
		t.Errorf("Secret = %q, %v", key, err)
	}
	if v, err := got[0].Secret(""); v != "" || err != nil {
		t.Errorf("empty key = %q, %v", v, err)
	}
	if _, err := got[0].Secret("../etc/passwd"); err == nil {
		t.Error("path traversal accepted")
	}
	if _, err := got[0].Secret("missing.apiKey"); err == nil {
		t.Error("missing secret file accepted")
	}

	write(t, cfg, ConfigMapKey, `not json`)
	if _, err := Load(cfg, sec); err == nil {
		t.Error("malformed providers.json accepted")
	}
}

func TestEntryValidate(t *testing.T) {
	cases := []struct {
		name  string
		entry Entry
		ok    bool
	}{
		{"matching block", Entry{Type: TypeOpenAI, OpenAI: &OpenAI{}}, true},
		{"missing block", Entry{Type: TypeBedrock}, false},
		{"extra block", Entry{Type: TypeOpenAI, OpenAI: &OpenAI{}, Anthropic: &Anthropic{}}, false},
		{"unknown type", Entry{Type: "mistral"}, false},
	}
	for _, tc := range cases {
		if err := tc.entry.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: Validate() = %v", tc.name, err)
		}
	}
}

func TestWatchCallsOnChange(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	go Watch(ctx, logging.NewLoggerWithOutput(io.Discard), []string{dir, ""}, func(context.Context) { calls.Add(1) })
	time.Sleep(100 * time.Millisecond)

	write(t, dir, ConfigMapKey, "[]")
	write(t, dir, "other", "x")
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Errorf("onChange calls = %d, want 1 after a debounced burst", calls.Load())
	}
}
