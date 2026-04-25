package jp

import (
	"path/filepath"
	"testing"
)

func TestBodyCapForProfile(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "protocol", "jp-safe-profiles.json")
	cap, err := BodyCapForProfile(path, "JP125_LONG_SF10", false)
	if err != nil {
		t.Fatal(err)
	}
	if cap != 10 {
		t.Fatalf("expected 10, got %d", cap)
	}
	relayedCap, err := BodyCapForProfile(path, "JP125_LONG_SF10", true)
	if err != nil {
		t.Fatal(err)
	}
	if relayedCap != 6 {
		t.Fatalf("expected 6, got %d", relayedCap)
	}
}

func TestUnknownProfileRejected(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "protocol", "jp-safe-profiles.json")
	if _, err := BodyCapForProfile(path, "UNKNOWN", false); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestAirtimeMSForProfileIsMonotonic(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "protocol", "jp-safe-profiles.json")
	small, err := AirtimeMSForProfile(path, "JP125_LONG_SF10", 18)
	if err != nil {
		t.Fatal(err)
	}
	large, err := AirtimeMSForProfile(path, "JP125_LONG_SF10", 24)
	if err != nil {
		t.Fatal(err)
	}
	if small <= 0 || large <= small {
		t.Fatalf("expected airtime to grow with payload size, small=%d large=%d", small, large)
	}
	if large < 300 || large > 450 {
		t.Fatalf("unexpected JP125_LONG_SF10 airtime for 24 bytes: %dms", large)
	}
}
