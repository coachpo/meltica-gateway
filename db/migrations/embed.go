// Package dbmigrations exposes embedded SQL migrations for Meltica binaries.
package dbmigrations

import "embed"

// Files contains the embedded SQL migrations bundled into Meltica binaries.
//
//go:embed *.sql
var Files embed.FS
