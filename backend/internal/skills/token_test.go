package skills

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
)

var (
	keyOnce sync.Once
	keyPEM  []byte
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	keyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	})
	return keyPEM
}

type fakeMinter struct {
	mu       sync.Mutex
	err      error
	requests []ghclient.InstallationTokenRequest
	expires  time.Time
}

func (f *fakeMinter) CreateInstallationToken(_ context.Context, _ string, _ int64, body ghclient.InstallationTokenRequest) (ghclient.InstallationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, body)
	if f.err != nil {
		return ghclient.InstallationToken{}, f.err
	}
	exp := f.expires
	if exp.IsZero() {
		exp = time.Now().Add(time.Hour)
	}
	return ghclient.InstallationToken{Token: "tok", ExpiresAt: exp}, nil
}

// Each token reads only the Marketplace repository, with contents:read.
func TestTokenProvider_ScopesToTheRepository(t *testing.T) {
	minter := &fakeMinter{}
	p, err := newTokenProvider(minter, "1", 2, "skills", testKeyPEM(t))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := p.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := []ghclient.InstallationTokenRequest{{Repositories: []string{"skills"}, Permissions: map[string]string{"contents": "read"}}}
	if !reflect.DeepEqual(minter.requests, want) {
		t.Errorf("requests = %+v, want one repository-scoped mint", minter.requests)
	}
}

func TestTokenProvider_RefreshesBeforeExpiry(t *testing.T) {
	minter := &fakeMinter{expires: time.Now().Add(4 * time.Minute)}
	p, err := newTokenProvider(minter, "1", 2, "skills", testKeyPEM(t))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Token(context.Background())
	_, _ = p.Token(context.Background())
	if len(minter.requests) != 2 {
		t.Errorf("mints = %d, want 2 for a token inside the refresh window", len(minter.requests))
	}
}

// A refusal is a credential failure; an outage is not.
func TestTokenProvider_ClassifiesMintFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		credential bool
	}{
		{"refused", ghclient.ErrInstallationPermissions, true},
		{"unexpected status", ghclient.ErrUnexpectedStatus, true},
		{"outage", ghclient.ErrUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := newTokenProvider(&fakeMinter{err: tc.err}, "1", 2, "skills", testKeyPEM(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Token(context.Background())
			if got := errors.Is(err, ErrCredentialUnavailable); got != tc.credential {
				t.Errorf("credential = %v, want %v (%v)", got, tc.credential, err)
			}
		})
	}
}

func TestNewTokenProvider_RejectsAnInvalidKey(t *testing.T) {
	if _, err := newTokenProvider(&fakeMinter{}, "1", 2, "skills", []byte("not a key")); err == nil {
		t.Fatal("want an error for an invalid key")
	}
}
