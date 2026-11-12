// Package migrations embeds the SQL schema. Name new files with the next number
// and the module name, e.g. 0003_messages.sql; never edit an applied file.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
