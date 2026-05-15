package siterouter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const routerApplicationID = 0x45465253 // EFRS

type schemaMigration struct {
	Version    int
	Name       string
	Statements []string
}

var routerMigrations = []schemaMigration{
	{
		Version: 1,
		Name:    "initial_inline_schema",
		Statements: []string{
			"inline schema creates messages/event/latest_state/command/outbox/node/heartbeat/file tables",
			"ensureOperationalSchema backfills v1 operational columns and indexes",
		},
	},
}

func (r *Router) runMigrations(ctx context.Context) error {
	if err := r.ensureApplicationID(ctx); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL,
			checksum TEXT NOT NULL
		)
	`); err != nil {
		return err
	}
	for _, migration := range routerMigrations {
		if err := r.applyMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func (r *Router) ensureApplicationID(ctx context.Context) error {
	var applicationID int
	if err := r.db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&applicationID); err != nil {
		return err
	}
	if applicationID != 0 && applicationID != routerApplicationID {
		return fmt.Errorf("unexpected sqlite application_id %d", applicationID)
	}
	if applicationID == routerApplicationID {
		return nil
	}
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA application_id = %d`, routerApplicationID))
	return err
}

func (r *Router) applyMigration(ctx context.Context, migration schemaMigration) error {
	checksum := migration.checksum()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var appliedChecksum string
	err = tx.QueryRowContext(ctx, `
		SELECT checksum
		FROM schema_migrations
		WHERE version = ?
	`, migration.Version).Scan(&appliedChecksum)
	switch {
	case err == nil:
		if appliedChecksum != checksum {
			return fmt.Errorf("schema migration %03d checksum mismatch", migration.Version)
		}
		return tx.Commit()
	case err != nil && err != sql.ErrNoRows:
		return err
	}

	for _, stmt := range migration.Statements {
		if strings.TrimSpace(stmt) == "" || !strings.Contains(stmt, " ") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(stmt), "inline schema") ||
			strings.HasPrefix(strings.TrimSpace(stmt), "ensureOperationalSchema") {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, applied_at, checksum)
		VALUES (?, ?, ?, ?)
	`, migration.Version, migration.Name, time.Now().UTC().Format(time.RFC3339Nano), checksum); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, migration.Version)); err != nil {
		return err
	}
	return tx.Commit()
}

func (m schemaMigration) checksum() string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%03d:%s\n", m.Version, m.Name)
	for _, stmt := range m.Statements {
		_, _ = fmt.Fprintln(h, strings.TrimSpace(stmt))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
