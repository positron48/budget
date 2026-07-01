package migrations

import "embed"

// FS embeds all SQL migration files so they can be applied automatically
// at application startup (no external migrate CLI required on deploy).
//
//go:embed *.sql
var FS embed.FS
