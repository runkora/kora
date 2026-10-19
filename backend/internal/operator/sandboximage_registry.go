package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
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

// remoteRegistry reads registries over HTTPS. Mirrors maps a registry host in an image
// reference to the host the operator reads instead; an http://host mirror is read over plain HTTP.
type remoteRegistry struct {
	Mirrors map[string]string
}

// NewRemoteRegistry reads image configs from their registries.
func NewRemoteRegistry(mirrors map[string]string) registryReader {
	return remoteRegistry{Mirrors: mirrors}
}

// Read resolves ref to a digest and reads the image config for the operator's architecture.
// An index digest is returned as is, and each node pulls its own platform from it.
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
	desc, err := getManifest(ctx, parsed, dockerKeychain(auth).authenticators(parsed.Context().RegistryStr()))
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

// getManifest reads the manifest with each credential in turn. It moves to the next on
// 401 or 403, stops at the first success and returns the last error when all fail.
func getManifest(ctx context.Context, ref name.Reference, auths []authn.Authenticator) (*remote.Descriptor, error) {
	var err error
	for _, a := range auths {
		var desc *remote.Descriptor
		desc, err = remote.Get(ref,
			remote.WithContext(ctx),
			remote.WithAuth(a),
			remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}),
		)
		if err == nil {
			return desc, nil
		}
		var terr *transport.Error
		if !errors.As(err, &terr) || (terr.StatusCode != http.StatusUnauthorized && terr.StatusCode != http.StatusForbidden) {
			return nil, err
		}
	}
	return nil, err
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

// dockerKeychain resolves a registry against pull secrets. Resolve returns the first
// match. authenticators returns every match, in order.
type dockerKeychain []dockerConfig

// Resolve implements authn.Keychain. Keys may carry the scheme or path that
// `docker login` writes.
func (k dockerKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	return k.authenticators(res.RegistryStr())[0], nil
}

// authenticators returns one authenticator per matching credential, or anonymous
// when none matches.
func (k dockerKeychain) authenticators(registry string) []authn.Authenticator {
	var out []authn.Authenticator
	for _, c := range k {
		for host, cfg := range c.Auths {
			if registryHost(host) == registry {
				out = append(out, authn.FromConfig(cfg))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, authn.Anonymous)
	}
	return out
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
	if uid != strconv.Itoa(si.SandboxUID) {
		return reasonWrongUser, fmt.Sprintf("image runs as user %q; sandbox images run as uid %d", cfg.User, si.SandboxUID)
	}
	if cfg.Labels[si.BaseLabel] == "" {
		return reasonMissingBaseLabel, fmt.Sprintf("image has no %s label; build it FROM kora-sandbox-base", si.BaseLabel)
	}
	return "", ""
}
