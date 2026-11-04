package token

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// A static verifier accepts tokens signed by its key, issuer and audience only.
func TestStaticVerifier(t *testing.T) {
	key := mustKey(t)
	other := mustKey(t)
	v := NewStaticVerifier(&key.PublicKey, DefaultIssuer, DefaultAudience)

	cases := []struct {
		name    string
		key     *rsa.PrivateKey
		iss     string
		aud     string
		wantErr bool
	}{
		{"own key", key, DefaultIssuer, DefaultAudience, false},
		{"other key", other, DefaultIssuer, DefaultAudience, true},
		{"wrong issuer", key, "someone-else", DefaultAudience, true},
		{"wrong audience", key, DefaultIssuer, "other-aud", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"sub": "person-1", "iss": tc.iss, "aud": tc.aud, "exp": time.Now().Add(time.Hour).Unix(),
			}).SignedString(tc.key)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			var got struct {
				Sub string `json:"sub"`
			}
			err = v.VerifyAndExtract(context.Background(), raw, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got.Sub != "person-1" {
				t.Errorf("sub = %q, want person-1", got.Sub)
			}
		})
	}
}

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}
