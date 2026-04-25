package devfixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

const (
	sleepyManifestPath = "contracts/fixtures/manifest-sleepy-leaf.json"
	sleepyLeasePath    = "contracts/fixtures/lease-sleepy-leaf.json"
)

func SeedBuiltIn(ctx context.Context, router *siterouter.Router, root string) ([]string, error) {
	if root == "" {
		root = "."
	}
	manifest, err := LoadManifest(filepath.Join(root, sleepyManifestPath))
	if err != nil {
		return nil, err
	}
	if err := router.UpsertManifest(ctx, manifest.HardwareID, manifest); err != nil {
		return nil, err
	}
	lease, err := LoadLease(filepath.Join(root, sleepyLeasePath))
	if err != nil {
		return nil, err
	}
	if err := router.UpsertLease(ctx, manifest.HardwareID, lease); err != nil {
		return nil, err
	}
	return []string{"manifest:" + manifest.HardwareID, "lease:" + manifest.HardwareID}, nil
}

func LoadManifest(path string) (*contracts.Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest contracts.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("load manifest %s: %w", path, err)
	}
	if manifest.HardwareID == "" {
		return nil, fmt.Errorf("manifest %s missing hardware_id", path)
	}
	return &manifest, nil
}

func LoadLease(path string) (*contracts.Lease, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lease contracts.Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return nil, fmt.Errorf("load lease %s: %w", path, err)
	}
	return &lease, nil
}
