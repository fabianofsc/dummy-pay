// Package migrations embeds the schema's goose SQL files so the service
// binary and the test harness apply migrations from the same source
// (ADR-0011).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
