package dbmigrations

import "embed"

// Files contains the embedded SQL migrations bundled into Meltica binaries.
//
//go:embed *.sql
var Files embed.FS
