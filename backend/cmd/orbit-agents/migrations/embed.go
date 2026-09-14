// Package migrations carries this service's goose SQL migrations. cmd/migrate
// embeds every service's FS into one migration set.
package migrations

import "embed"

// FS holds the migration files. go:embed cannot reach a parent directory, so
// each service embeds its own.
//
//go:embed *.sql
var FS embed.FS
