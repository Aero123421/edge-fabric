package hostagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Aero123421/edge-fabric/internal/protocol/compactcodec"
	"github.com/Aero123421/edge-fabric/internal/protocol/usbcdc"
	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

const (
	FrameEnvelopeJSON  byte = 1
	FrameHeartbeatJSON byte = 2
	FrameCompactBinary byte = 3
	FrameSummaryBinary byte = 4
)

type IngressObservation struct {
	IngressID  string   `json:"ingress_id"`
	SessionID  string   `json:"session_id"`
	Transport  string   `json:"transport"`
	ReceivedAt string   `json:"received_at"`
	FrameType  *byte    `json:"frame_type,omitempty"`
	RSSI       *int     `json:"rssi,omitempty"`
	SNR        *float64 `json:"snr,omitempty"`
	HopCount   *int     `json:"hop_count,omitempty"`
}

type RelayResult struct {
	Status      string
	Ack         *siterouter.PersistAck
	Spooled     bool
	Observation IngressObservation
}

type Ingester interface {
	Ingest(context.Context, *contracts.Envelope, string) (*siterouter.PersistAck, error)
}

type Agent struct {
	router        Ingester
	spoolPath     string
	rejectsPath   string
	heartbeatPath string
	mu            sync.Mutex
}

func New(router Ingester, spoolPath string) *Agent {
	return &Agent{
		router:        router,
		spoolPath:     spoolPath,
		rejectsPath:   stringsTrimSuffix(spoolPath, filepath.Ext(spoolPath)) + ".rejected.jsonl",
		heartbeatPath: stringsTrimSuffix(spoolPath, filepath.Ext(spoolPath)) + ".heartbeat.json",
	}
}

func (a *Agent) RelayUSBFrame(ctx context.Context, ingressID, sessionID string, frame []byte, metadata map[string]any) (*RelayResult, error) {
	frameType, payload, err := usbcdc.DecodeFrame(frame)
	if err != nil {
		return nil, err
	}
	observation := newObservation(ingressID, sessionID, "usb_cdc", &frameType, metadata)
	if frameType == FrameHeartbeatJSON {
		var heartbeat map[string]any
		if err := json.Unmarshal(payload, &heartbeat); err != nil {
			return nil, err
		}
		if err := atomicWriteFile(a.heartbeatPath, mustJSON(map[string]any{
			"observation": observation,
			"payload":     heartbeat,
		}), 0o644); err != nil {
			return nil, err
		}
		return &RelayResult{Status: "heartbeat_recorded", Observation: observation}, nil
	}
	if frameType == FrameCompactBinary || frameType == FrameSummaryBinary {
		envelope, status, err := decodeCompactSummaryEnvelope(frameType, payload)
		if err != nil {
			return nil, err
		}
		if envelope == nil {
			return &RelayResult{Status: status, Observation: observation}, nil
		}
		return a.relayEnvelope(ctx, ingressID, sessionID, "usb_cdc", &observation, envelope)
	}
	if frameType != FrameEnvelopeJSON {
		return nil, errors.New("unsupported USB frame type")
	}
	var envelope contracts.Envelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	return a.relayEnvelope(ctx, ingressID, sessionID, "usb_cdc", &observation, &envelope)
}

func (a *Agent) RelayDirectIP(ctx context.Context, ingressID, sessionID string, envelope *contracts.Envelope, metadata map[string]any) (*RelayResult, error) {
	observation := newObservation(ingressID, sessionID, "wifi_ip", nil, metadata)
	return a.relayEnvelope(ctx, ingressID, sessionID, "wifi_ip", &observation, envelope)
}

func (a *Agent) relayEnvelope(ctx context.Context, ingressID, sessionID, transport string, observation *IngressObservation, envelope *contracts.Envelope) (*RelayResult, error) {
	cloned, err := cloneEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	if cloned.Delivery == nil {
		cloned.Delivery = &contracts.DeliverySpec{}
	}
	if cloned.Delivery.IngressMeta == nil {
		cloned.Delivery.IngressMeta = map[string]any{}
	}
	cloned.Delivery.IngressMeta["transport"] = transport
	cloned.Delivery.IngressMeta["session_id"] = sessionID
	cloned.Delivery.IngressMeta["received_at"] = observation.ReceivedAt
	cloned.Delivery.IngressMeta["ingress_id"] = ingressID
	if observation.RSSI != nil {
		cloned.Delivery.IngressMeta["rssi"] = *observation.RSSI
	}
	if observation.SNR != nil {
		cloned.Delivery.IngressMeta["snr"] = *observation.SNR
	}
	if observation.HopCount != nil {
		cloned.Delivery.IngressMeta["hop_count"] = *observation.HopCount
	}

	ack, err := a.router.Ingest(ctx, cloned, ingressID)
	if err == nil {
		return &RelayResult{Status: ack.Status, Ack: ack, Observation: *observation}, nil
	}
	var validationError *contracts.ValidationError
	if errors.As(err, &validationError) {
		_ = a.appendJSONL(a.rejectsPath, map[string]any{
			"record_type": "reject",
			"observation": observation,
			"envelope":    cloned,
			"error":       err.Error(),
		})
		return &RelayResult{Status: "rejected", Observation: *observation}, nil
	}
	_ = a.appendJSONL(a.spoolPath, map[string]any{
		"record_type": "envelope",
		"observation": observation,
		"ingress_id":  ingressID,
		"session_id":  sessionID,
		"transport":   transport,
		"envelope":    cloned,
		"error":       err.Error(),
	})
	return &RelayResult{Status: "spooled", Spooled: true, Observation: *observation}, nil
}

func (a *Agent) FlushSpool(ctx context.Context) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	file, err := os.Open(a.spoolPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var remaining []map[string]any
	flushed := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			_ = appendJSONLUnlocked(a.rejectsPath, map[string]any{
				"record_type": "reject",
				"raw_line":    string(line),
				"error":       err.Error(),
			})
			continue
		}
		if record["record_type"] != "envelope" {
			continue
		}
		rawEnvelope, _ := json.Marshal(record["envelope"])
		var envelope contracts.Envelope
		if err := json.Unmarshal(rawEnvelope, &envelope); err != nil {
			_ = appendJSONLUnlocked(a.rejectsPath, map[string]any{
				"record_type": "reject",
				"record":      record,
				"error":       err.Error(),
			})
			continue
		}
		ingressID, _ := record["ingress_id"].(string)
		_, err := a.router.Ingest(ctx, &envelope, ingressID)
		if err == nil {
			flushed++
			continue
		}
		var validationError *contracts.ValidationError
		if errors.As(err, &validationError) {
			record["error"] = err.Error()
			_ = appendJSONLUnlocked(a.rejectsPath, record)
			continue
		}
		record["error"] = err.Error()
		remaining = append(remaining, record)
	}
	if err := scanner.Err(); err != nil {
		return flushed, err
	}
	if len(remaining) == 0 {
		_ = os.Remove(a.spoolPath)
		return flushed, nil
	}
	var builder strings.Builder
	for _, record := range remaining {
		builder.Write(mustJSON(record))
		builder.WriteByte('\n')
	}
	return flushed, atomicWriteFile(a.spoolPath, []byte(builder.String()), 0o644)
}

func (a *Agent) Diagnostics() (map[string]any, error) {
	return map[string]any{
		"spool_records":  countLines(a.spoolPath),
		"spool_path":     a.spoolPath,
		"reject_records": countLines(a.rejectsPath),
		"rejects_path":   a.rejectsPath,
		"last_heartbeat": readJSONFile(a.heartbeatPath),
	}, nil
}

func EncodeEnvelopeFrame(envelope *contracts.Envelope) ([]byte, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return usbcdc.EncodeFrame(FrameEnvelopeJSON, payload)
}

func EncodeHeartbeatFrame(payload map[string]any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return usbcdc.EncodeFrame(FrameHeartbeatJSON, raw)
}

func newObservation(ingressID, sessionID, transport string, frameType *byte, metadata map[string]any) IngressObservation {
	observation := IngressObservation{
		IngressID:  ingressID,
		SessionID:  sessionID,
		Transport:  transport,
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		FrameType:  frameType,
	}
	if value, ok := metadata["rssi"].(int); ok {
		observation.RSSI = &value
	}
	if value, ok := metadata["snr"].(float64); ok {
		observation.SNR = &value
	}
	if value, ok := metadata["hop_count"].(int); ok {
		observation.HopCount = &value
	}
	return observation
}

func countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count
}

func readJSONFile(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	return value
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func stringsTrimSuffix(value, suffix string) string {
	if suffix == "" || !strings.HasSuffix(value, suffix) {
		return value
	}
	return value[:len(value)-len(suffix)]
}

func cloneEnvelope(envelope *contracts.Envelope) (*contracts.Envelope, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	var cloned contracts.Envelope
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (a *Agent) appendJSONL(path string, record map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return appendJSONLUnlocked(path, record)
}

func appendJSONLUnlocked(path string, record map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(mustJSON(record), '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	writeErr := func() error {
		if _, err := file.Write(data); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return file.Close()
	}()
	if writeErr != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return writeErr
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func decodeCompactSummaryEnvelope(frameType byte, payload []byte) (*contracts.Envelope, string, error) {
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return nil, "", errors.New("empty compact payload")
	}
	parts := strings.Split(text, "|")
	frameSpec, err := loadCompactFrameSpec(frameType)
	if err != nil {
		return nil, "", err
	}
	switch parts[0] {
	case "S":
		if len(parts) < 5 || parts[1] == "" || parts[2] == "" {
			return nil, "", errors.New("invalid compact state payload")
		}
		shape, err := loadCompactShape(parts[0], frameType)
		if err != nil {
			return nil, "", err
		}
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", time.Now().UTC().UnixNano()),
			Kind:          "state",
			Priority:      "normal",
			OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
			Source:        contracts.SourceRef{HardwareID: parts[1]},
			Target:        contracts.TargetRef{Kind: "service", Value: "state"},
			Payload: map[string]any{
				"state_key":    parts[2],
				"value":        parts[3],
				"event_wake":   parts[4] == "1",
				"shape":        shape,
				"wire_shape":   frameSpec.WireShape,
				"codec_family": frameSpec.Name,
			},
		}, "", nil
	case "E":
		if len(parts) < 4 || parts[1] == "" || parts[2] == "" {
			return nil, "", errors.New("invalid compact event payload")
		}
		shape, err := loadCompactShape(parts[0], frameType)
		if err != nil {
			return nil, "", err
		}
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", time.Now().UTC().UnixNano()),
			Kind:          "event",
			Priority:      "critical",
			EventID:       parts[2],
			OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
			Source:        contracts.SourceRef{HardwareID: parts[1]},
			Target:        contracts.TargetRef{Kind: "service", Value: "events"},
			Payload: map[string]any{
				"value":        parts[3],
				"shape":        shape,
				"wire_shape":   frameSpec.WireShape,
				"codec_family": frameSpec.Name,
			},
		}, "", nil
	case "R":
		if len(parts) < 5 || parts[1] == "" || parts[2] == "" {
			return nil, "", errors.New("invalid compact command_result payload")
		}
		shape, err := loadCompactShape(parts[0], frameType)
		if err != nil {
			return nil, "", err
		}
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", time.Now().UTC().UnixNano()),
			Kind:          "command_result",
			Priority:      "control",
			CommandID:     parts[2],
			OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
			Source:        contracts.SourceRef{HardwareID: parts[1]},
			Target:        contracts.TargetRef{Kind: "client", Value: "sleepy-node-sdk"},
			Payload: map[string]any{
				"command_id":   parts[2],
				"phase":        parts[3],
				"result":       parts[4],
				"shape":        shape,
				"wire_shape":   frameSpec.WireShape,
				"codec_family": frameSpec.Name,
			},
		}, "", nil
	case "D":
		return nil, "digest_recorded", nil
	case "P":
		return nil, "poll_recorded", nil
	default:
		return nil, "", errors.New("unsupported compact payload kind")
	}
}

func loadCompactFrameSpec(frameType byte) (*compactcodec.FrameTypeSpec, error) {
	registry, err := compactcodec.LoadRegistry(compactcodec.DefaultRegistryPath())
	if err != nil {
		return nil, err
	}
	return registry.FrameTypeSpec(frameType)
}

func loadCompactShape(logicalKey string, frameType byte) (string, error) {
	registry, err := compactcodec.LoadRegistry(compactcodec.DefaultRegistryPath())
	if err != nil {
		return "", err
	}
	return registry.ShapeFor(logicalKey, frameType)
}
