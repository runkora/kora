// Package config provides a typed, environment-aware configuration loader
// for the Orbit control plane.
//
// # Loading modes
//
// Use [Load] for packages that own a single config file (e.g. storage):
//
//	var cfg = config.MustLoad[StorageConfig]("internal/platform/storage", config.Environment())
//
// Use [LoadService] for service entry points under cmd/. It merges two YAML
// layers in order — service-only, no platform config included:
//
//  1. {serviceModule}/config/base.yaml   — service defaults (optional)
//
//  2. {serviceModule}/config/{env}.yaml  — service env overrides (optional)
//
//     var cfg = config.MustLoadService[Config]("cmd/identity", config.Environment())
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
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// defaultEnvironment is used when APP_ENV is not set.
// Production is the safe default — explicit env var overrides (e.g. ORBIT_POSTGRES_SSL_MODE,
// ORBIT_REDIS_TLS_ENABLED) are used by local dev tooling instead of changing this.
const defaultEnvironment = "production"

// Environment reads the APP_ENV environment variable.
// Returns "production" if APP_ENV is unset or empty.
// Local development binaries set APP_ENV=local via the .env file (make dev).
// Chart deployments no longer inject APP_ENV — production is always the default in cluster.
//
// Supported values by convention: "local", "development", "production".
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

// Load reads and unmarshals the YAML config file at
// {projectRoot}/{module}/config/{environment}.yaml into a new T.
//
// Use this for packages that own a single config file. For service binaries
// that need the four-layer platform + per-service merge, use [LoadService].
func Load[T any](module, environment string) (*T, error) {
	path, err := resolvePath(module, environment)
	if err != nil {
		return nil, fmt.Errorf("config: resolve path for module %q env %q: %w", module, environment, err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	return unmarshalExact[T](v)
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

// LoadService loads a service binary's configuration by merging two
// service-specific YAML layers into T:
//
//  1. {serviceModule}/config/base.yaml   — service defaults (optional)
//  2. {serviceModule}/config/{env}.yaml  — service env overrides (optional)
//
// Only service-owned keys are loaded here (e.g. server.port, observability.serviceName).
// Platform-wide settings (OTLP endpoint, sampling rate, log format) are owned by
// the observability package and loaded independently via [observability.DefaultConfig].
func LoadService[T any](serviceModule, environment string) (*T, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	// Layer 1: service base (optional).
	serviceBase, err := resolvePath(serviceModule, "base")
	if err != nil {
		return nil, fmt.Errorf("config: resolve service base path: %w", err)
	}
	err = mergeIfExists(v, serviceBase)
	if err != nil {
		return nil, fmt.Errorf("config: merge service base config %q: %w", serviceBase, err)
	}

	// Layer 2: service env override (optional).
	serviceEnv, err := resolvePath(serviceModule, environment)
	if err != nil {
		return nil, fmt.Errorf("config: resolve service env path: %w", err)
	}
	if err := mergeIfExists(v, serviceEnv); err != nil {
		return nil, fmt.Errorf("config: merge service env config %q: %w", serviceEnv, err)
	}

	return unmarshalExact[T](v)
}

// mergeIfExists merges the YAML file at path into v. If the file does not
// exist it is silently skipped. Any other error is returned.
func mergeIfExists(v *viper.Viper, path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	v.SetConfigFile(path)
	return v.MergeInConfig()
}

// unmarshalExact decodes the Viper instance into a new T using yaml struct
// tags. Fields tagged yaml:"-" are skipped. Unknown keys in the config file
// that have no corresponding struct field cause an error.
func unmarshalExact[T any](v *viper.Viper) (*T, error) {
	var cfg T
	err := v.UnmarshalExact(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	})
	if err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return &cfg, nil
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
