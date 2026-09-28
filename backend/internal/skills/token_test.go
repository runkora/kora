package skills

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
)

// testKeyPath generates a fresh RSA key, PEM-encodes it, writes it to a file
// under t.TempDir(), and returns that file's path.
func testKeyPath(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	path := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

// fakeMinter is a test double for installationTokenMinter. It records every
// call (for assertions on mint count, the JWT it was handed, and the request
// body's permission scope) and returns a canned token/error.
type fakeMinter struct {
	mu     sync.Mutex
	calls  int
	jwts   []string
	bodies []ghclient.InstallationTokenRequest

	tok ghclient.InstallationToken // set by tests
	err error
}

func (f *fakeMinter) CreateInstallationToken(_ context.Context, appJWT string, _ int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error) {
	f.mu.Lock()
	f.calls++
	f.jwts = append(f.jwts, appJWT)
	f.bodies = append(f.bodies, body)
	f.mu.Unlock()
	return f.tok, f.err
}

func (f *fakeMinter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newProvider(t *testing.T, minter installationTokenMinter) *TokenProvider {
	t.Helper()
	return NewTokenProvider(minter, "app-1", 42, testKeyPath(t))
}

func TestToken_MintsOnFirstUse(t *testing.T) {
	keyPath := testKeyPath(t)
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read test key: %v", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	minter := &fakeMinter{tok: ghclient.InstallationToken{
		Token:     "tok-123",
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	p := NewTokenProvider(minter, "app-1", 42, keyPath)

	got, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: unexpected error: %v", err)
	}
	if got != "tok-123" {
		t.Errorf("Token: got %q, want %q", got, "tok-123")
	}
	if minter.callCount() != 1 {
		t.Errorf("CreateInstallationToken calls: got %d, want 1", minter.callCount())
	}

	// The JWT handed to the minter must be signed for this App (App JWT,
	// not the installation token) and parseable as RS256.
	claims := &jwt.RegisteredClaims{}
	_, err = jwt.ParseWithClaims(minter.jwts[0], claims, func(*jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse app jwt: %v", err)
	}
	if claims.Issuer != "app-1" {
		t.Errorf("app jwt issuer: got %q, want %q", claims.Issuer, "app-1")
	}

	// The mint request must scope down to read-only contents access rather
	// than requesting the App installation's full configured access.
	if !maps.Equal(minter.bodies[0].Permissions, installationTokenPermissions) {
		t.Errorf("CreateInstallationToken permissions: got %v, want %v", minter.bodies[0].Permissions, installationTokenPermissions)
	}
}

func TestToken_MissingPrivateKeyFile(t *testing.T) {
	minter := &fakeMinter{}
	p := NewTokenProvider(minter, "app-1", 42, filepath.Join(t.TempDir(), "does-not-exist.pem"))

	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("Token: expected error for missing private key file, got nil")
	}
	if minter.callCount() != 0 {
		t.Errorf("CreateInstallationToken calls: got %d, want 0 (should fail before minting)", minter.callCount())
	}
}

func TestToken_InvalidPrivateKeyFileContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write invalid key file: %v", err)
	}
	minter := &fakeMinter{}
	p := NewTokenProvider(minter, "app-1", 42, path)

	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("Token: expected error for invalid private key contents, got nil")
	}
	if minter.callCount() != 0 {
		t.Errorf("CreateInstallationToken calls: got %d, want 0 (should fail before minting)", minter.callCount())
	}
}

func TestToken_PropagatesMintError(t *testing.T) {
	wantErr := errors.New("github unavailable")
	minter := &fakeMinter{err: wantErr}
	p := newProvider(t, minter)

	_, err := p.Token(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Token: got error %v, want wrapping %v", err, wantErr)
	}
}

func TestToken_ReusesCachedTokenBeforeRefreshWindow(t *testing.T) {
	minter := &fakeMinter{tok: ghclient.InstallationToken{
		Token:     "tok-first",
		ExpiresAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), // 1h TTL
	}}
	p := newProvider(t, minter)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }

	first, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token (first): %v", err)
	}

	// 10 minutes later: still well outside the 5-minute refresh window
	// (expiresAt - refreshBefore = 00:55, now = 00:10).
	now = now.Add(10 * time.Minute)
	second, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token (second): %v", err)
	}

	if second != first {
		t.Errorf("Token: got %q on second call, want cached %q", second, first)
	}
	if minter.callCount() != 1 {
		t.Errorf("CreateInstallationToken calls: got %d, want 1 (cache should be reused)", minter.callCount())
	}
}

func TestToken_RefreshesInsideRefreshWindow(t *testing.T) {
	firstExpiry := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC) // 1h TTL
	minter := &fakeMinter{tok: ghclient.InstallationToken{Token: "tok-first", ExpiresAt: firstExpiry}}
	p := newProvider(t, minter)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }

	if _, err := p.Token(context.Background()); err != nil {
		t.Fatalf("Token (first): %v", err)
	}

	// Advance to inside the 5-minute refresh window (00:56 > 00:55 = expiresAt - refreshBefore).
	now = firstExpiry.Add(-4 * time.Minute)
	minter.tok = ghclient.InstallationToken{Token: "tok-second", ExpiresAt: now.Add(time.Hour)}

	got, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token (second): %v", err)
	}
	if got != "tok-second" {
		t.Errorf("Token: got %q, want refreshed %q", got, "tok-second")
	}
	if minter.callCount() != 2 {
		t.Errorf("CreateInstallationToken calls: got %d, want 2 (should have refreshed)", minter.callCount())
	}
}

func TestToken_RefreshesAtExactBoundary(t *testing.T) {
	firstExpiry := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC) // 1h TTL
	minter := &fakeMinter{tok: ghclient.InstallationToken{Token: "tok-first", ExpiresAt: firstExpiry}}
	p := newProvider(t, minter)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }

	if _, err := p.Token(context.Background()); err != nil {
		t.Fatalf("Token (first): %v", err)
	}

	// now == expiresAt - refreshBefore exactly (00:55). Token uses a strict
	// Before comparison, so equality must count as stale, not fresh — this
	// pins that boundary rather than leaving it to accident.
	now = firstExpiry.Add(-refreshBefore)
	minter.tok = ghclient.InstallationToken{Token: "tok-second", ExpiresAt: now.Add(time.Hour)}

	got, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token (at boundary): %v", err)
	}
	if got != "tok-second" {
		t.Errorf("Token: got %q at exact refresh boundary, want refreshed %q", got, "tok-second")
	}
	if minter.callCount() != 2 {
		t.Errorf("CreateInstallationToken calls: got %d, want 2 (boundary must refresh, not reuse)", minter.callCount())
	}
}

func TestToken_ConcurrentCallsMintOnce(t *testing.T) {
	minter := &fakeMinter{tok: ghclient.InstallationToken{
		Token:     "tok-concurrent",
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	p := newProvider(t, minter)

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = p.Token(context.Background())
		}(i)
	}
	wg.Wait()

	for i := range goroutines {
		if errs[i] != nil {
			t.Fatalf("Token[%d]: unexpected error: %v", i, errs[i])
		}
		if results[i] != "tok-concurrent" {
			t.Errorf("Token[%d]: got %q, want %q", i, results[i], "tok-concurrent")
		}
	}
	if minter.callCount() != 1 {
		t.Errorf("CreateInstallationToken calls: got %d, want 1 (concurrent callers must not double-mint)", minter.callCount())
	}
}

func TestToken_ConcurrentCallsDuringFailureMintOnce(t *testing.T) {
	wantErr := errors.New("github unavailable")
	minter := &fakeMinter{err: wantErr}
	p := newProvider(t, minter)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.Token(context.Background())
		}(i)
	}
	wg.Wait()

	for i := range goroutines {
		if !errors.Is(errs[i], wantErr) {
			t.Errorf("Token[%d]: got error %v, want wrapping %v", i, errs[i], wantErr)
		}
	}
	if minter.callCount() != 1 {
		t.Errorf("CreateInstallationToken calls: got %d, want 1 (concurrent callers must share one failed mint attempt, not each retry)", minter.callCount())
	}
}

// blockingMinter blocks CreateInstallationToken until release is closed (or
// ctx is canceled), so tests can control exactly when an in-flight mint
// completes.
type blockingMinter struct {
	release chan struct{}
	tok     ghclient.InstallationToken
}

func (b *blockingMinter) CreateInstallationToken(ctx context.Context, _ string, _ int64, _ ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error) {
	select {
	case <-b.release:
		return b.tok, nil
	case <-ctx.Done():
		return ghclient.InstallationToken{}, ctx.Err()
	}
}

func TestToken_WaiterHonorsOwnContext(t *testing.T) {
	release := make(chan struct{})
	minter := &blockingMinter{release: release, tok: ghclient.InstallationToken{
		Token:     "tok-x",
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	p := newProvider(t, minter)

	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		if _, err := p.Token(context.Background()); err != nil {
			t.Errorf("leader Token: unexpected error: %v", err)
		}
	}()

	// Wait for the leader to register as in-flight before the waiter joins.
	for {
		p.mu.Lock()
		inflight := p.inflight != nil
		p.mu.Unlock()
		if inflight {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Token(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter Token: got %v, want context.Canceled", err)
	}

	close(release)
	<-leaderDone
}
