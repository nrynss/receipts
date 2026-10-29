package store

import (
	"context"
	"io/fs"

	"github.com/nrynss/keel/sqlite"
)

// MigrateAccounts applies the account/session migration to the application's
// shared Keel SQLite database. It uses the ledger namespace so fresh and
// existing installations advance one ordered migration stream.
func MigrateAccounts(ctx context.Context, db *sqlite.DB) error {
	if db == nil {
		return ErrInvalid
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return sqlite.Migrate(ctx, db, migrationNamespace, migrations)
}
