package hostagent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Aero123421/edge-fabric/internal/protocol/onair"
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

const onairDuplicateWindow = 5 * time.Second

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

type RuntimeResolver interface {
	ResolveHardwareIDByShortID(context.Context, uint16) (string, error)
	ResolveCommandIDByToken(context.Context, uint16) (string, error)
	ResolveCommandIDByTokenForTarget(context.Context, string, uint16) (string, error)
}

type Agent struct {
	router        Ingester
	spoolPath     string
	rejectsPath   string
	heartbeatPath string
	onairSeen     map[string]time.Time
	mu            sync.Mutex
}

func New(router Ingester, spoolPath string) *Agent {
	return &Agent{
		router:        router,
		spoolPath:     spoolPath,
		rejectsPath:   stringsTrimSuffix(spoolPath, filepath.Ext(spoolPath)) + ".rejected.jsonl",
		heartbeatPath: stringsTrimSuffix(spoolPath, filepath.Ext(spoolPath)) + ".heartbeat.json",
		onairSeen:     map[string]time.Time{},
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
		envelope := heartbeatEnvelope(heartbeat)
		return a.relayEnvelope(ctx, ingressID, sessionID, "usb_cdc", &observation, envelope)
	}
	if frameType == FrameCompactBinary || frameType == FrameSummaryBinary {
		dedupeKey := onairDuplicateKey(frameType, payload)
		envelope, status, err := decodeCompactSummaryEnvelope(ctx, runtimeResolver(a.router), frameType, payload)
		if err != nil {
			return nil, err
		}
		if envelope == nil {
			return &RelayResult{Status: status, Observation: observation}, nil
		}
		if a.seenRecentOnAirFrame(dedupeKey, time.Now().UTC()) {
			return &RelayResult{Status: "duplicate_onair", Observation: observation}, nil
		}
		result, err := a.relayEnvelope(ctx, ingressID, sessionID, "usb_cdc", &observation, envelope)
		if err == nil && result != nil && (result.Status == "persisted" || result.Status == "spooled") {
			a.rememberOnAirFrame(dedupeKey, time.Now().UTC())
		}
		return result, err
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
	cloned.Delivery.IngressMeta["host_link"] = transport
	cloned.Delivery.IngressMeta["transport"] = transport
	cloned.Delivery.IngressMeta["session_id"] = sessionID
	cloned.Delivery.IngressMeta["received_at"] = observation.ReceivedAt
	cloned.Delivery.IngressMeta["ingress_id"] = ingressID
	if bearer, ok := observationBearer(observation); ok {
		cloned.Delivery.IngressMeta["bearer"] = bearer
	}
	if observation.RSSI != nil {
		cloned.Delivery.IngressMeta["rssi"] = *observation.RSSI
	}
	if observation.SNR != nil {
		cloned.Delivery.IngressMeta["snr"] = *observation.SNR
	}
	if observation.HopCount != nil {
		cloned.Delivery.IngressMeta["hop_count"] = *observation.HopCount
	}
	if cloned.MeshMeta != nil && cloned.MeshMeta.IngressGatewayID == "" {
		cloned.MeshMeta.IngressGatewayID = ingressID
	}

	ack, err := a.router.Ingest(ctx, cloned, ingressID)
	if err == nil {
		return &RelayResult{Status: ack.Status, Ack: ack, Observation: *observation}, nil
	}
	var validationError *contracts.ValidationError
	if errors.As(err, &validationError) {
		if appendErr := a.appendJSONL(a.rejectsPath, map[string]any{
			"record_type": "reject",
			"observation": observation,
			"envelope":    cloned,
			"error":       err.Error(),
		}); appendErr != nil {
			return &RelayResult{Status: "reject_spool_failed", Observation: *observation}, appendErr
		}
		return &RelayResult{Status: "rejected", Observation: *observation}, nil
	}
	if appendErr := a.appendJSONL(a.spoolPath, map[string]any{
		"record_type": "envelope",
		"observation": observation,
		"ingress_id":  ingressID,
		"session_id":  sessionID,
		"transport":   transport,
		"envelope":    cloned,
		"error":       err.Error(),
	}); appendErr != nil {
		return &RelayResult{Status: "spool_failed", Observation: *observation}, fmt.Errorf("router ingest failed: %w; spool failed: %v", err, appendErr)
	}
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
			if appendErr := appendJSONLUnlocked(a.rejectsPath, map[string]any{
				"record_type": "reject",
				"raw_line":    string(line),
				"error":       err.Error(),
			}); appendErr != nil {
				return flushed, appendErr
			}
			continue
		}
		if record["record_type"] != "envelope" {
			continue
		}
		rawEnvelope, _ := json.Marshal(record["envelope"])
		var envelope contracts.Envelope
		if err := json.Unmarshal(rawEnvelope, &envelope); err != nil {
			if appendErr := appendJSONLUnlocked(a.rejectsPath, map[string]any{
				"record_type": "reject",
				"record":      record,
				"error":       err.Error(),
			}); appendErr != nil {
				return flushed, appendErr
			}
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
			if appendErr := appendJSONLUnlocked(a.rejectsPath, record); appendErr != nil {
				return flushed, appendErr
			}
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

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
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

func onairDuplicateKey(frameType byte, payload []byte) string {
	sum := sha256.Sum256(append([]byte{frameType}, payload...))
	return fmt.Sprintf("%x", sum[:])
}

func stableOnAirKey(packet *onair.Packet) string {
	if packet == nil {
		return ""
	}
	sourceShortID := packet.SourceShortID
	if packet.Relay != nil && packet.Relay.OriginShortID != 0 {
		sourceShortID = packet.Relay.OriginShortID
	}
	sum := sha256.Sum256(packet.Body)
	return fmt.Sprintf(
		"onair-v1:%d:%d:%d:%d:%x",
		sourceShortID,
		packet.TargetShortID,
		packet.Sequence,
		packet.LogicalType,
		sum[:8],
	)
}

func stableOnAirEventID(packet *onair.Packet, receivedAt time.Time) string {
	// The packet key is an 8-bit-sequence radio dedupe hint, so bucket it for
	// durable event identity instead of treating it as globally unique forever.
	return fmt.Sprintf("evt-%s:rx5m:%d", stableOnAirKey(packet), receivedAt.UTC().Unix()/300)
}

func onairMeshMeta(packet *onair.Packet) *contracts.MeshMeta {
	key := stableOnAirKey(packet)
	if key == "" {
		return nil
	}
	zeroHop := 0
	meta := &contracts.MeshMeta{OnAirKey: key, HopCount: &zeroHop}
	if packet.Relay != nil {
		origin := int(packet.Relay.OriginShortID)
		previous := int(packet.Relay.PreviousHopShortID)
		ttl := int(packet.Relay.TTL)
		hopCount := int(packet.Relay.HopCount)
		routeHint := int(packet.Relay.RouteHint)
		meta.OriginShortID = &origin
		meta.PreviousHopShortID = &previous
		meta.TTL = &ttl
		meta.HopCount = &hopCount
		meta.RouteHint = &routeHint
	}
	return meta
}

func (a *Agent) seenRecentOnAirFrame(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := now.Add(-onairDuplicateWindow)
	for existingKey, seenAt := range a.onairSeen {
		if seenAt.Before(cutoff) {
			delete(a.onairSeen, existingKey)
		}
	}
	seenAt, ok := a.onairSeen[key]
	return ok && !seenAt.Before(cutoff)
}

func (a *Agent) rememberOnAirFrame(key string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onairSeen[key] = now
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

func decodeCompactSummaryEnvelope(ctx context.Context, resolver RuntimeResolver, frameType byte, payload []byte) (*contracts.Envelope, string, error) {
	packet, err := onair.Decode(payload)
	if err != nil {
		return nil, "", err
	}
	if frameType == FrameCompactBinary && packet.Summary() {
		return nil, "", errors.New("compact USB frame carried summary on-air packet")
	}
	if frameType == FrameSummaryBinary && !packet.Summary() {
		return nil, "", errors.New("summary USB frame carried compact on-air packet")
	}
	receivedAt := time.Now().UTC()
	occurredAt := receivedAt.Format(time.RFC3339Nano)
	onairKey := stableOnAirKey(packet)
	shortID := packet.SourceShortID
	if packet.Relay != nil && packet.Relay.OriginShortID != 0 {
		shortID = packet.Relay.OriginShortID
	}
	hardwareID := fmt.Sprintf("short:%d", shortID)
	if resolver != nil && shortID != 0 {
		resolved, err := resolver.ResolveHardwareIDByShortID(ctx, shortID)
		if err != nil {
			return nil, "", err
		}
		if resolved != "" {
			hardwareID = resolved
		}
	}
	meshMeta := onairMeshMeta(packet)
	annotateRelayPayload := func(payload map[string]any) map[string]any {
		if packet.Relay == nil {
			return payload
		}
		payload["relay_extension"] = true
		payload["origin_short_id"] = int(packet.Relay.OriginShortID)
		payload["previous_hop_short_id"] = int(packet.Relay.PreviousHopShortID)
		payload["ttl"] = int(packet.Relay.TTL)
		payload["hop_count"] = int(packet.Relay.HopCount)
		payload["route_hint"] = int(packet.Relay.RouteHint)
		return payload
	}
	switch packet.LogicalType {
	case onair.TypeState:
		body, err := onair.DecodeState(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "state")
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", receivedAt.UnixNano()),
			Kind:          "state",
			Priority:      "normal",
			OccurredAt:    occurredAt,
			Source:        contracts.SourceRef{HardwareID: hardwareID, FabricShortID: shortIDPtr(shortID)},
			Target:        contracts.TargetRef{Kind: "service", Value: "state"},
			MeshMeta:      meshMeta,
			Payload: annotateRelayPayload(map[string]any{
				"state_key":       stateKeyFromToken(body.KeyToken),
				"value":           stateValueFromToken(body.ValueToken),
				"event_wake":      body.EventWake,
				"shape":           shape,
				"wire_shape":      wireShape,
				"codec_family":    codecFamily,
				"source_short_id": shortID,
				"target_short_id": packet.TargetShortID,
				"onair_sequence":  packet.Sequence,
				"onair_key":       onairKey,
			}),
		}, "", nil
	case onair.TypeEvent:
		body, err := onair.DecodeEvent(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "event")
		priority := "normal"
		if body.Severity == onair.EventSeverityCritical {
			priority = "critical"
		}
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", receivedAt.UnixNano()),
			Kind:          "event",
			Priority:      priority,
			EventID:       stableOnAirEventID(packet, receivedAt),
			OccurredAt:    occurredAt,
			Source:        contracts.SourceRef{HardwareID: hardwareID, FabricShortID: shortIDPtr(shortID)},
			Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
			MeshMeta:      meshMeta,
			Payload: annotateRelayPayload(map[string]any{
				"event_code":      int(body.EventCode),
				"event_name":      eventNameFromToken(body.EventCode),
				"severity":        eventSeverityFromToken(body.Severity),
				"value_bucket":    int(body.ValueBucket),
				"flags":           int(body.Flags),
				"event_wake":      body.Flags&onair.EventFlagEventWake != 0,
				"latched":         body.Flags&onair.EventFlagLatched != 0,
				"shape":           shape,
				"wire_shape":      wireShape,
				"codec_family":    codecFamily,
				"source_short_id": shortID,
				"target_short_id": packet.TargetShortID,
				"onair_sequence":  packet.Sequence,
				"onair_key":       onairKey,
			}),
		}, "", nil
	case onair.TypeCommandResult:
		body, err := onair.DecodeCommandResult(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "command_result")
		commandID := fmt.Sprintf("token:%d", body.CommandToken)
		if resolver != nil {
			resolved, err := resolver.ResolveCommandIDByTokenForTarget(ctx, hardwareID, body.CommandToken)
			if err != nil {
				return nil, "", err
			}
			if resolved != "" {
				commandID = resolved
			}
		}
		phase, err := commandPhaseFromToken(body.PhaseToken)
		if err != nil {
			return nil, "", err
		}
		reason, err := commandReasonFromToken(body.ReasonToken)
		if err != nil {
			return nil, "", err
		}
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-compact-%d", receivedAt.UnixNano()),
			Kind:          "command_result",
			Priority:      "control",
			CommandID:     commandID,
			OccurredAt:    occurredAt,
			Source:        contracts.SourceRef{HardwareID: hardwareID, FabricShortID: shortIDPtr(shortID)},
			Target:        contracts.TargetRef{Kind: "client", Value: "sleepy-node-sdk"},
			MeshMeta:      meshMeta,
			Payload: annotateRelayPayload(map[string]any{
				"command_id":      commandID,
				"command_token":   int(body.CommandToken),
				"phase":           phase,
				"result":          reason,
				"shape":           shape,
				"wire_shape":      wireShape,
				"codec_family":    codecFamily,
				"source_short_id": shortID,
				"target_short_id": packet.TargetShortID,
				"onair_sequence":  packet.Sequence,
				"onair_key":       onairKey,
			}),
		}, "", nil
	case onair.TypePendingDigest:
		body, err := onair.DecodePendingDigest(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "pending_digest")
		return controlHeartbeatEnvelope(hardwareID, shortID, packet, "pending_digest", map[string]any{
			"pending_count": int(body.PendingCount),
			"flags":         int(body.Flags),
			"urgent":        body.Flags&onair.PendingFlagUrgent != 0,
			"expires_soon":  body.Flags&onair.PendingFlagExpiresSoon != 0,
			"shape":         shape,
			"wire_shape":    wireShape,
			"codec_family":  codecFamily,
		}), "", nil
	case onair.TypeTinyPoll:
		body, err := onair.DecodeTinyPoll(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "tiny_poll")
		return controlHeartbeatEnvelope(hardwareID, shortID, packet, "tiny_poll", map[string]any{
			"service_level": int(body.ServiceLevel),
			"service_name":  serviceLevelFromToken(body.ServiceLevel),
			"shape":         shape,
			"wire_shape":    wireShape,
			"codec_family":  codecFamily,
		}), "", nil
	case onair.TypeHeartbeat:
		body, err := onair.DecodeHeartbeat(packet)
		if err != nil {
			return nil, "", err
		}
		shape, wireShape, codecFamily := onairShape(packet, "heartbeat")
		return &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     fmt.Sprintf("msg-heartbeat-%d", receivedAt.UnixNano()),
			Kind:          "heartbeat",
			Priority:      "normal",
			OccurredAt:    occurredAt,
			Source:        contracts.SourceRef{HardwareID: hardwareID, FabricShortID: shortIDPtr(shortID)},
			Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
			MeshMeta:      meshMeta,
			Payload: annotateRelayPayload(map[string]any{
				"subject_kind":      "node",
				"subject_id":        hardwareID,
				"live":              true,
				"status":            "onair_heartbeat",
				"health":            heartbeatHealthFromToken(body.Health),
				"health_code":       int(body.Health),
				"battery_bucket":    int(body.BatteryBucket),
				"link_quality":      int(body.LinkQuality),
				"uptime_bucket":     int(body.UptimeBucket),
				"flags":             int(body.Flags),
				"event_wake":        body.Flags&onair.HeartbeatFlagEventWake != 0,
				"maintenance_awake": body.Flags&onair.HeartbeatFlagMaintenanceAwake != 0,
				"low_power":         body.Flags&onair.HeartbeatFlagLowPower != 0,
				"shape":             shape,
				"wire_shape":        wireShape,
				"codec_family":      codecFamily,
				"source_short_id":   shortID,
				"target_short_id":   packet.TargetShortID,
				"onair_sequence":    packet.Sequence,
				"onair_key":         onairKey,
			}),
		}, "", nil
	default:
		return nil, "", errors.New("unsupported on-air logical type")
	}
}

func onairShape(packet *onair.Packet, logicalName string) (string, string, string) {
	if packet.Summary() {
		return logicalName + "_summary_v1", "summary_v1", "summary_binary_v1"
	}
	return logicalName + "_compact_v1", "compact_v1", "compact_binary_v1"
}

func controlHeartbeatEnvelope(hardwareID string, shortID uint16, packet *onair.Packet, status string, payload map[string]any) *contracts.Envelope {
	payload = cloneMap(payload)
	payload["subject_kind"] = "node"
	payload["subject_id"] = hardwareID
	payload["live"] = true
	payload["status"] = status
	payload["source_short_id"] = shortID
	payload["target_short_id"] = packet.TargetShortID
	payload["onair_sequence"] = packet.Sequence
	payload["onair_key"] = stableOnAirKey(packet)
	return &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     fmt.Sprintf("msg-heartbeat-%d-%d-%d", packet.LogicalType, packet.Sequence, time.Now().UTC().UnixNano()),
		Kind:          "heartbeat",
		Priority:      "normal",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: hardwareID, FabricShortID: shortIDPtr(shortID)},
		Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
		MeshMeta:      onairMeshMeta(packet),
		Payload:       payload,
	}
}

func heartbeatEnvelope(payload map[string]any) *contracts.Envelope {
	gatewayID, _ := payload["gateway_id"].(string)
	if gatewayID == "" {
		gatewayID = "gateway-unknown"
	}
	return &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     fmt.Sprintf("msg-heartbeat-%d", time.Now().UTC().UnixNano()),
		Kind:          "heartbeat",
		Priority:      "normal",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: gatewayID},
		Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
		Payload:       cloneMap(payload),
	}
}

func runtimeResolver(router Ingester) RuntimeResolver {
	resolver, ok := router.(RuntimeResolver)
	if !ok {
		return nil
	}
	return resolver
}

func observationBearer(observation *IngressObservation) (string, bool) {
	if observation == nil || observation.FrameType == nil {
		return "", false
	}
	if *observation.FrameType == FrameCompactBinary || *observation.FrameType == FrameSummaryBinary {
		return "lora_direct", true
	}
	return "", false
}

func shortIDPtr(value uint16) *int {
	if value == 0 {
		return nil
	}
	converted := int(value)
	return &converted
}

func stateKeyFromToken(token byte) string {
	switch token {
	case onair.StateKeyNodePower:
		return "node.power"
	default:
		return "state.unknown"
	}
}

func stateValueFromToken(token byte) string {
	switch token {
	case onair.StateValueAwake:
		return "awake"
	case onair.StateValueSleep:
		return "sleep"
	default:
		return "unknown"
	}
}

func eventNameFromToken(token byte) string {
	switch token {
	case onair.EventCodeBatteryLow:
		return "battery_low"
	case onair.EventCodeMotionDetected:
		return "motion_detected"
	case onair.EventCodeLeakDetected:
		return "leak_detected"
	case onair.EventCodeTamper:
		return "tamper"
	case onair.EventCodeThresholdCrossed:
		return "threshold_crossed"
	default:
		return "event.unknown"
	}
}

func eventSeverityFromToken(token byte) string {
	switch token {
	case onair.EventSeverityInfo:
		return "info"
	case onair.EventSeverityWarning:
		return "warning"
	case onair.EventSeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

func serviceLevelFromToken(token byte) string {
	switch token {
	case onair.ServiceLevelEventualNextPoll:
		return "eventual_next_poll"
	default:
		return "unknown"
	}
}

func heartbeatHealthFromToken(token byte) string {
	switch token {
	case onair.HeartbeatHealthOK:
		return "ok"
	case onair.HeartbeatHealthDegraded:
		return "degraded"
	case onair.HeartbeatHealthCritical:
		return "critical"
	default:
		return "unknown"
	}
}

func commandPhaseFromToken(token byte) (string, error) {
	switch token {
	case onair.PhaseAccepted:
		return "accepted", nil
	case onair.PhaseExecuting:
		return "executing", nil
	case onair.PhaseSucceeded:
		return "succeeded", nil
	case onair.PhaseFailed:
		return "failed", nil
	case onair.PhaseRejected:
		return "rejected", nil
	case onair.PhaseExpired:
		return "expired", nil
	default:
		return "", fmt.Errorf("unknown command_result phase token: %d", token)
	}
}

func commandReasonFromToken(token byte) (string, error) {
	switch token {
	case onair.ReasonOK:
		return "ok", nil
	case onair.ReasonService:
		return "svc", nil
	case onair.ReasonMaintenance:
		return "maintenance", nil
	case onair.ReasonStale:
		return "stale", nil
	case onair.ReasonBadCommand:
		return "badcmd", nil
	case onair.ReasonUnsupported:
		return "unsupported", nil
	default:
		return "", fmt.Errorf("unknown command_result reason token: %d", token)
	}
}
