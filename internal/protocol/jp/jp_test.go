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
