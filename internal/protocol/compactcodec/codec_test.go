package compactcodec

import (
	"path/filepath"
	"runtime"
	"testing"
)

func contractsPath(parts ...string) string {
	_, filename, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(filename), "..", "..", "..", "contracts", "protocol")
	all := append([]string{base}, parts...)
	return filepath.Join(all...)
}

func TestRegistryLoadsCompactShapes(t *testing.T) {
	registry, err := LoadRegistry(contractsPath("compact-codecs.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.FrameTypeSpec(FrameCompactBinary)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "compact_binary_v1" || spec.WireShape != "compact_v1" {
		t.Fatalf("unexpected frame spec: %+v", spec)
	}
	shape, err := registry.ShapeFor("R", FrameSummaryBinary)
	if err != nil {
		t.Fatal(err)
	}
	if shape != "command_result_summary_v1" {
		t.Fatalf("unexpected shape: %s", shape)
	}
}
