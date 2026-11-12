// Package seed embeds synthetic demo data. Each module may add its own file;
// files are applied in name order.
package seed

import "embed"

//go:embed *.sql
var FS embed.FS
