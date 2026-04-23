package hostagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aero123421/edge-fabric/internal/protocol/usbcdc"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type failingRouter struct{}

func (f failingRouter) Ingest(context.Context, *contracts.Envelope, string) (*siterouter.PersistAck, error) {
	return nil, errors.New("router unavailable")
}

func openAgentAndRouter(t *testing.T) (*Agent, *siterouter.Router) {
	t.Helper()
	tempDir := t.TempDir()
	router, err := siterouter.Open(filepath.Join(tempDir, "site-router.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
	})
	agent := New(router, filepath.Join(tempDir, "host-agent-spool.jsonl"))
	return agent, router
}

func TestUSBRelay(t *testing.T) {
	agent, router := openAgentAndRouter(t)
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-usb-001",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-usb-001",
		Source:        contracts.SourceRef{HardwareID: "battery-01"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "battery_low"},
	}
	frame, err := EncodeEnvelopeFrame(envelope)
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RelayUSBFrame(context.Background(), "gateway-usb-01", "usb-session-01", frame, map[string]any{
		"rssi":      -111,
		"snr":       6.25,
		"hop_count": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "persisted" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	count, err := router.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestDirectIPRelay(t *testing.T) {
	agent, router := openAgentAndRouter(t)
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-wifi-001",
		Kind:          "state",
		Priority:      "normal",
		Source: contracts.SourceRef{
			HardwareID: "powered-01",
			SessionID:  "sess-02",
			SeqLocal:   intPtr(9),
		},
		Target:  contracts.TargetRef{Kind: "service", Value: "state"},
		Payload: map[string]any{"state_key": "tank.level", "value": 77},
	}
	result, err := agent.RelayDirectIP(context.Background(), "wifi-direct-01", "wifi-session-01", envelope, map[string]any{"rssi": -43})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "persisted" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	state, err := router.LatestState(context.Background(), "powered-01", "tank.level")
	if err != nil {
		t.Fatal(err)
	}
	if state["value"].(float64) != 77 {
		t.Fatalf("expected 77, got %v", state["value"])
	}
}

func TestRouterFailureSpools(t *testing.T) {
	tempDir := t.TempDir()
	agent := New(failingRouter{}, filepath.Join(tempDir, "host-agent-spool.jsonl"))
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-spool-001",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-spool-001",
		Source:        contracts.SourceRef{HardwareID: "battery-02"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "water"},
	}
	result, err := agent.RelayDirectIP(context.Background(), "wifi-direct-fail", "wifi-session-fail", envelope, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "spooled" {
		t.Fatalf("expected spooled, got %s", result.Status)
	}
	diag, err := agent.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if diag["spool_records"].(int) != 1 {
		t.Fatalf("expected 1 spool record, got %v", diag["spool_records"])
	}
}

func TestHeartbeatStoredSeparately(t *testing.T) {
	agent, _ := openAgentAndRouter(t)
	frame, err := EncodeHeartbeatFrame(map[string]any{"gateway_id": "gw-01", "live": true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RelayUSBFrame(context.Background(), "gateway-usb-01", "heartbeat-session-01", frame, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "heartbeat_recorded" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	diag, err := agent.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if diag["spool_records"].(int) != 0 {
		t.Fatalf("expected no spool records, got %v", diag["spool_records"])
	}
}

func TestMalformedInputRejected(t *testing.T) {
	agent, _ := openAgentAndRouter(t)
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-bad-command-result",
		Kind:          "command_result",
		Priority:      "control",
		CommandID:     "cmd-bad-02",
		Source:        contracts.SourceRef{HardwareID: "servo-03"},
		Target:        contracts.TargetRef{Kind: "client", Value: "controller-03"},
		Payload:       map[string]any{"phase": "bad_phase", "command_id": "cmd-bad-02"},
	}
	result, err := agent.RelayDirectIP(context.Background(), "wifi-direct-02", "wifi-session-02", envelope, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", result.Status)
	}
	diag, err := agent.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if diag["reject_records"].(int) != 1 {
		t.Fatalf("expected 1 reject record, got %v", diag["reject_records"])
	}
}

func TestRelayDoesNotMutateOriginalEnvelope(t *testing.T) {
	agent, _ := openAgentAndRouter(t)
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-no-mutate-001",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-no-mutate-001",
		Source:        contracts.SourceRef{HardwareID: "battery-03"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "door_open"},
	}
	if _, err := agent.RelayDirectIP(context.Background(), "wifi-direct-03", "wifi-session-03", envelope, map[string]any{"rssi": -55}); err != nil {
		t.Fatal(err)
	}
	if envelope.Delivery != nil {
		t.Fatal("original envelope must not be mutated")
	}
}

func TestFlushSpoolHandlesLargeRecord(t *testing.T) {
	tempDir := t.TempDir()
	router, err := siterouter.Open(filepath.Join(tempDir, "site-router.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
	})
	agent := New(router, filepath.Join(tempDir, "host-agent-spool.jsonl"))
	large := strings.Repeat("x", 70*1024)
	record := map[string]any{
		"record_type": "envelope",
		"ingress_id":  "wifi-large-01",
		"session_id":  "wifi-session-large-01",
		"transport":   "wifi_ip",
		"envelope": map[string]any{
			"schema_version": "1.0.0",
			"message_id":     "msg-large-001",
			"kind":           "event",
			"priority":       "critical",
			"event_id":       "evt-large-001",
			"source":         map[string]any{"hardware_id": "sensor-large-01"},
			"target":         map[string]any{"kind": "service", "value": "alerts"},
			"payload":        map[string]any{"blob": large},
		},
	}
	if err := os.WriteFile(agent.spoolPath, append(mustJSON(record), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	flushed, err := agent.FlushSpool(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if flushed != 1 {
		t.Fatalf("expected 1 flushed record, got %d", flushed)
	}
}

func TestCompactSummaryCommandResultRelaysIntoRouter(t *testing.T) {
	agent, router := openAgentAndRouter(t)
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-compact-init",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-compact-001",
		Source:        contracts.SourceRef{HardwareID: "controller-compact"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-leaf-01"},
		Payload:       map[string]any{"command_name": "mode.set", "mode": "eco"},
	}
	if _, err := router.Ingest(context.Background(), command, "local"); err != nil {
		t.Fatal(err)
	}
	frame, err := usbcdc.EncodeFrame(FrameSummaryBinary, []byte("R|sleepy-leaf-01|cmd-compact-001|succeeded|ok"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RelayUSBFrame(context.Background(), "gateway-usb-compact", "compact-session-01", frame, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "persisted" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	state, err := router.CommandState(context.Background(), "cmd-compact-001")
	if err != nil {
		t.Fatal(err)
	}
	if state != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state)
	}
}

func TestDecodeCompactSummaryPreservesWireShape(t *testing.T) {
	envelope, status, err := decodeCompactSummaryEnvelope(FrameCompactBinary, []byte("S|sleepy-leaf-01|node.power|awake|1"))
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("unexpected status: %s", status)
	}
	if envelope.Payload["shape"] != "state_compact_v1" {
		t.Fatalf("unexpected shape: %v", envelope.Payload["shape"])
	}
	if envelope.Payload["wire_shape"] != "compact_v1" {
		t.Fatalf("unexpected wire_shape: %v", envelope.Payload["wire_shape"])
	}
	if envelope.Payload["codec_family"] != "compact_binary_v1" {
		t.Fatalf("unexpected codec_family: %v", envelope.Payload["codec_family"])
	}
}

func TestSummaryEventRelaysIntoRouter(t *testing.T) {
	agent, router := openAgentAndRouter(t)
	frame, err := usbcdc.EncodeFrame(FrameSummaryBinary, []byte("E|sleepy-leaf-02|evt-compact-001|leak"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RelayUSBFrame(context.Background(), "gateway-usb-summary", "summary-session-01", frame, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "persisted" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	count, err := router.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestDigestAndPollFramesAreRecordedOnly(t *testing.T) {
	agent, router := openAgentAndRouter(t)
	for _, payload := range []string{"D|sleepy-leaf-01|1", "P|sleepy-leaf-01|TP|ENP"} {
		frame, err := usbcdc.EncodeFrame(FrameSummaryBinary, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		result, err := agent.RelayUSBFrame(context.Background(), "gateway-usb-summary", "summary-session-02", frame, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "digest_recorded" && result.Status != "poll_recorded" {
			t.Fatalf("unexpected status for %s: %s", payload, result.Status)
		}
	}
	count, err := router.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 events after record-only frames, got %d", count)
	}
}

func TestInvalidCompactPayloadRejected(t *testing.T) {
	if _, _, err := decodeCompactSummaryEnvelope(FrameCompactBinary, []byte("R|missing-fields")); err == nil {
		t.Fatal("expected invalid compact payload error")
	}
}

func TestSummaryCommandResultUsesSummaryMetadata(t *testing.T) {
	envelope, status, err := decodeCompactSummaryEnvelope(FrameSummaryBinary, []byte("R|sleepy-leaf-01|cmd-sum-001|succeeded|ok"))
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("unexpected status: %s", status)
	}
	if envelope.Payload["shape"] != "command_result_summary_v1" {
		t.Fatalf("unexpected shape: %v", envelope.Payload["shape"])
	}
	if envelope.Payload["wire_shape"] != "summary_v1" {
		t.Fatalf("unexpected wire_shape: %v", envelope.Payload["wire_shape"])
	}
	if envelope.Payload["codec_family"] != "summary_binary_v1" {
		t.Fatalf("unexpected codec_family: %v", envelope.Payload["codec_family"])
	}
}

func intPtr(value int) *int {
	return &value
}
