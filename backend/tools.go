//go:build tools

// Package main (tools) pins build-time-only tool dependencies so `go mod tidy`
// keeps them in the module graph without compiling them into any binary.
//
// github.com/quasilyte/go-ruleguard/dsl is imported by rules/orbit_rules.go
// (outside this module, loaded at lint-time by golangci-lint's gocritic/
// ruleguard integration). Ruleguard type-checks that file using this module's
// dependency graph, so the dsl package must be resolvable here even though no
// application code imports it directly. Without this pin, `go mod tidy` prunes
// the dependency, ruleguard's importer fails silently, and every custom rule
// in rules/orbit_rules.go (see docs/adr/2026-03-21-opentelemetry-observability-
// discipline.md) silently stops firing.
package tools

import (
	_ "github.com/quasilyte/go-ruleguard/dsl"
)
