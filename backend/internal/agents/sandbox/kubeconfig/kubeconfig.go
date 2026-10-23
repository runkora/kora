// Package kubeconfig renders the credential-less kubeconfig materialized into
// an agent's sandbox for each Kubernetes connection the agent is granted.
//
// The agent never holds a cluster credential. The kubeconfig names the target
// API server and carries PlaceholderToken as its bearer; the egress gateway
// intercepts the request and overwrites the Authorization header with the
// connection's brokered ServiceAccount token before re-originating it upstream.
// This mirrors the GH_TOKEN placeholder the sandbox already sets for `gh`.
//
// The placeholder is not an optimisation — it is required. kubectl's clientcmd
// refuses to send an unauthenticated request: given a kubeconfig with no auth
// info (even an explicit empty `user: {}`), it prompts on stdin for a username
// and fails, never opening a connection. Only a non-empty credential makes it
// construct a Bearer header, which the gateway then replaces. A bare
// `kubectl --server=…` therefore cannot work in a sandbox, which is why this
// file must exist before the agent runs.
package kubeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PlaceholderToken is the bearer written into every rendered kubeconfig. It is
// deliberately non-functional and self-describing: it never reaches the API
// server (the gateway strips it), and it reads unambiguously as a non-secret to
// a credential scanner. Same value and reasoning as the sandbox's GH_TOKEN.
const PlaceholderToken = "placeholder-credential-injected-by-orbit-egress-gateway"

// ClustersEnvVar carries the JSON-encoded []Cluster from kora-agents into the
// sandbox, where sandbox-exec renders it to disk at startup.
const ClustersEnvVar = "KORA_KUBE_CLUSTERS"

// ErrNoServer is returned when a cluster entry names no API server.
var ErrNoServer = errors.New("kubeconfig cluster has no server")

// ErrNoName is returned when a cluster entry has no name. The name becomes the
// cluster, context and user key, and current-context when it is first — an empty
// or whitespace name would emit a document kubectl cannot resolve a context in.
var ErrNoName = errors.New("kubeconfig cluster has no name")

// Cluster is one Kubernetes connection exposed to the sandbox. Name is the
// registered connection name (it becomes the context name, so an agent granted
// several clusters can `kubectl config use-context <connection>`), and Server
// is the API server's URL.
type Cluster struct {
	Name   string `json:"name"`
	Server string `json:"server"`
}

// ServerURL builds a cluster's server URL from a connection's baseHost, which
// is conventionally "host:443" and carries no scheme.
func ServerURL(baseHost string) string {
	h := strings.TrimSpace(baseHost)
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "http://") {
		return h
	}
	return "https://" + h
}

// Encode marshals clusters for ClustersEnvVar. An empty slice encodes to "" so
// callers can omit the env var entirely rather than set it to "null".
func Encode(clusters []Cluster) (string, error) {
	if len(clusters) == 0 {
		return "", nil
	}
	b, err := json.Marshal(clusters)
	if err != nil {
		return "", fmt.Errorf("encode kube clusters: %w", err)
	}
	return string(b), nil
}

// Decode parses ClustersEnvVar. An empty value yields no clusters and no error —
// an agent with no Kubernetes connection simply gets no kubeconfig.
func Decode(raw string) ([]Cluster, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var clusters []Cluster
	if err := json.Unmarshal([]byte(raw), &clusters); err != nil {
		return nil, fmt.Errorf("decode %s: %w", ClustersEnvVar, err)
	}
	return clusters, nil
}

// kubeconfig mirrors the subset of the kubeconfig schema we emit. It is
// marshaled as JSON, which kubectl parses: YAML is a superset of JSON, and
// emitting JSON avoids a YAML dependency in the sandbox binary.
type kubeconfig struct {
	APIVersion     string       `json:"apiVersion"`
	Kind           string       `json:"kind"`
	Clusters       []namedClust `json:"clusters"`
	Contexts       []namedCtx   `json:"contexts"`
	CurrentContext string       `json:"current-context"`
	Users          []namedUser  `json:"users"`
}

type namedClust struct {
	Name    string      `json:"name"`
	Cluster clusterSpec `json:"cluster"`
}
type clusterSpec struct {
	Server string `json:"server"`
}
type namedCtx struct {
	Name    string  `json:"name"`
	Context ctxSpec `json:"context"`
}
type ctxSpec struct {
	Cluster string `json:"cluster"`
	User    string `json:"user"`
}
type namedUser struct {
	Name string   `json:"name"`
	User userSpec `json:"user"`
}
type userSpec struct {
	Token string `json:"token"`
}

// Render produces the kubeconfig document for clusters. The first cluster
// becomes the current context, so an agent with exactly one Kubernetes
// connection — the common case — can run `kubectl get pods` with no flags.
//
// No certificate-authority is set: the sandbox mounts the tenant CA over the OS
// trust bundle, which kubectl (a Go binary) honors. Pinning a CA file here
// would break the gateway's re-originated TLS, whose leaf chains to that CA and
// not to the target cluster's.
//
// Names and servers are trimmed and validated rather than trusted: this input
// crosses a process boundary (ClustersEnvVar), and a blank or whitespace name
// would emit a document whose current-context resolves to nothing.
func Render(clusters []Cluster) ([]byte, error) {
	if len(clusters) == 0 {
		return nil, nil
	}
	cfg := kubeconfig{APIVersion: "v1", Kind: "Config"}
	for _, c := range clusters {
		name := strings.TrimSpace(c.Name)
		server := strings.TrimSpace(c.Server)
		if name == "" {
			return nil, fmt.Errorf("%w: server %q", ErrNoName, server)
		}
		if server == "" {
			return nil, fmt.Errorf("%w: %q", ErrNoServer, name)
		}
		if cfg.CurrentContext == "" {
			cfg.CurrentContext = name
		}
		cfg.Clusters = append(cfg.Clusters, namedClust{Name: name, Cluster: clusterSpec{Server: server}})
		cfg.Contexts = append(cfg.Contexts, namedCtx{Name: name, Context: ctxSpec{Cluster: name, User: name}})
		cfg.Users = append(cfg.Users, namedUser{Name: name, User: userSpec{Token: PlaceholderToken}})
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render kubeconfig: %w", err)
	}
	return append(b, '\n'), nil
}

// WriteTo renders clusters and writes them to path, creating parent directories.
// It is a no-op for an empty cluster list. Mode 0600 matches kubectl's own
// expectations for a kubeconfig, even though this one holds no secret.
func WriteTo(path string, clusters []Cluster) error {
	data, err := Render(clusters)
	if err != nil || data == nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create kubeconfig dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	return nil
}
