package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

// imageConfig is what validation reads from a registry: the digest sandboxes
// pin to, and the user and labels of the image config.
type imageConfig struct {
	Digest string
	User   string
	Labels map[string]string
}

// registryReader reads an image's config from its registry without pulling layers.
type registryReader interface {
	Read(ctx context.Context, ref string, auth []dockerConfig) (imageConfig, error)
}

// remoteRegistry reads registries over HTTPS. Mirrors maps a registry host in
// an image reference to the host the operator reads it from. A mirror given as
// http://host is read over plain HTTP.
type remoteRegistry struct {
	Mirrors map[string]string
}

// NewRemoteRegistry reads image configs from their registries.
func NewRemoteRegistry(mirrors map[string]string) registryReader {
	return remoteRegistry{Mirrors: mirrors}
}

// Read resolves ref to a digest and reads the config of the image for the
// operator's own architecture. An index digest is returned as is. Each node
// then pulls its own platform from it.
func (r remoteRegistry) Read(ctx context.Context, ref string, auth []dockerConfig) (imageConfig, error) {
	var opts []name.Option
	for from, to := range r.Mirrors {
		if rest, ok := strings.CutPrefix(ref, from+"/"); ok {
			// An http:// mirror is read over plain HTTP.
			if host, plain := strings.CutPrefix(to, "http://"); plain {
				to = host
				opts = append(opts, name.Insecure)
			}
			ref = strings.TrimPrefix(to, "https://") + "/" + rest
			break
		}
	}
	parsed, err := name.ParseReference(ref, opts...)
	if err != nil {
		return imageConfig{}, fmt.Errorf("parse image reference: %w", err)
	}
	desc, err := remote.Get(parsed,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(dockerKeychain(auth)),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}),
	)
	if err != nil {
		return imageConfig{}, fmt.Errorf("read manifest: %w", err)
	}
	img, err := desc.Image()
	if err != nil {
		return imageConfig{}, fmt.Errorf("resolve image: %w", err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return imageConfig{}, fmt.Errorf("read image config: %w", err)
	}
	return imageConfig{Digest: desc.Digest.String(), User: cfg.Config.User, Labels: cfg.Config.Labels}, nil
}

// dockerConfig is the parsed .dockerconfigjson of one pull secret.
type dockerConfig struct {
	Auths map[string]authn.AuthConfig `json:"auths"`
}

// parseDockerConfig reads a kubernetes.io/dockerconfigjson payload.
func parseDockerConfig(raw []byte) (dockerConfig, error) {
	var c dockerConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return dockerConfig{}, fmt.Errorf("parse docker config: %w", err)
	}
	return c, nil
}

// dockerKeychain resolves a registry against pull secrets. The first match wins.
type dockerKeychain []dockerConfig

// Resolve implements authn.Keychain. Keys may carry the scheme or path that
// `docker login` writes.
func (k dockerKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	want := res.RegistryStr()
	for _, c := range k {
		for host, cfg := range c.Auths {
			if registryHost(host) == want {
				return authn.FromConfig(cfg), nil
			}
		}
	}
	return authn.Anonymous, nil
}

func registryHost(key string) string {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	host, _, _ := strings.Cut(key, "/")
	if host == "docker.io" || host == "index.docker.io" {
		return name.DefaultRegistry
	}
	return host
}

// Validation failure reasons on the Valid condition.
const (
	reasonValidated        = "Validated"
	reasonRegistryError    = "RegistryError"
	reasonPullSecretError  = "PullSecretError"
	reasonWrongUser        = si.ReasonWrongUser
	reasonMissingBaseLabel = si.ReasonMissingBaseLabel
)

// validateImageConfig checks an image is built on the base image. It returns
// the failure reason and message, or empty strings for a valid image.
func validateImageConfig(cfg imageConfig) (reason, message string) {
	uid, _, _ := strings.Cut(cfg.User, ":")
	if uid != si.SandboxUID {
		return reasonWrongUser, fmt.Sprintf("image runs as user %q; sandbox images run as uid %s", cfg.User, si.SandboxUID)
	}
	if cfg.Labels[si.BaseLabel] == "" {
		return reasonMissingBaseLabel, fmt.Sprintf("image has no %s label; build it FROM kora-sandbox-base", si.BaseLabel)
	}
	return "", ""
}
