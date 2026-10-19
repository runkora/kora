// Package brand holds the product name that services put in text people or agents read.
// Components take [Brand] or an interface with its one method, never [Config].
package brand

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/runkora/kora/backend/internal/platform/config"
)

// Module is the config module path; the chart renders the per-install file for it.
const Module = "internal/platform/brand"

// EnvProductName sets the product name on session-manager pods, which the chart does not render.
// It is the one non-secret passed as an env var.
const EnvProductName = "ORBIT_PRODUCT_NAME"

// Sentinel errors for an unusable product name.
var (
	ErrProductNameEmpty   = errors.New("brand: product name is empty")
	ErrProductNameControl = errors.New("brand: product name contains a control character")
)

// Config is the brand config module: the file shape the chart renders.
type Config struct {
	// ProductName is the name used in every string people or agents read.
	// Length and character bounds belong to the theme contract, not this package.
	ProductName string `yaml:"productName"`
}

// Brand is the resolved brand a component reads. The zero value has no product name.
type Brand struct {
	productName string
}

// ProductName is the configured product name.
func (b Brand) ProductName() string { return b.productName }

// New returns a Brand for productName. It rejects an empty name or one with control characters.
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

// Load reads the brand config, applies EnvProductName when set, and validates the result.
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
