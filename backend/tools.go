//go:build tools

// Package tools pins github.com/quasilyte/go-ruleguard/dsl, which
// rules/kora_rules.go imports; without it ruleguard loads no rules and fails silently.
package tools

import (
	_ "github.com/quasilyte/go-ruleguard/dsl"
)
