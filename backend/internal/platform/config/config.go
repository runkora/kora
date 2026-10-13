// Package config provides a typed, environment-aware configuration loader
// for the kora control plane.
//
// # Loading modes
//
// Use [Load] for packages that own a single config file (e.g. storage):
//
//	var cfg = config.MustLoad[StorageConfig]("internal/platform/storage", config.Environment())
//
// Use [LoadService] for service entry points under cmd/. It merges the
// service's own YAML layers in order:
//
//  1. {serviceModule}/config/base.yaml   — service defaults (optional)
//
//  2. {serviceModule}/config/{env}.yaml  — service env overrides (optional)
//
//     var cfg = config.MustLoadService[Config]("cmd/identity", config.Environment())
//
// # Rendered config files
//
// Both loaders merge /etc/orbit/config/{module}.yaml last when it exists. The
// chart renders it per install and mounts it there; see
// docs/adr/2026-10-06-rendered-service-config-files.md. Decoding is strict:
// an unknown key or a mistyped value fails the load.
//
// Platform-wide settings (observability tunables, log format) are loaded
// independently by each platform package — see [observability.DefaultConfig].
//
// # Security
//
// YAML config files must not contain secrets. Use environment variables for
// all sensitive values; mark secret fields with yaml:"-".
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"go.yaml.in/yaml/v3"
)

// defaultEnvironment is used when APP_ENV is not set.
const defaultEnvironment = "production"

// overlayRoot is where the chart mounts the rendered config files. Tests point it elsewhere.
var overlayRoot = "/etc/orbit/config"

// Environment reads the APP_ENV environment variable.
// Returns "production" if APP_ENV is unset or empty.
// Binaries run on the host set APP_ENV=local. Chart deployments do not set it.
//
// Supported values by convention: "local", "production".
func Environment() string {
	if env := os.Getenv("APP_ENV"); env != "" {
		return env
	}
	return defaultEnvironment
}

// MustLoad is like [Load] but panics on error. Intended for package-level
// variable initialisation where a missing config file is a fatal startup error.
func MustLoad[T any](module, environment string) *T {
	cfg, err := Load[T](module, environment)
	if err != nil {
		panic(err)
	}
	return cfg
}

// Load reads {projectRoot}/{module}/config/{environment}.yaml, then the
// rendered file for module when it exists, and decodes them into a new T.
//
// Use this for packages that own a single config file. For service binaries
// use [LoadService].
func Load[T any](module, environment string) (*T, error) {
	path, err := resolvePath(module, environment)
	if err != nil {
		return nil, fmt.Errorf("config: resolve path for module %q env %q: %w", module, environment, err)
	}
	merged, err := readYAML(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	return loadWithOverlay[T](module, merged)
}

// MustLoadService is like [LoadService] but panics on error. It is the
// standard loader for service binaries under cmd/:
//
//	var Current = config.MustLoadService[Config]("cmd/identity", config.Environment())
func MustLoadService[T any](serviceModule, environment string) *T {
	cfg, err := LoadService[T](serviceModule, environment)
	if err != nil {
		panic(err)
	}
	return cfg
}

// LoadService loads a service binary's configuration by merging, in order,
// {serviceModule}/config/base.yaml, {serviceModule}/config/{env}.yaml and the
// rendered file for serviceModule. Each layer is optional.
func LoadService[T any](serviceModule, environment string) (*T, error) {
	merged := map[string]any{}
	for _, name := range []string{"base", environment} {
		path, err := resolvePath(serviceModule, name)
		if err != nil {
			return nil, fmt.Errorf("config: resolve %s path for %q: %w", name, serviceModule, err)
		}
		if err := mergeIfExists(merged, path); err != nil {
			return nil, err
		}
	}
	return loadWithOverlay[T](serviceModule, merged)
}

// loadWithOverlay merges the rendered file for module over merged and decodes the result.
func loadWithOverlay[T any](module string, merged map[string]any) (*T, error) {
	if err := mergeIfExists(merged, filepath.Join(overlayRoot, module+".yaml")); err != nil {
		return nil, err
	}
	return decodeExact[T](merged)
}

// mergeIfExists deep-merges the YAML file at path into dst. A missing file is skipped.
func mergeIfExists(dst map[string]any, path string) error {
	src, err := readYAML(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: read %q: %w", path, err)
	}
	mergeMaps(dst, src)
	return nil
}

func readYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// mergeMaps merges src into dst. Nested maps merge key by key; any other value replaces.
func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeMaps(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// decodeExact decodes merged into a new T using yaml struct tags. Fields tagged
// yaml:"-" are skipped. A key with no field, or a value of the wrong type, is an error.
func decodeExact[T any](merged map[string]any) (*T, error) {
	var cfg T
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:     "yaml",
		ErrorUnused: true,
		DecodeHook:  jsonDecodeHook,
		Result:      &cfg,
	})
	if err != nil {
		return nil, fmt.Errorf("config: decoder: %w", err)
	}
	if err := dec.Decode(merged); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return &cfg, nil
}

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// jsonDecodeHook decodes Kubernetes API types and json.Unmarshaler implementations
// through encoding/json, so their json tags and formats such as "500m" apply.
func jsonDecodeHook(_, to reflect.Type, data any) (any, error) {
	if to == reflect.TypeFor[time.Duration]() {
		value, ok := data.(string)
		if !ok {
			return nil, fmt.Errorf("expected duration string, got %T", data)
		}

		duration, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("parse duration %q: %w", value, err)
		}

		return duration, nil
	}

	if !reflect.PointerTo(to).Implements(jsonUnmarshalerType) &&
		!strings.HasPrefix(to.PkgPath(), "k8s.io/") {
		return data, nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", to, err)
	}

	out := reflect.New(to)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(out.Interface()); err != nil {
		return nil, fmt.Errorf("decode %s: %w", to, err)
	}

	return out.Elem().Interface(), nil
}

// resolvePath builds the absolute path to
// {projectRoot}/{module}/config/{name}.yaml.
func resolvePath(module, name string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	root, err := findProjectRoot(cwd)
	if err != nil {
		// Graceful fallback: treat CWD as root (e.g. distroless containers).
		root = cwd
	}

	return filepath.Join(root, module, "config", name+".yaml"), nil
}

// findProjectRoot walks up the directory tree from startDir looking for a
// go.mod file. Returns the directory that contains go.mod, or an error if
// the filesystem root is reached without finding it.
func findProjectRoot(startDir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(startDir)
	if err != nil {
		resolved = startDir
	}

	dir := resolved
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat go.mod in %q: %w", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory of %q", startDir)
		}
		dir = parent
	}
}
