// Package migrations embeds the SQL migration files applied at startup.
package migrations

import "embed"

// FS contains the ordered .sql migrations.
//
//go:embed *.sql
var FS embed.FS
