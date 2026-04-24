package onair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type artifactHeader struct {
	Version   int `json:"version"`
	SizeBytes int `json:"size_bytes"`
}

type artifactLogicalType struct {
	Name                 string `json:"name"`
	ImplementationStatus string `json:"implementation_status"`
}

type artifactFile struct {
	Version      string                         `json:"version"`
	Track        string                         `json:"track"`
	Header       artifactHeader                 `json:"header"`
	LogicalTypes map[string]artifactLogicalType `json:"logical_types"`
}

func TestOnAirArtifactStaysInSync(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "contracts", "protocol", "onair-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var artifact artifactFile
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Track != "mainline" {
		t.Fatalf("unexpected track: %s", artifact.Track)
	}
	if artifact.Header.Version != int(Version) {
		t.Fatalf("artifact version=%d code version=%d", artifact.Header.Version, Version)
	}
	if artifact.Header.SizeBytes != HeaderSize {
		t.Fatalf("artifact header size=%d code header size=%d", artifact.Header.SizeBytes, HeaderSize)
	}
	if artifact.LogicalTypes["1"].Name != "state" || artifact.LogicalTypes["1"].ImplementationStatus != "active" {
		t.Fatalf("unexpected state entry: %+v", artifact.LogicalTypes["1"])
	}
	if artifact.LogicalTypes["3"].Name != "command_result" || artifact.LogicalTypes["3"].ImplementationStatus != "active" {
		t.Fatalf("unexpected command_result entry: %+v", artifact.LogicalTypes["3"])
	}
	if artifact.LogicalTypes["2"].Name != "event" || artifact.LogicalTypes["2"].ImplementationStatus != "active" {
		t.Fatalf("unexpected event entry: %+v", artifact.LogicalTypes["2"])
	}
	if artifact.LogicalTypes["7"].Name != "heartbeat" || artifact.LogicalTypes["7"].ImplementationStatus != "active" {
		t.Fatalf("unexpected heartbeat entry: %+v", artifact.LogicalTypes["7"])
	}
}
