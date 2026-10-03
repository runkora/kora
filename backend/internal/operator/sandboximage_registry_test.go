package operator

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

func TestValidateImageConfig(t *testing.T) {
	base := map[string]string{si.BaseLabel: "v1.2.0"}
	for _, tc := range []struct {
		name       string
		cfg        imageConfig
		wantReason string
	}{
		{"uid only", imageConfig{User: "100", Labels: base}, ""},
		{"uid and gid", imageConfig{User: "100:100", Labels: base}, ""},
		{"root", imageConfig{User: "", Labels: base}, reasonWrongUser},
		{"named user", imageConfig{User: "orbit", Labels: base}, reasonWrongUser},
		{"other uid", imageConfig{User: "1000", Labels: base}, reasonWrongUser},
		{"no label", imageConfig{User: "100"}, reasonMissingBaseLabel},
		{"empty label", imageConfig{User: "100", Labels: map[string]string{si.BaseLabel: ""}}, reasonMissingBaseLabel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason, msg := validateImageConfig(tc.cfg)
			if reason != tc.wantReason {
				t.Errorf("reason = %q (%s), want %q", reason, msg, tc.wantReason)
			}
			if reason != "" && msg == "" {
				t.Error("a failure must carry a message")
			}
		})
	}
}

func TestDockerKeychain(t *testing.T) {
	cfg, err := parseDockerConfig([]byte(`{"auths":{
		"https://ghcr.io/v1/":{"username":"u","password":"p"},
		"https://index.docker.io/v1/":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("d:q")) + `"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	kc := dockerKeychain{cfg}
	for _, tc := range []struct{ ref, want string }{
		{"ghcr.io/acme/go:1", "u:p"},
		{"ubuntu:24.04", "d:q"},
		{"quay.io/acme/go:1", ""},
	} {
		ref, err := name.ParseReference(tc.ref)
		if err != nil {
			t.Fatal(err)
		}
		a, err := kc.Resolve(ref.Context())
		if err != nil {
			t.Fatal(err)
		}
		got, err := authn.Authorization(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		user, pass := got.Username, got.Password
		if got.Auth != "" {
			raw, _ := base64.StdEncoding.DecodeString(got.Auth)
			user, pass, _ = strings.Cut(string(raw), ":")
		}
		if cred := strings.Trim(user+":"+pass, ":"); cred != tc.want {
			t.Errorf("%s resolved %q, want %q", tc.ref, cred, tc.want)
		}
	}
}

// Read resolves a tag to its digest and reads the user and labels from the config.
func TestRemoteRegistryRead(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	img, err = mutate.Config(img, v1.Config{User: "100:100", Labels: map[string]string{si.BaseLabel: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.ParseReference(host+"/acme/go:1", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}
	want, _ := img.Digest()

	got, err := remoteRegistry{Insecure: []string{host}}.Read(context.Background(), host+"/acme/go:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != want.String() || got.User != "100:100" || got.Labels[si.BaseLabel] != "v1" {
		t.Errorf("Read = %+v, want digest %s, user 100:100 and the base label", got, want)
	}
}
