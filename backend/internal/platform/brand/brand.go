// Package brand holds the product name every speaking service reads: the one
// word that reaches guardrail messages, the capabilities brief, MCP tool
// descriptions, egress denials and sandbox image descriptions. A white-labeled
// install sets it through the theme document, and the chart renders it into
// this package's config module (docs/adr/2026-10-12-white-labelling-theme.md,
// Decision 5).
//
// Components take the narrow [Brand] value, or an interface with its one
// method, never [Config]: the config is how the name arrives, the brand is
// what a string site needs.
package brand

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Module is the config module path. The chart renders the per-install file at
// /etc/orbit/config/internal/platform/brand.yaml and config/base.yaml holds the
// default.
const Module = "internal/platform/brand"

// EnvProductName is the environment variable a session-manager pod reads for
// the product name. Session pods are created by orbit-agents at runtime, not
// rendered by the chart, so they have no rendered config file to merge; the
// typed env producer in internal/agents sets this variable on each pod it
// builds (docs/adr/2026-10-06-rendered-service-config-files.md, section 6).
// The rule that only secrets travel as env vars is about secrets; this is the
// one non-secret the Go-rendered pods cannot receive any other way.
const EnvProductName = "ORBIT_PRODUCT_NAME"

// Sentinel errors for an unusable product name.
var (
	ErrProductNameEmpty   = errors.New("brand: product name is empty")
	ErrProductNameControl = errors.New("brand: product name contains a control character")
)

// Config is the brand config module: the file shape the chart renders.
type Config struct {
	// ProductName is the name the product goes by in every string a client's
	// people or agents read. The theme contract bounds its length and
	// characters, checked by the frontend's init container; this package only
	// rejects a name no string can carry.
	ProductName string `yaml:"productName"`
}

// Brand is the resolved brand a component reads. Construct it with [New] or
// [Load]; the zero value has no product name.
type Brand struct {
	productName string
}

// ProductName is the configured product name.
func (b Brand) ProductName() string { return b.productName }

// New validates productName and returns the brand that carries it. The name
// must be non-empty and free of control characters; every other bound is the
// theme contract's and is not re-checked here.
func New(productName string) (Brand, error) {
	if strings.TrimSpace(productName) == "" {
		return Brand{}, ErrProductNameEmpty
	}
	for _, r := range productName {
		if unicode.IsControl(r) {
			return Brand{}, fmt.Errorf("%w: %q", ErrProductNameControl, productName)
		}
	}
	return Brand{productName: productName}, nil
}

// MustNew is [New] for wiring and tests, where a bad literal is a programming error.
func MustNew(productName string) Brand {
	b, err := New(productName)
	if err != nil {
		panic(err)
	}
	return b
}

// Load reads the brand config module through the typed loader, config/base.yaml
// under the environment overlay and the chart-rendered file last, then applies
// the session-pod environment variable when it is set, and validates the result.
func Load() (Brand, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Brand, error) {
	cfg, err := config.LoadService[Config](Module, config.Environment())
	if err != nil {
		return Brand{}, fmt.Errorf("brand: load config: %w", err)
	}
	if v := getenv(EnvProductName); v != "" {
		cfg.ProductName = v
	}
	b, err := New(cfg.ProductName)
	if err != nil {
		return Brand{}, fmt.Errorf("brand: productName: %w", err)
	}
	return b, nil
}
