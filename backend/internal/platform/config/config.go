// Package config provides a typed, environment-aware YAML configuration loader
// for the Orbit control plane.
//
// Each feature owns its config struct and places environment-specific YAML files
// under a config/ subdirectory within its own package directory. The loader
// resolves file paths relative to the project root so that tests and binaries
// work correctly regardless of the working directory.
//
// # Security
//
// YAML config files must not contain secrets (passwords, API keys, certificates).
// Use environment variables or a secret manager for all sensitive values.
//
// # Usage
//
// Config is intended to be loaded once at program startup as a package-level
// variable, following the same convention as [regexp.MustCompile]:
//
//	type serverConfig struct {
//	    Port     int    `yaml:"port"`
//	    BasePath string `yaml:"basePath"`
//	}
//
//	var cfg = config.MustLoad[serverConfig]("cmd/orbit", config.Environment())
//
// MustLoad panics if the file cannot be read or parsed, which surfaces
// misconfiguration immediately at startup rather than silently at first use.
// Use [Load] directly in tests or when you need to handle the error explicitly.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// defaultEnvironment is used when APP_ENV is not set.
const defaultEnvironment = "local"

// Environment reads the APP_ENV environment variable and returns its value.
// If APP_ENV is not set or is empty, it returns "local".
//
// Supported values by convention: "local", "development", "production".
func Environment() string {
	if env := os.Getenv("APP_ENV"); env != "" {
		return env
	}
	return defaultEnvironment
}

// MustLoad is like [Load] but panics if the config file cannot be read or
// parsed. It is intended for package-level variable initialisation where a
// missing config file is a fatal startup error:
//
//	var cfg = config.MustLoad[serverConfig]("cmd/orbit", config.Environment())
func MustLoad[T any](module, environment string) *T {
	cfg, err := Load[T](module, environment)
	if err != nil {
		panic(err)
	}
	return cfg
}

// Load reads the YAML config file for the given module and environment,
// unmarshals it into a new instance of T, and returns a pointer to it.
//
// module is a path relative to the project root (e.g. "internal/identity" or
// "cmd/orbit"). environment is the name of the YAML file without the extension
// (e.g. "production"). The resolved path is:
//
//	{projectRoot}/{module}/config/{environment}.yaml
//
// The project root is determined by walking up from the current working directory
// until a go.mod file is found. In environments where go.mod is absent (e.g.
// inside a minimal container image), the working directory is used as the root.
//
// Config files must not contain secrets. Use environment variables for all
// sensitive values.
func Load[T any](module, environment string) (*T, error) {
	path, err := resolvePath(module, environment)
	if err != nil {
		return nil, fmt.Errorf("config: resolve path for module %q env %q: %w", module, environment, err)
	}

	var cfg T
	if err := parseFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// resolvePath builds the absolute path to the config file. It first attempts
// to locate the project root via go.mod, then falls back to the working
// directory as root for containerised deployments.
func resolvePath(module, environment string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	root, err := findProjectRoot(cwd)
	if err != nil {
		// Graceful fallback: treat CWD as root (e.g. distroless containers).
		root = cwd
	}

	return filepath.Join(root, module, "config", environment+".yaml"), nil
}

// findProjectRoot walks up the directory tree from startDir looking for a
// go.mod file. Returns the directory that contains go.mod, or an error if the
// filesystem root is reached without finding it.
//
// startDir is resolved through symlinks before walking so the returned path is
// always the canonical real path. This matters on macOS where /tmp and /var are
// symlinks into /private, and os.Getwd() returns the real path.
func findProjectRoot(startDir string) (string, error) {
	// Resolve symlinks so the returned root is always a real path.
	resolved, err := filepath.EvalSymlinks(startDir)
	if err != nil {
		// If EvalSymlinks fails (e.g. dir does not exist yet), continue with
		// the original path so the error surfaces naturally below.
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

// parseFile reads the YAML file at path and strictly unmarshals its contents
// into dst. Unknown YAML keys are rejected so that typos in config files are
// caught at startup rather than silently ignored.
// dst must be a non-nil pointer.
func parseFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read file %q: %w", path, err)
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil && err != io.EOF {
		return fmt.Errorf("config: unmarshal %q: %w", path, err)
	}
	return nil
}
