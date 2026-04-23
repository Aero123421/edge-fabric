package contracts

import "testing"

func TestValidateAllowsFileChunk(t *testing.T) {
	envelope := &Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-file-001",
		Kind:          "file_chunk",
		Priority:      "bulk",
		Source:        SourceRef{HardwareID: "gateway-01"},
		Target:        TargetRef{Kind: "host", Value: "storage"},
		Payload:       map[string]any{"chunk_index": 1, "total_chunks": 4},
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("file_chunk should validate, got %v", err)
	}
}
