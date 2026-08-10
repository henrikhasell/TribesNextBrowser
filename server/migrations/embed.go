// Package migrations carries the schema as files compiled into the binary.
//
// The declaration lives beside the SQL rather than under internal/ for two
// reasons: go:embed cannot reach outside its own package directory, and this
// path is the one server/README.md documents, so `psql -f migrations/0001_init.sql`
// keeps working for anyone with a database in front of them. The embed is an
// addition, not a replacement.
package migrations

import "embed"

// FS holds every migration, named NNNN_description.sql and applied in
// filename order. See internal/migrate.
//
//go:embed *.sql
var FS embed.FS
