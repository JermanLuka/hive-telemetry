// Package migrations holds the SQL schema migrations, embedded into the
// binary so a deployed program needs no separate files.
package migrations

import "embed"

// FS contains every migration file. cmd/migrate applies them in order.
//
//go:embed *.sql
var FS embed.FS
