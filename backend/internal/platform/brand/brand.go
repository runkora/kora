// Package brand holds the product name that services put in text people or
// agents read: guardrail messages, the capabilities brief, MCP tool
// descriptions, egress denials and sandbox image descriptions.
//
// A white-labelled install sets the name in the theme document. The chart
// renders it into this package's config module.
//
// Components take the narrow [Brand] value, or an interface with its one
// method, never [Config]. Config is how the name arrives. Brand is what a
// string needs.
package brand

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Module is the config module path. config/base.yaml holds the default. The
// chart renders the per-install file at /etc/orbit/config/internal/platform/brand.yaml.
const Module = "internal/platform/brand"

// EnvProductName is the environment variable that sets the product name on a
// session-manager pod.
//
// orbit-agents creates session pods at runtime, so the chart cannot render a
// config file for them. The env producer in internal/agents sets this variable
// on each pod it builds. Env vars are otherwise for secrets only
// (docs/adr/2026-10-06-rendered-service-config-files.md, section 6). This is
// the one non-secret that Go-rendered pods cannot receive another way.
const EnvProductName = "ORBIT_PRODUCT_NAME"

// Sentinel errors for an unusable product name.
var (
	ErrProductNameEmpty   = errors.New("brand: product name is empty")
	ErrProductNameControl = errors.New("brand: product name contains a control character")
)

// Config is the brand config module: the file shape the chart renders.
type Config struct {
	// ProductName is the name used in every string people or agents read.
	// The theme contract bounds its length and characters, and the frontend's
	// init container checks them. This package only rejects a name no string
	// can carry.
	ProductName string `yaml:"productName"`
}

// Brand is the resolved brand a component reads. Construct it with [New] or
// [Load]; the zero value has no product name.
type Brand struct {
	productName string
}

// ProductName is the configured product name.
func (b Brand) ProductName() string { return b.productName }

// New returns a Brand for productName. The name must be non-empty and have no
// control characters. The theme contract owns every other bound.
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

// Load reads the brand config module: base.yaml, the environment overlay, then
// the chart-rendered file. It applies EnvProductName when set, then validates
// the result.
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
