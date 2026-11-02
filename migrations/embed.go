// Package migrations holds the SQL schema migrations, applied by goose on start.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
