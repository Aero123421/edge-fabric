package compactcodec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	FrameCompactBinary byte = 3
	FrameSummaryBinary byte = 4
)

type FrameTypeSpec struct {
	Name      string `json:"name"`
	WireShape string `json:"wire_shape"`
}

type LogicalKindSpec struct {
	Kind             string            `json:"kind"`
	ShapeByFrameType map[string]string `json:"shape_by_frame_type"`
	IngestBehavior   string            `json:"ingest_behavior"`
}

type Registry struct {
	Version      string                     `json:"version"`
	FrameTypes   map[string]FrameTypeSpec   `json:"frame_types"`
	LogicalKinds map[string]LogicalKindSpec `json:"logical_kinds"`
}

func DefaultRegistryPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "contracts", "protocol", "compact-codecs.json")
}

func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return &registry, nil
}

func (r *Registry) FrameTypeSpec(frameType byte) (*FrameTypeSpec, error) {
	spec, ok := r.FrameTypes[fmt.Sprintf("%d", frameType)]
	if !ok {
		return nil, fmt.Errorf("unknown compact codec frame type: %d", frameType)
	}
	return &spec, nil
}

func (r *Registry) ShapeFor(logicalKey string, frameType byte) (string, error) {
	spec, ok := r.LogicalKinds[logicalKey]
	if !ok {
		return "", fmt.Errorf("unknown compact logical kind: %s", logicalKey)
	}
	shape, ok := spec.ShapeByFrameType[fmt.Sprintf("%d", frameType)]
	if !ok || shape == "" {
		return "", fmt.Errorf("no compact shape for logical kind %s and frame type %d", logicalKey, frameType)
	}
	return shape, nil
}
