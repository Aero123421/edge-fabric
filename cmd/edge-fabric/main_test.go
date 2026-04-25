package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIssueCommandAndDescribeProfileCommands(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	dbPath := filepath.Join(t.TempDir(), "site.db")
	if err := run([]string{
		"issue-command",
		"-db", dbPath,
		"-seed-fixtures",
		"-fixture", filepath.Join("contracts", "fixtures", "command-sleepy-threshold-set.json"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{
		"describe-profile",
		"-profile", "motion_sensor_battery_v1",
	}); err != nil {
		t.Fatal(err)
	}
}
