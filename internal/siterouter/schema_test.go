package siterouter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationLedgerIsRecordedForFileDB(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	var applicationID int
	if err := router.db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&applicationID); err != nil {
		t.Fatal(err)
	}
	if applicationID != routerApplicationID {
		t.Fatalf("expected application_id %d, got %d", routerApplicationID, applicationID)
	}
	var version int
	var name string
	var checksum string
	if err := router.db.QueryRowContext(ctx, `
		SELECT version, name, checksum
		FROM schema_migrations
		WHERE version = 1
	`).Scan(&version, &name, &checksum); err != nil {
		t.Fatal(err)
	}
	if version != 1 || name != "initial_inline_schema" || checksum == "" {
		t.Fatalf("unexpected migration row version=%d name=%q checksum=%q", version, name, checksum)
	}
	info, err := router.SchemaInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != 1 {
		t.Fatalf("expected schema version 1, got %+v", info)
	}
}

func TestMigrationLedgerWorksForInMemoryDB(t *testing.T) {
	router, err := Open(":memory:", 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	var count int
	if err := router.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one migration row, got %d", count)
	}
}

func TestMigrationChecksumMismatchFailsFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-router.db")
	router, err := Open(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	if _, err := router.db.ExecContext(context.Background(), `
		UPDATE schema_migrations
		SET checksum = 'tampered'
		WHERE version = 1
	`); err != nil {
		t.Fatal(err)
	}
	err = router.runMigrations(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}
