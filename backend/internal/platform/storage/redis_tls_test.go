package storage

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestRedisTLSConfig(t *testing.T) {
	if tc, err := (&RedisConfig{Host: "r"}).tlsConfig(); err != nil || tc != nil {
		t.Fatalf("disabled TLS = %v, %v; want nil", tc, err)
	}
	tc, err := (&RedisConfig{Host: "r", TLSEnabled: true}).tlsConfig()
	if err != nil || tc.MinVersion != tls.VersionTLS12 {
		t.Fatalf("default TLS = %+v, %v", tc, err)
	}
	tc, _ = (&RedisConfig{Host: "r", TLSEnabled: true, ServerName: "redis.svc"}).tlsConfig()
	if tc.ServerName != "redis.svc" {
		t.Errorf("ServerName = %q", tc.ServerName)
	}
	if _, err := (&RedisConfig{TLSEnabled: true, CAFile: "/nonexistent"}).tlsConfig(); err == nil {
		t.Error("missing CA file must fail")
	}
	bad := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(bad, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&RedisConfig{TLSEnabled: true, CAFile: bad}).tlsConfig(); err == nil {
		t.Error("invalid CA file must fail")
	}
}

func TestNewRedisTLSAgainstPlaintextFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, "+PONG\r\n")
			c.Close()
		}
	}()
	cfg := &RedisConfig{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLSEnabled: true,
		DialTimeoutSeconds: 1, ReadTimeoutSeconds: 1, WriteTimeoutSeconds: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = NewRedis(ctx, cfg, logging.NewLogger())
	if err == nil {
		t.Fatal("TLS to a plaintext Redis must fail at startup")
	}
	var rh tls.RecordHeaderError
	if !errors.As(err, &rh) {
		t.Errorf("error %q is not a TLS handshake failure", err)
	}
}
