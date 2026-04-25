package siterouter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func openTestRouter(t *testing.T) *Router {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site-router.db")
	router, err := Open(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
	})
	return router
}

type routePolicyArtifact struct {
	RouteClasses map[string]routePolicySpec `json:"route_classes"`
}

type routePolicySpec struct {
	AllowedBearers     []string `json:"allowed_bearers"`
	RequiresTargetRole []string `json:"requires_target_role"`
	MaxLoRaBodyBytes   *int     `json:"max_lora_body_bytes"`
	AllowRelay         bool     `json:"allow_relay"`
	AllowRedundant     bool     `json:"allow_redundant"`
	HopLimit           *int     `json:"hop_limit"`
}

type rolePolicyArtifact struct {
	Roles map[string]rolePolicySpec `json:"roles"`
}

type rolePolicySpec struct {
	RequiresAlwaysOn bool `json:"requires_always_on"`
}

func loadRoutePolicyArtifact(t *testing.T) routePolicyArtifact {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "policy", "route-classes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact routePolicyArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func loadRolePolicyArtifact(t *testing.T) rolePolicyArtifact {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "policy", "role-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact rolePolicyArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func upsertPolicyNode(t *testing.T, router *Router, hardwareID, role, primaryBearer string, shortID *int) {
	t.Helper()
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, hardwareID, &contracts.Manifest{
		HardwareID:          hardwareID,
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          policyPowerClass(role),
		WakeClass:           policyWakeClass(role),
		SupportedBearers:    []string{policyManifestBearerName(primaryBearer)},
		AllowedNetworkRoles: []string{role},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, hardwareID, &contracts.Lease{
		RoleLeaseID:      "lease-" + hardwareID,
		SiteID:           "site-a",
		LogicalBindingID: "binding-" + hardwareID,
		FabricShortID:    shortID,
		EffectiveRole:    role,
		PrimaryBearer:    primaryBearer,
	}); err != nil {
		t.Fatal(err)
	}
}

func policyPowerClass(role string) string {
	if role == "sleepy_leaf" {
		return "primary_battery"
	}
	return "mains_powered"
}

func policyWakeClass(role string) string {
	if role == "sleepy_leaf" {
		return "sleepy_event"
	}
	return "always_on"
}

func policyRoleForRoute(policy routePolicySpec) string {
	if policyContainsString(policy.RequiresTargetRole, "sleepy_leaf") {
		return "sleepy_leaf"
	}
	if len(policy.RequiresTargetRole) > 0 {
		return policy.RequiresTargetRole[0]
	}
	return "powered_leaf"
}

func primaryBearerForPolicyLabel(label string) string {
	switch label {
	case "lora_direct":
		return "lora"
	default:
		return label
	}
}

func routePolicyEnvelope(routeClass, target string, payloadBytes int) *contracts.Envelope {
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-policy-" + routeClass + "-" + target,
		Kind:          "fabric_summary",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "controller-policy"},
		Target:        contracts.TargetRef{Kind: "node", Value: target},
		Delivery:      &contracts.DeliverySpec{RouteClass: routeClass},
		Payload:       map[string]any{"payload_bytes": payloadBytes, "allow_declared_lora_size_for_alpha": true},
	}
	if routeClass == "sleepy_tiny_control" {
		envelope.Kind = "command"
		envelope.Priority = "control"
		envelope.CommandID = "cmd-policy-" + target
		envelope.Payload = map[string]any{
			"command_name":  "threshold.set",
			"command_token": 0x1201,
			"value":         42,
		}
	}
	return envelope
}

func routePolicyAllowsBearer(policy routePolicySpec, bearer string) bool {
	return policyContainsString(policy.AllowedBearers, bearer)
}

func policyContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func policyManifestBearerName(bearer string) string {
	switch bearer {
	case "lora_direct", "lora_relay", "lora_mesh":
		return "lora"
	case "wifi_ip", "wifi_mesh", "wifi_lr":
		return "wifi"
	default:
		return bearer
	}
}

func TestDuplicateEventDedupe(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-001",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-001",
		Source:        contracts.SourceRef{HardwareID: "sensor-01"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "water"},
	}
	second := *first
	second.MessageID = "msg-002"
	ack1, err := router.Ingest(ctx, first, "gateway-a")
	if err != nil {
		t.Fatal(err)
	}
	ack2, err := router.Ingest(ctx, &second, "gateway-b")
	if err != nil {
		t.Fatal(err)
	}
	if ack1.Duplicate {
		t.Fatal("first ingest must not be duplicate")
	}
	if !ack2.Duplicate {
		t.Fatal("second ingest must be duplicate")
	}
	count, err := router.CountEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestOnAirPacketKeyDedupesAcrossGatewayEventIDs(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	packetKey := "onair-v1:201:0:7:2:abcdef01"
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-onair-gw-a",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-onair-gw-a",
		Source:        contracts.SourceRef{HardwareID: "sleepy-onair-01"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		MeshMeta:      &contracts.MeshMeta{OnAirKey: packetKey},
		Payload:       map[string]any{"event_code": 2, "severity": 3},
	}
	second := *first
	second.MessageID = "msg-onair-gw-b"
	second.EventID = "evt-onair-gw-b"
	ack1, err := router.Ingest(ctx, first, "gateway-a")
	if err != nil {
		t.Fatal(err)
	}
	ack2, err := router.Ingest(ctx, &second, "gateway-b")
	if err != nil {
		t.Fatal(err)
	}
	if ack1.Duplicate {
		t.Fatal("first on-air ingest must not be duplicate")
	}
	if !ack2.Duplicate {
		t.Fatal("second on-air ingest must be duplicate by packet key")
	}
	if ack2.AckedMessageID != first.MessageID {
		t.Fatalf("expected duplicate ack for %s, got %s", first.MessageID, ack2.AckedMessageID)
	}
	var lastMessageID string
	if err := router.db.QueryRowContext(ctx, `SELECT last_message_id FROM radio_packet_observation WHERE packet_key = ?`, packetKey).Scan(&lastMessageID); err != nil {
		t.Fatal(err)
	}
	if lastMessageID != first.MessageID {
		t.Fatalf("duplicate observation must not point last_message_id at non-persisted duplicate, got %s", lastMessageID)
	}
	count, err := router.CountEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestOnAirPacketKeyDedupeExpires(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	packetKey := "onair-v1:201:0:21:2:abcdef01"
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-onair-expire-a",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-onair-expire-a",
		Source:        contracts.SourceRef{HardwareID: "sleepy-onair-ttl"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		MeshMeta:      &contracts.MeshMeta{OnAirKey: packetKey},
		Payload:       map[string]any{"event_code": 2, "severity": 3},
	}
	if _, err := router.Ingest(ctx, first, "gateway-a"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-2 * radioPacketObservationWindow).Format(time.RFC3339Nano)
	if _, err := router.db.ExecContext(ctx, `UPDATE radio_packet_observation SET last_seen_at = ? WHERE packet_key = ?`, old, packetKey); err != nil {
		t.Fatal(err)
	}
	second := *first
	second.MessageID = "msg-onair-expire-b"
	second.EventID = "evt-onair-expire-b"
	ack, err := router.Ingest(ctx, &second, "gateway-b")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Duplicate {
		t.Fatal("stale on-air packet key must not dedupe a new real event")
	}
	count, err := router.CountEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 events after dedupe window expiry, got %d", count)
	}
}

func TestRelayPacketDedupeNormalizesRelayHopFields(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	originPacketKey := "onair-v1:201:1:31:2:abcdef01"
	firstHopCount := 1
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-relay-dedupe-a",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-relay-dedupe-a",
		Source:        contracts.SourceRef{HardwareID: "sleepy-relay-origin", FabricShortID: intPtr(201)},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		MeshMeta: &contracts.MeshMeta{
			OnAirKey:   originPacketKey + ":prev=301:ttl=2",
			HopCount:   &firstHopCount,
			LastHop:    "relay-a",
			RelayTrace: []string{"relay-a"},
		},
		Payload: map[string]any{
			"origin_onair_key": originPacketKey,
			"event_code":       2,
			"severity":         3,
		},
	}
	second := *first
	second.MessageID = "msg-relay-dedupe-b"
	second.EventID = "evt-relay-dedupe-b"
	secondHopCount := 2
	second.MeshMeta = &contracts.MeshMeta{
		OnAirKey:   originPacketKey + ":prev=302:ttl=1",
		HopCount:   &secondHopCount,
		LastHop:    "relay-b",
		RelayTrace: []string{"relay-a", "relay-b"},
	}
	ack1, err := router.Ingest(ctx, first, "gateway-relay-a")
	if err != nil {
		t.Fatal(err)
	}
	ack2, err := router.Ingest(ctx, &second, "gateway-relay-b")
	if err != nil {
		t.Fatal(err)
	}
	if ack1.Duplicate {
		t.Fatal("first relayed packet observation must not be duplicate")
	}
	if !ack2.Duplicate || ack2.AckedMessageID != first.MessageID {
		t.Fatalf("relay hop metadata must not create a new durable event, got ack %+v", ack2)
	}
	count, err := router.CountEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected relay duplicate to persist one event, got %d", count)
	}
}

func TestQueueRecovery(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-queue-001",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-queue-001",
		Source:        contracts.SourceRef{HardwareID: "sensor-02"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "battery_low"},
	}
	queueID, err := router.EnqueueOutbound(ctx, envelope, "")
	if err != nil {
		t.Fatal(err)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-a", 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].QueueID != queueID {
		t.Fatal("failed to lease expected item")
	}
	if err := router.MarkSending(ctx, queueID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	recovered, err := router.RecoverExpiredLeases(ctx, time.Now().UTC().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered item, got %d", recovered)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_count"] != 1 {
		t.Fatalf("expected queued_count=1, got %d", metrics["queued_count"])
	}
}

func TestStateProjectionOrdering(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	newer := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-state-new",
		Kind:          "state",
		Priority:      "normal",
		OccurredAt:    "2026-04-23T08:05:00+00:00",
		Source: contracts.SourceRef{
			HardwareID: "tank-02",
			SessionID:  "sess-a",
			SeqLocal:   intPtr(5),
		},
		Target:  contracts.TargetRef{Kind: "service", Value: "state"},
		Payload: map[string]any{"state_key": "tank.level", "value": 80},
	}
	older := *newer
	older.MessageID = "msg-state-old"
	older.OccurredAt = "2026-04-23T08:01:00+00:00"
	older.Source.SeqLocal = intPtr(1)
	older.Payload = map[string]any{"state_key": "tank.level", "value": 25}
	if _, err := router.Ingest(ctx, newer, "wifi-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Ingest(ctx, &older, "wifi-b"); err != nil {
		t.Fatal(err)
	}
	payload, err := router.LatestState(ctx, "tank-02", "tank.level")
	if err != nil {
		t.Fatal(err)
	}
	if payload["value"].(float64) != 80 {
		t.Fatalf("expected 80, got %v", payload["value"])
	}
}

func TestInvalidCommandResultPhaseRejected(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-init",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-bad-01",
		Source:        contracts.SourceRef{HardwareID: "controller-01"},
		Target:        contracts.TargetRef{Kind: "node", Value: "servo-01"},
		Payload:       map[string]any{"command_name": "servo.set_angle", "angle": 45},
	}
	if _, err := router.Ingest(ctx, command, "local"); err != nil {
		t.Fatal(err)
	}
	result := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-result-bad",
		Kind:          "command_result",
		Priority:      "control",
		CommandID:     "cmd-bad-01",
		Source:        contracts.SourceRef{HardwareID: "servo-01"},
		Target:        contracts.TargetRef{Kind: "client", Value: "controller-01"},
		Payload:       map[string]any{"phase": "bad_phase", "command_id": "cmd-bad-01"},
	}
	if _, err := router.Ingest(ctx, result, "local"); err == nil {
		t.Fatal("expected invalid phase error")
	}
}

func TestUnknownCommandResultRejected(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	result := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-result-unknown",
		Kind:          "command_result",
		Priority:      "control",
		CommandID:     "cmd-missing-01",
		Source:        contracts.SourceRef{HardwareID: "servo-02"},
		Target:        contracts.TargetRef{Kind: "client", Value: "controller-02"},
		Payload:       map[string]any{"phase": "accepted", "command_id": "cmd-missing-01"},
	}
	if _, err := router.Ingest(ctx, result, "local"); err == nil {
		t.Fatal("expected missing command_id error")
	}
}

func TestOccurredAtMustBeRFC3339(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-state-invalid-time",
		Kind:          "state",
		Priority:      "normal",
		OccurredAt:    "not-a-time",
		Source:        contracts.SourceRef{HardwareID: "tank-03"},
		Target:        contracts.TargetRef{Kind: "service", Value: "state"},
		Payload:       map[string]any{"state_key": "tank.level", "value": 12},
	}
	_, err := router.Ingest(ctx, envelope, "wifi-a")
	if err == nil {
		t.Fatal("expected time validation error")
	}
	var validationError *contracts.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestIssueCommandCreatesPendingDigest(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "sleepy-01", &contracts.Manifest{
		HardwareID:          "sleepy-01",
		DeviceFamily:        "xiao-esp32s3",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"wifi"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "sleepy-01", &contracts.Lease{
		RoleLeaseID:      "lease-pending-01",
		SiteID:           "site-a",
		LogicalBindingID: "binding-pending-01",
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "wifi",
	}); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-pending",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-pending-01",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-02"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-01"},
		Delivery:      &contracts.DeliverySpec{ExpiresAt: expiresAt},
		Payload:       map[string]any{"command_name": "threshold.set", "value": 42},
	}
	ack, queueID, err := router.IssueCommand(ctx, command, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "persisted" || queueID == 0 {
		t.Fatalf("unexpected issue result: ack=%+v queueID=%d", ack, queueID)
	}
	digest, err := router.PendingCommandDigest(ctx, "sleepy-01", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if digest.PendingCount != 1 || digest.NewestCommandID != "cmd-pending-01" {
		t.Fatalf("unexpected digest: %+v", digest)
	}
	if !digest.ExpiresSoon || !digest.Urgent {
		t.Fatalf("expected urgent expires-soon digest, got %+v", digest)
	}
	items, err := router.PendingCommandsForNode(ctx, "sleepy-01", 8, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CommandID != "cmd-pending-01" {
		t.Fatalf("unexpected pending items: %+v", items)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_count"] != 1 {
		t.Fatalf("expected queued_count=1, got %d", metrics["queued_count"])
	}
}

func TestCommandRequiresCommandID(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-no-id",
		Kind:          "command",
		Priority:      "control",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-03"},
		Target:        contracts.TargetRef{Kind: "node", Value: "servo-05"},
		Payload:       map[string]any{"command_name": "servo.set_angle", "angle": 30},
	}
	if _, err := router.Ingest(ctx, command, "local"); err == nil {
		t.Fatal("expected missing command_id error")
	}
}

func TestSentOKStateParticipatesInRecovery(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-queue-sent-ok",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-queue-sent-ok",
		Source:        contracts.SourceRef{HardwareID: "sensor-09"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "water"},
	}
	queueID, err := router.EnqueueOutbound(ctx, envelope, "")
	if err != nil {
		t.Fatal(err)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-z", 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].QueueID != queueID {
		t.Fatal("failed to lease expected item")
	}
	if err := router.MarkSending(ctx, queueID, "worker-z"); err != nil {
		t.Fatal(err)
	}
	if err := router.MarkSentOK(ctx, queueID, "worker-z"); err != nil {
		t.Fatal(err)
	}
	recovered, err := router.RecoverExpiredLeases(ctx, time.Now().UTC().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered sent_ok item, got %d", recovered)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_count"] != 1 {
		t.Fatalf("expected queued_count=1 after recovery, got %d", metrics["queued_count"])
	}
}

func TestPendingCommandsExcludeExpiredAndSentOK(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := router.UpsertManifest(ctx, "sleepy-02", &contracts.Manifest{
		HardwareID:          "sleepy-02",
		DeviceFamily:        "xiao-esp32s3",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"wifi"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "sleepy-02", &contracts.Lease{
		RoleLeaseID:      "lease-pending-02",
		SiteID:           "site-a",
		LogicalBindingID: "binding-pending-02",
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "wifi",
	}); err != nil {
		t.Fatal(err)
	}
	fresh := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-fresh",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-fresh-01",
		OccurredAt:    now.Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-04"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-02"},
		Delivery:      &contracts.DeliverySpec{ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano)},
		Payload:       map[string]any{"command_name": "threshold.set", "value": 11},
	}
	expired := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-expired",
		Kind:          "command",
		Priority:      "normal",
		CommandID:     "cmd-expired-01",
		OccurredAt:    now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-04"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-02"},
		Delivery:      &contracts.DeliverySpec{ExpiresAt: now.Add(-1 * time.Minute).Format(time.RFC3339Nano)},
		Payload:       map[string]any{"command_name": "mode.set", "mode": "eco"},
	}
	if _, _, err := router.IssueCommand(ctx, fresh, "local", ""); err != nil {
		t.Fatal(err)
	}
	_, sentQueueID, err := router.IssueCommand(ctx, expired, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-y", 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, lease := range leases {
		if lease.QueueID == sentQueueID {
			if err := router.MarkSending(ctx, lease.QueueID, "worker-y"); err != nil {
				t.Fatal(err)
			}
			if err := router.MarkSentOK(ctx, lease.QueueID, "worker-y"); err != nil {
				t.Fatal(err)
			}
		}
	}
	pending, err := router.PendingCommandsForNode(ctx, "sleepy-02", 8, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].CommandID != "cmd-fresh-01" {
		t.Fatalf("unexpected pending commands: %+v", pending)
	}
}

func TestPendingDigestFlagsUrgencyAcrossMixedQueueStates(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := router.UpsertManifest(ctx, "sleepy-03", &contracts.Manifest{
		HardwareID:          "sleepy-03",
		DeviceFamily:        "xiao-esp32s3",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"wifi"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "sleepy-03", &contracts.Lease{
		RoleLeaseID:      "lease-mixed-03",
		SiteID:           "site-a",
		LogicalBindingID: "binding-mixed-03",
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "wifi",
	}); err != nil {
		t.Fatal(err)
	}
	control := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-mixed-01",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-mixed-01",
		OccurredAt:    now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-05"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-03"},
		Delivery:      &contracts.DeliverySpec{ExpiresAt: now.Add(2 * time.Minute).Format(time.RFC3339Nano)},
		Payload:       map[string]any{"command_name": "threshold.set", "value": 3},
	}
	normal := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-mixed-02",
		Kind:          "command",
		Priority:      "normal",
		CommandID:     "cmd-mixed-02",
		OccurredAt:    now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-05"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-03"},
		Delivery:      &contracts.DeliverySpec{ExpiresAt: now.Add(20 * time.Minute).Format(time.RFC3339Nano)},
		Payload:       map[string]any{"command_name": "mode.set", "mode": "eco"},
	}
	if _, _, err := router.IssueCommand(ctx, control, "local", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := router.IssueCommand(ctx, normal, "local", ""); err != nil {
		t.Fatal(err)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-mix", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(leases))
	}
	if err := router.MarkSending(ctx, leases[0].QueueID, "worker-mix"); err != nil {
		t.Fatal(err)
	}
	digest, err := router.PendingCommandDigest(ctx, "sleepy-03", now)
	if err != nil {
		t.Fatal(err)
	}
	if digest.PendingCount != 2 {
		t.Fatalf("expected 2 pending commands, got %+v", digest)
	}
	if !digest.Urgent || !digest.ExpiresSoon {
		t.Fatalf("expected urgent and expires-soon digest, got %+v", digest)
	}
}

func TestSleepyLeaseRejectsAlwaysOnRole(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "battery-leaf-01", &contracts.Manifest{
		HardwareID:          "battery-leaf-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	err := router.UpsertLease(ctx, "battery-leaf-01", &contracts.Lease{
		RoleLeaseID:      "lease-bad-01",
		SiteID:           "site-a",
		LogicalBindingID: "binding-bad-01",
		FabricShortID:    intPtr(201),
		EffectiveRole:    "mesh_router",
		PrimaryBearer:    "wifi_mesh",
	})
	if err == nil {
		t.Fatal("expected sleepy/battery role enforcement error")
	}
}

func TestSleepyTinyControlRequiresLeaseAndShortID(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-route-01",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-route-01",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-06"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-04"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "sleepy_tiny_control"},
		Payload: map[string]any{
			"command_name":  "mode.set",
			"mode":          "maintenance_awake",
			"command_token": 0x1020,
		},
	}
	if _, _, err := router.IssueCommand(ctx, command, "local", ""); err == nil {
		t.Fatal("expected missing lease/short id error")
	}
	if err := router.UpsertManifest(ctx, "sleepy-04", &contracts.Manifest{
		HardwareID:          "sleepy-04",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora", "ble_maintenance"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "sleepy-04", &contracts.Lease{
		RoleLeaseID:      "lease-good-01",
		SiteID:           "site-a",
		LogicalBindingID: "binding-good-01",
		FabricShortID:    intPtr(204),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	_, queueID, err := router.IssueCommand(ctx, command, "local", "")
	if err != nil {
		t.Fatalf("expected sleepy_tiny_control planning to pass, got %v", err)
	}
	plan, err := router.PlanOutboundRoute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bearer != "lora_direct" || plan.PathLabel != "sleepy_tiny_control/direct" || !plan.PayloadFit {
		t.Fatalf("unexpected sleepy route plan: %+v", plan)
	}
	if plan.Detail["target_short_id"].(int) != 204 {
		t.Fatalf("expected target short id detail, got %+v", plan.Detail)
	}
	routeRecord, err := router.OutboxRoutePlan(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if routeRecord == nil || routeRecord.RouteStatus != "ready_to_send" || routeRecord.SelectedBearer != "lora_direct" || routeRecord.RoutePlan == nil {
		t.Fatalf("unexpected persisted route plan: %+v", routeRecord)
	}
	if err := router.UpsertManifest(ctx, "sleepy-04", &contracts.Manifest{
		HardwareID:          "sleepy-04",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"wifi"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.1"},
	}); err != nil {
		t.Fatal(err)
	}
	duplicateAck, duplicateQueueID, err := router.IssueCommand(ctx, command, "local", "")
	if err != nil {
		t.Fatalf("duplicate command must remain idempotent even if current route policy changes: %v", err)
	}
	if duplicateAck == nil || !duplicateAck.Duplicate || duplicateQueueID != queueID {
		t.Fatalf("unexpected duplicate command result: ack=%+v queue_id=%d original_queue_id=%d", duplicateAck, duplicateQueueID, queueID)
	}
}

func TestLoRaPrimaryRequiresExplicitCompactRouteClass(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "sleepy-default-lora", &contracts.Manifest{
		HardwareID:          "sleepy-default-lora",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "sleepy-default-lora", &contracts.Lease{
		RoleLeaseID:      "lease-default-lora",
		SiteID:           "site-a",
		LogicalBindingID: "binding-default-lora",
		FabricShortID:    intPtr(209),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-default-lora",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-default-lora",
		Source:        contracts.SourceRef{HardwareID: "controller-default-lora"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-default-lora"},
		Payload:       map[string]any{"command_name": "relay.set", "raw": map[string]any{"too": "rich"}},
	}
	if _, err := router.EnqueueOutbound(ctx, command, ""); err == nil {
		t.Fatal("expected implicit rich LoRa route to be rejected")
	}
}

func TestUnsupportedRouteClassIsRejected(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-route-bad",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-route-bad",
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        contracts.SourceRef{HardwareID: "controller-bad-route"},
		Target:        contracts.TargetRef{Kind: "node", Value: "powered-bad-route"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "unknown_mesh_magic"},
		Payload:       map[string]any{"command_name": "relay.toggle"},
	}
	if _, err := router.EnqueueOutbound(ctx, command, ""); err == nil {
		t.Fatal("expected unsupported route_class to be rejected")
	}
}

func TestIssueCommandRouteFailureDoesNotLeaveIssuedCommand(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-route-fail",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-route-fail",
		Source:        contracts.SourceRef{HardwareID: "controller-route-fail"},
		Target:        contracts.TargetRef{Kind: "node", Value: "node-route-fail"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "unknown_mesh_magic"},
		Payload:       map[string]any{"command_name": "relay.toggle"},
	}
	if _, _, err := router.IssueCommand(ctx, command, "local", ""); err == nil {
		t.Fatal("expected route planning failure")
	}
	state, err := router.CommandState(ctx, "cmd-route-fail")
	if err != nil {
		t.Fatal(err)
	}
	if state != "" {
		t.Fatalf("route failure should not leave issued command, got state %s", state)
	}
}

func TestIssueCommandRouteBlockedDoesNotPersistCommandOrQueue(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "node-command-blocked", &contracts.Manifest{
		HardwareID:          "node-command-blocked",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains_powered",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "node-command-blocked", &contracts.Lease{
		RoleLeaseID:      "lease-command-blocked",
		SiteID:           "site-a",
		LogicalBindingID: "binding-command-blocked",
		FabricShortID:    intPtr(229),
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-blocked",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-command-blocked",
		Source:        contracts.SourceRef{HardwareID: "controller-command-blocked"},
		Target:        contracts.TargetRef{Kind: "node", Value: "node-command-blocked"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "relay.set"},
	}
	if _, _, err := router.IssueCommand(ctx, command, "local", ""); err == nil {
		t.Fatal("expected route_blocked command to be rejected by IssueCommand")
	}
	state, err := router.CommandState(ctx, command.CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if state != "" {
		t.Fatalf("blocked command must not be marked issued, got %s", state)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_count"] != 0 || metrics["queued_route_blocked_count"] != 0 {
		t.Fatalf("blocked IssueCommand must not create queue rows, got %+v", metrics)
	}
}

func TestRegisterDeviceIsAtomic(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	manifest := &contracts.Manifest{
		HardwareID:          "atomic-device-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}
	lease := &contracts.Lease{
		RoleLeaseID:      "lease-atomic-device",
		SiteID:           "site-a",
		LogicalBindingID: "binding-atomic-device",
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "wifi_ip",
	}
	if err := router.RegisterDevice(ctx, manifest.HardwareID, manifest, lease); err == nil {
		t.Fatal("expected invalid lease bearer to fail")
	}
	info, err := router.RuntimeInfoForNode(ctx, manifest.HardwareID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Manifest != nil || info.Lease != nil {
		t.Fatalf("atomic register must not leave partial manifest/lease, got %+v", info)
	}
}

func TestRoutePendingQueueIsNotLeased(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-route-pending",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-route-pending",
		Source:        contracts.SourceRef{HardwareID: "controller-route-pending"},
		Target:        contracts.TargetRef{Kind: "node", Value: "node-route-pending"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "relay.set"},
	}
	queueID, err := router.EnqueueOutbound(ctx, command, "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := router.OutboxRoutePlan(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.RouteStatus != "route_pending" {
		t.Fatalf("expected route_pending record, got %+v", record)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-route-pending", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("route_pending queue must not be leased, got %+v", leases)
	}
	pending, err := router.PendingCommandsForNode(ctx, "node-route-pending", 8, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("route_pending queue must not wake sleepy pending digest, got %+v", pending)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["queued_route_pending_count"] != 1 || metrics["queued_ready_count"] != 0 {
		t.Fatalf("expected route_pending metrics, got %+v", metrics)
	}
	schema, err := router.SchemaInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if schema.SchemaVersion != 1 || schema.OpenedAt == "" {
		t.Fatalf("unexpected schema info: %+v", schema)
	}
}

func TestQueueMetricsBreakDownRouteStatus(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	ready := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-metrics-ready",
		Kind:          "event",
		Priority:      "normal",
		EventID:       "evt-metrics-ready",
		Source:        contracts.SourceRef{HardwareID: "sensor-metrics"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"event_type": "motion_detected"},
	}
	if _, err := router.EnqueueOutbound(ctx, ready, ""); err != nil {
		t.Fatal(err)
	}
	pending := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-metrics-pending",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-metrics-pending",
		Source:        contracts.SourceRef{HardwareID: "controller-metrics"},
		Target:        contracts.TargetRef{Kind: "node", Value: "node-metrics-pending"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "relay.set"},
	}
	if _, err := router.EnqueueOutbound(ctx, pending, ""); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertManifest(ctx, "node-metrics-blocked", &contracts.Manifest{
		HardwareID:          "node-metrics-blocked",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "node-metrics-blocked", &contracts.Lease{
		RoleLeaseID:      "lease-metrics-blocked",
		SiteID:           "site-a",
		LogicalBindingID: "binding-metrics-blocked",
		FabricShortID:    intPtr(222),
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	blocked := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-metrics-blocked",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-metrics-blocked",
		Source:        contracts.SourceRef{HardwareID: "controller-metrics"},
		Target:        contracts.TargetRef{Kind: "node", Value: "node-metrics-blocked"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "relay.set"},
	}
	if _, err := router.EnqueueOutbound(ctx, blocked, ""); err != nil {
		t.Fatal(err)
	}
	metrics, err := router.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int64{
		"queued_ready_count":                                                1,
		"queued_route_pending_count":                                        1,
		"queued_route_blocked_count":                                        1,
		"queued_ready_to_send_count":                                        1,
		"queued_ready_to_send_reason_default_count":                         1,
		"queued_route_pending_reason_lease_missing_count":                   1,
		"queued_route_blocked_reason_bearer_forbidden_by_route_class_count": 1,
	} {
		if metrics[key] != want {
			t.Fatalf("expected %s=%d, got metrics %+v", key, want, metrics)
		}
	}
}

func TestDeliveryRelayIntentAffectsPolicyRoute(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "relay-capable-01", &contracts.Manifest{
		HardwareID:          "relay-capable-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"lora_relay"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "relay-capable-01", &contracts.Lease{
		RoleLeaseID:      "lease-relay-capable",
		SiteID:           "site-a",
		LogicalBindingID: "binding-relay-capable",
		FabricShortID:    intPtr(223),
		EffectiveRole:    "lora_relay",
		PrimaryBearer:    "lora_relay",
	}); err != nil {
		t.Fatal(err)
	}
	hopLimit := 1
	event := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-relay-alert",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-relay-alert",
		Source:        contracts.SourceRef{HardwareID: "motion-relay"},
		Target:        contracts.TargetRef{Kind: "node", Value: "relay-capable-01"},
		Delivery: &contracts.DeliverySpec{
			RouteClass: "critical_alert",
			AllowRelay: boolPtr(true),
			HopLimit:   &hopLimit,
		},
		Payload: map[string]any{"event_type": "motion_detected", "severity": "critical", "final_target_short_id": 224},
	}
	plan, err := router.PlanOutboundRoute(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.PayloadFit || plan.Bearer != "lora_relay" || plan.Detail["hop_limit"].(int) != 1 {
		t.Fatalf("expected relay route to be allowed by delivery intent, got %+v", plan)
	}
	hopLimit = 0
	plan, err = router.PlanOutboundRoute(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "relay_forbidden_by_hop_limit" {
		t.Fatalf("expected relay blocked by hop_limit=0, got %+v", plan)
	}
	event.Delivery.HopLimit = nil
	event.Delivery.AllowRelay = boolPtr(false)
	plan, err = router.PlanOutboundRoute(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "relay_forbidden_by_delivery_policy" {
		t.Fatalf("expected relay blocked by allow_relay=false, got %+v", plan)
	}
}

func TestRelayRouteBlocksWhenHopCountExhaustsTTL(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "relay-ttl-01", &contracts.Manifest{
		HardwareID:          "relay-ttl-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"lora_relay"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "relay-ttl-01", &contracts.Lease{
		RoleLeaseID:      "lease-relay-ttl",
		SiteID:           "site-a",
		LogicalBindingID: "binding-relay-ttl",
		FabricShortID:    intPtr(224),
		EffectiveRole:    "lora_relay",
		PrimaryBearer:    "lora_relay",
	}); err != nil {
		t.Fatal(err)
	}
	hopLimit := 2
	hopCount := 2
	event := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-relay-ttl-exhausted",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-relay-ttl-exhausted",
		Source:        contracts.SourceRef{HardwareID: "motion-relay-ttl"},
		Target:        contracts.TargetRef{Kind: "node", Value: "relay-ttl-01"},
		Delivery: &contracts.DeliverySpec{
			RouteClass: "critical_alert",
			AllowRelay: boolPtr(true),
			HopLimit:   &hopLimit,
		},
		MeshMeta: &contracts.MeshMeta{HopCount: &hopCount, LastHop: "relay-a"},
		Payload:  map[string]any{"payload_bytes": 4, "allow_declared_lora_size_for_alpha": true},
	}
	plan, err := router.PlanOutboundRoute(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "relay_ttl_exhausted" {
		t.Fatalf("expected relay TTL exhausted by hop_count=hop_limit, got %+v", plan)
	}
}

func TestRoutePlannerSupportsMeshRelayBackboneAndRedundantCritical(t *testing.T) {
	tests := []struct {
		name            string
		routeClass      string
		hardwareID      string
		role            string
		primaryBearer   string
		supportedBearer string
		wantBearer      string
		wantRelay       bool
		wantRedundant   bool
		wantHopLimit    *int
	}{
		{
			name:            "lora one relay",
			routeClass:      "lora_relay_1",
			hardwareID:      "route-lora-relay-1",
			role:            "lora_relay",
			primaryBearer:   "lora_relay",
			supportedBearer: "lora",
			wantBearer:      "lora_relay",
			wantRelay:       true,
			wantHopLimit:    intPtr(1),
		},
		{
			name:            "wifi mesh backbone",
			routeClass:      "wifi_mesh_backbone",
			hardwareID:      "route-wifi-mesh-backbone",
			role:            "mesh_router",
			primaryBearer:   "wifi_mesh",
			supportedBearer: "wifi",
			wantBearer:      "wifi_mesh",
			wantRelay:       true,
		},
		{
			name:            "redundant critical",
			routeClass:      "redundant_critical",
			hardwareID:      "route-redundant-critical",
			role:            "dual_bearer_bridge",
			primaryBearer:   "wifi_ip",
			supportedBearer: "wifi",
			wantBearer:      "wifi_ip",
			wantRelay:       true,
			wantRedundant:   true,
			wantHopLimit:    intPtr(2),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := openTestRouter(t)
			ctx := context.Background()
			if err := router.UpsertManifest(ctx, tt.hardwareID, &contracts.Manifest{
				HardwareID:          tt.hardwareID,
				DeviceFamily:        "xiao-esp32s3-sx1262",
				PowerClass:          "mains",
				WakeClass:           "always_on",
				SupportedBearers:    []string{tt.supportedBearer},
				AllowedNetworkRoles: []string{tt.role},
				Firmware:            map[string]any{"app": "0.1.0"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := router.UpsertLease(ctx, tt.hardwareID, &contracts.Lease{
				RoleLeaseID:      "lease-" + tt.hardwareID,
				SiteID:           "site-a",
				LogicalBindingID: "binding-" + tt.hardwareID,
				FabricShortID:    intPtr(500),
				EffectiveRole:    tt.role,
				PrimaryBearer:    tt.primaryBearer,
			}); err != nil {
				t.Fatal(err)
			}
			event := &contracts.Envelope{
				SchemaVersion: "1.0.0",
				MessageID:     "msg-" + tt.hardwareID,
				Kind:          "event",
				Priority:      "critical",
				EventID:       "evt-" + tt.hardwareID,
				Source:        contracts.SourceRef{HardwareID: "controller-" + tt.hardwareID},
				Target:        contracts.TargetRef{Kind: "node", Value: tt.hardwareID},
				Delivery:      &contracts.DeliverySpec{RouteClass: tt.routeClass},
				Payload:       map[string]any{"payload_bytes": 4, "allow_declared_lora_size_for_alpha": true, "final_target_short_id": 501},
			}
			plan, err := router.PlanOutboundRoute(ctx, event)
			if err != nil {
				t.Fatalf("%s should be supported by route planner: %v", tt.routeClass, err)
			}
			if !plan.PayloadFit || plan.Bearer != tt.wantBearer || plan.RouteClass != tt.routeClass {
				t.Fatalf("unexpected plan for %s: %+v", tt.routeClass, plan)
			}
			if plan.AllowRelay != tt.wantRelay || plan.AllowRedundant != tt.wantRedundant {
				t.Fatalf("unexpected relay policy for %s: %+v", tt.routeClass, plan)
			}
			if tt.wantHopLimit != nil {
				if plan.HopLimit == nil || *plan.HopLimit != *tt.wantHopLimit {
					t.Fatalf("expected hop_limit=%d for %s, got %+v", *tt.wantHopLimit, tt.routeClass, plan)
				}
			}
		})
	}
}

func TestLoRaRelayOneHopUsesRelayedPayloadCap(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "leaf-behind-relay-01", &contracts.Manifest{
		HardwareID:          "leaf-behind-relay-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "leaf-behind-relay-01", &contracts.Lease{
		RoleLeaseID:      "lease-leaf-behind-relay",
		SiteID:           "site-a",
		LogicalBindingID: "binding-leaf-behind-relay",
		FabricShortID:    intPtr(201),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertManifest(ctx, "relay-hop-01", &contracts.Manifest{
		HardwareID:          "relay-hop-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"lora_relay"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "relay-hop-01", &contracts.Lease{
		RoleLeaseID:      "lease-relay-hop",
		SiteID:           "site-a",
		LogicalBindingID: "binding-relay-hop",
		FabricShortID:    intPtr(302),
		EffectiveRole:    "lora_relay",
		PrimaryBearer:    "lora_relay",
	}); err != nil {
		t.Fatal(err)
	}
	ok := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-relay-hop-ok",
		Kind:          "fabric_summary",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "controller-relay-hop"},
		Target:        contracts.TargetRef{Kind: "node", Value: "leaf-behind-relay-01"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "lora_relay_1"},
		Payload:       map[string]any{"payload_bytes": 6, "allow_declared_lora_size_for_alpha": true, "relay_hardware_id": "relay-hop-01"},
	}
	plan, err := router.PlanOutboundRoute(ctx, ok)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.PayloadFit || plan.Bearer != "lora_relay" || plan.Detail["relayed"] != true ||
		plan.NextHopID != "relay-hop-01" || plan.FinalTargetID != "leaf-behind-relay-01" ||
		plan.NextHopShortID == nil || *plan.NextHopShortID != 302 ||
		plan.FinalTargetShortID == nil || *plan.FinalTargetShortID != 201 ||
		plan.Detail["relay_short_id"].(int) != 302 || plan.Detail["final_target_short_id"].(int) != 201 {
		t.Fatalf("unexpected relayed plan: %+v", plan)
	}
	tooLarge := *ok
	tooLarge.MessageID = "msg-relay-hop-large"
	tooLarge.Payload = map[string]any{"payload_bytes": 7, "allow_declared_lora_size_for_alpha": true, "relay_hardware_id": "relay-hop-01"}
	plan, err = router.PlanOutboundRoute(ctx, &tooLarge)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "lora_payload_too_large" {
		t.Fatalf("expected relayed payload cap gate, got %+v", plan)
	}
	noFinalShort := *ok
	noFinalShort.MessageID = "msg-relay-hop-no-final-short"
	noFinalShort.Target = contracts.TargetRef{Kind: "node", Value: "relayless-leaf-01"}
	noFinalShort.Payload = map[string]any{"payload_bytes": 6, "allow_declared_lora_size_for_alpha": true, "relay_hardware_id": "relay-hop-01"}
	if err := router.UpsertManifest(ctx, "relayless-leaf-01", &contracts.Manifest{
		HardwareID:          "relayless-leaf-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "relayless-leaf-01", &contracts.Lease{
		RoleLeaseID:      "lease-relayless-leaf",
		SiteID:           "site-a",
		LogicalBindingID: "binding-relayless-leaf",
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	plan, err = router.PlanOutboundRoute(ctx, &noFinalShort)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "lora_relay_requires_final_target_short_id" {
		t.Fatalf("expected final target short id gate, got %+v", plan)
	}
}

func TestLeasePrimaryBearerMustMatchManifest(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "bearer-guard-01", &contracts.Manifest{
		HardwareID:          "bearer-guard-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	err := router.UpsertLease(ctx, "bearer-guard-01", &contracts.Lease{
		RoleLeaseID:      "lease-bearer-bad",
		SiteID:           "site-a",
		LogicalBindingID: "binding-bearer-bad",
		FabricShortID:    intPtr(211),
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "wifi_ip",
	})
	if err == nil {
		t.Fatal("expected unsupported primary_bearer to be rejected")
	}
}

func TestFabricShortIDRangeIsValidated(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "short-range-01", &contracts.Manifest{
		HardwareID:          "short-range-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	err := router.UpsertLease(ctx, "short-range-01", &contracts.Lease{
		RoleLeaseID:      "lease-short-bad",
		SiteID:           "site-a",
		LogicalBindingID: "binding-short-bad",
		FabricShortID:    intPtr(70000),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	})
	if err == nil {
		t.Fatal("expected out-of-range fabric_short_id to be rejected")
	}
}

func TestPolicyRouteClassesHaveSafeMinimalGates(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	if err := router.UpsertManifest(ctx, "wifi-local-01", &contracts.Manifest{
		HardwareID:          "wifi-local-01",
		DeviceFamily:        "xiao-esp32s3",
		PowerClass:          "mains",
		WakeClass:           "always_on",
		SupportedBearers:    []string{"wifi"},
		AllowedNetworkRoles: []string{"powered_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "wifi-local-01", &contracts.Lease{
		RoleLeaseID:      "lease-wifi-local",
		SiteID:           "site-a",
		LogicalBindingID: "binding-wifi-local",
		EffectiveRole:    "powered_leaf",
		PrimaryBearer:    "wifi_ip",
	}); err != nil {
		t.Fatal(err)
	}
	local := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-local-control",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-local-control",
		Source:        contracts.SourceRef{HardwareID: "controller-local"},
		Target:        contracts.TargetRef{Kind: "node", Value: "wifi-local-01"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "servo.set_angle"},
	}
	plan, err := router.PlanOutboundRoute(ctx, local)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bearer != "wifi_ip" || plan.RouteClass != "local_control" || !plan.PayloadFit {
		t.Fatalf("unexpected local_control plan: %+v", plan)
	}
	if err := router.UpsertManifest(ctx, "lora-summary-01", &contracts.Manifest{
		HardwareID:          "lora-summary-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.UpsertLease(ctx, "lora-summary-01", &contracts.Lease{
		RoleLeaseID:      "lease-lora-summary",
		SiteID:           "site-a",
		LogicalBindingID: "binding-lora-summary",
		FabricShortID:    intPtr(212),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	summary := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-summary-ok",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-summary-ok",
		Source:        contracts.SourceRef{HardwareID: "controller-summary"},
		Target:        contracts.TargetRef{Kind: "node", Value: "lora-summary-01"},
		Delivery:      &contracts.DeliverySpec{RouteClass: "sparse_summary"},
		Payload:       map[string]any{"payload_bytes": 6, "allow_declared_lora_size_for_alpha": true},
	}
	plan, err = router.PlanOutboundRoute(ctx, summary)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bearer != "lora_direct" || !plan.PayloadFit {
		t.Fatalf("unexpected sparse_summary plan: %+v", plan)
	}
	summary.Payload = map[string]any{"payload_bytes": 99, "allow_declared_lora_size_for_alpha": true}
	plan, err = router.PlanOutboundRoute(ctx, summary)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "lora_payload_too_large" {
		t.Fatalf("expected lora payload too large gate, got %+v", plan)
	}
}

func TestRouteClassPolicyArtifactAllowedBearersMatchPlanner(t *testing.T) {
	artifact := loadRoutePolicyArtifact(t)
	bearers := []string{"lora_direct", "wifi", "wifi_ip", "usb_cdc", "wifi_mesh", "ble_maintenance"}
	for routeClass, policy := range artifact.RouteClasses {
		for index, bearer := range bearers {
			router := openTestRouter(t)
			role := policyRoleForRoute(policy)
			hardwareID := routeClass + "-" + bearer
			shortID := intPtr(300 + index)
			upsertPolicyNode(t, router, hardwareID, role, primaryBearerForPolicyLabel(bearer), shortID)
			plan, err := router.PlanOutboundRoute(context.Background(), routePolicyEnvelope(routeClass, hardwareID, 4))
			allowed := routePolicyAllowsBearer(policy, bearer)
			if allowed {
				if err != nil {
					t.Fatalf("%s should allow bearer %s: %v", routeClass, bearer, err)
				}
				if !plan.PayloadFit {
					t.Fatalf("%s should produce payload-fit plan for bearer %s, got %+v", routeClass, bearer, plan)
				}
				continue
			}
			if err == nil && plan.PayloadFit {
				t.Fatalf("%s must reject bearer %s per route-classes.json, got %+v", routeClass, bearer, plan)
			}
		}
	}
}

func TestRouteClassPolicyArtifactLoRaCapsMatchPlanner(t *testing.T) {
	artifact := loadRoutePolicyArtifact(t)
	for routeClass, policy := range artifact.RouteClasses {
		if policy.MaxLoRaBodyBytes == nil || !routePolicyAllowsBearer(policy, "lora_direct") || routeClass == "sleepy_tiny_control" {
			continue
		}
		router := openTestRouter(t)
		hardwareID := "cap-" + routeClass
		upsertPolicyNode(t, router, hardwareID, policyRoleForRoute(policy), "lora", intPtr(410))

		plan, err := router.PlanOutboundRoute(context.Background(), routePolicyEnvelope(routeClass, hardwareID, *policy.MaxLoRaBodyBytes))
		if err != nil {
			t.Fatalf("%s should accept max_lora_body_bytes=%d: %v", routeClass, *policy.MaxLoRaBodyBytes, err)
		}
		if !plan.PayloadFit {
			t.Fatalf("%s should fit max_lora_body_bytes=%d, got %+v", routeClass, *policy.MaxLoRaBodyBytes, plan)
		}

		plan, err = router.PlanOutboundRoute(context.Background(), routePolicyEnvelope(routeClass, hardwareID, *policy.MaxLoRaBodyBytes+1))
		if err != nil {
			t.Fatalf("%s over-cap planning should return a non-fit plan, not error: %v", routeClass, err)
		}
		if plan.PayloadFit {
			t.Fatalf("%s should reject max_lora_body_bytes+1, got %+v", routeClass, plan)
		}
	}
}

func TestDeclaredLoRaPayloadBytesRequireAlphaOptIn(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	upsertPolicyNode(t, router, "declared-alpha-gate", "sleepy_leaf", "lora", intPtr(420))
	envelope := routePolicyEnvelope("sparse_summary", "declared-alpha-gate", 4)
	delete(envelope.Payload, "allow_declared_lora_size_for_alpha")
	plan, err := router.PlanOutboundRoute(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "lora_requires_compact_payload" {
		t.Fatalf("declared bytes without alpha opt-in must not pass LoRa fit, got %+v", plan)
	}
	envelope.Payload["allow_declared_lora_size_for_alpha"] = true
	plan, err = router.PlanOutboundRoute(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.PayloadFit || plan.Detail["payload_fit_source"] != "declared_alpha_opt_in" {
		t.Fatalf("explicit alpha opt-in should pass declared bytes path, got %+v", plan)
	}
}

func TestRouteClassPolicyArtifactRelayDefaultsMatchPlanner(t *testing.T) {
	artifact := loadRoutePolicyArtifact(t)
	for routeClass, policy := range artifact.RouteClasses {
		router := openTestRouter(t)
		hardwareID := "relay-" + routeClass
		bearer := primaryBearerForPolicyLabel(policy.AllowedBearers[0])
		upsertPolicyNode(t, router, hardwareID, policyRoleForRoute(policy), bearer, intPtr(510))
		plan, err := router.PlanOutboundRoute(context.Background(), routePolicyEnvelope(routeClass, hardwareID, 4))
		if err != nil {
			t.Fatalf("%s route planning failed: %v", routeClass, err)
		}
		if plan.AllowRelay != policy.AllowRelay || plan.AllowRedundant != policy.AllowRedundant {
			t.Fatalf("%s relay defaults drifted from route-classes.json: plan relay=%t redundant=%t policy relay=%t redundant=%t",
				routeClass, plan.AllowRelay, plan.AllowRedundant, policy.AllowRelay, policy.AllowRedundant)
		}
		if policy.HopLimit != nil {
			if plan.HopLimit == nil || *plan.HopLimit != *policy.HopLimit {
				t.Fatalf("%s hop limit drifted from route-classes.json: plan=%v policy=%d", routeClass, plan.HopLimit, *policy.HopLimit)
			}
		}
	}
}

func TestRouteClassPolicyArtifactRequiredRolesAreEnforced(t *testing.T) {
	artifact := loadRoutePolicyArtifact(t)
	for routeClass, policy := range artifact.RouteClasses {
		if len(policy.RequiresTargetRole) == 0 {
			continue
		}
		router := openTestRouter(t)
		hardwareID := "wrong-role-" + routeClass
		bearer := primaryBearerForPolicyLabel(policy.AllowedBearers[0])
		upsertPolicyNode(t, router, hardwareID, "powered_leaf", bearer, intPtr(620))
		plan, err := router.PlanOutboundRoute(context.Background(), routePolicyEnvelope(routeClass, hardwareID, 4))
		if err != nil {
			continue
		}
		if plan.PayloadFit || (plan.Reason != "target_role_forbidden_by_route_class" && plan.Reason != "target_not_sleepy_leaf") {
			t.Fatalf("%s must enforce requires_target_role, got %+v", routeClass, plan)
		}
	}
}

func TestWifiMeshBackboneHonorsExplicitHopLimit(t *testing.T) {
	router := openTestRouter(t)
	upsertPolicyNode(t, router, "mesh-router-01", "mesh_router", "wifi_mesh", intPtr(630))
	hopCount := 1
	hopLimit := 1
	env := routePolicyEnvelope("wifi_mesh_backbone", "mesh-router-01", 4)
	env.Delivery.HopLimit = &hopLimit
	env.MeshMeta = &contracts.MeshMeta{HopCount: &hopCount}
	plan, err := router.PlanOutboundRoute(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PayloadFit || plan.Reason != "relay_ttl_exhausted" {
		t.Fatalf("expected wifi_mesh explicit hop limit to block exhausted path, got %+v", plan)
	}
}

func TestRolePolicyArtifactRequiresAlwaysOnMatchesLeaseValidation(t *testing.T) {
	artifact := loadRolePolicyArtifact(t)
	for role, policy := range artifact.Roles {
		router := openTestRouter(t)
		hardwareID := "role-policy-" + role
		if err := router.UpsertManifest(context.Background(), hardwareID, &contracts.Manifest{
			HardwareID:          hardwareID,
			DeviceFamily:        "xiao-esp32s3-sx1262",
			PowerClass:          "primary_battery",
			WakeClass:           "sleepy_event",
			SupportedBearers:    []string{"wifi"},
			AllowedNetworkRoles: []string{role},
			Firmware:            map[string]any{"app": "0.1.0"},
		}); err != nil {
			t.Fatal(err)
		}
		err := router.UpsertLease(context.Background(), hardwareID, &contracts.Lease{
			RoleLeaseID:      "lease-" + hardwareID,
			SiteID:           "site-a",
			LogicalBindingID: "binding-" + hardwareID,
			EffectiveRole:    role,
			PrimaryBearer:    "wifi",
		})
		if policy.RequiresAlwaysOn && err == nil {
			t.Fatalf("%s requires always-on per role-policy.json, but battery lease was accepted", role)
		}
		if !policy.RequiresAlwaysOn && err != nil {
			t.Fatalf("%s does not require always-on per role-policy.json, but battery lease was rejected: %v", role, err)
		}
	}
}

func TestDuplicateCommandResultSamePhaseIsIdempotent(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-token-01",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-token-01",
		Source:        contracts.SourceRef{HardwareID: "controller-07"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-07"},
		Payload: map[string]any{
			"command_name":  "mode.set",
			"mode":          "maintenance_awake",
			"command_token": 0x2201,
		},
	}
	if _, err := router.Ingest(ctx, command, "local"); err != nil {
		t.Fatal(err)
	}
	result := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-token-result-01",
		Kind:          "command_result",
		Priority:      "control",
		Source:        contracts.SourceRef{HardwareID: "sleepy-07"},
		Target:        contracts.TargetRef{Kind: "client", Value: "controller-07"},
		Payload: map[string]any{
			"command_token": 0x2201,
			"phase":         "accepted",
		},
	}
	if _, err := router.Ingest(ctx, result, "local"); err != nil {
		t.Fatal(err)
	}
	result.MessageID = "msg-command-token-result-02"
	if _, err := router.Ingest(ctx, result, "local"); err != nil {
		t.Fatalf("duplicate same-phase result should be idempotent, got %v", err)
	}
}

func TestCommandTokenIsScopedByTargetNode(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	for _, target := range []string{"sleepy-token-a", "sleepy-token-b"} {
		command := &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     "msg-command-" + target,
			Kind:          "command",
			Priority:      "control",
			CommandID:     "cmd-" + target,
			Source:        contracts.SourceRef{HardwareID: "controller-token"},
			Target:        contracts.TargetRef{Kind: "node", Value: target},
			Payload: map[string]any{
				"command_name":  "mode.set",
				"mode":          "maintenance_awake",
				"command_token": 0x3301,
			},
		}
		if _, err := router.Ingest(ctx, command, "local"); err != nil {
			t.Fatalf("same command_token should be accepted for target %s: %v", target, err)
		}
	}
	result := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-command-token-result-scoped",
		Kind:          "command_result",
		Priority:      "control",
		Source:        contracts.SourceRef{HardwareID: "sleepy-token-b"},
		Target:        contracts.TargetRef{Kind: "client", Value: "controller-token"},
		Payload: map[string]any{
			"command_token": 0x3301,
			"phase":         "succeeded",
		},
	}
	if _, err := router.Ingest(ctx, result, "local"); err != nil {
		t.Fatal(err)
	}
	stateB, err := router.CommandState(ctx, "cmd-sleepy-token-b")
	if err != nil {
		t.Fatal(err)
	}
	if stateB != "succeeded" {
		t.Fatalf("expected target-scoped token to resolve command B, got %s", stateB)
	}
	stateA, err := router.CommandState(ctx, "cmd-sleepy-token-a")
	if err != nil {
		t.Fatal(err)
	}
	if stateA != "issued" {
		t.Fatalf("expected command A to remain issued, got %s", stateA)
	}
}

func TestCommandTokenCanBeReusedAcrossWindows(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	for _, window := range []string{"poll-001", "poll-002"} {
		command := &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     "msg-token-window-" + window,
			Kind:          "command",
			Priority:      "control",
			CommandID:     "cmd-token-window-" + window,
			Source:        contracts.SourceRef{HardwareID: "controller-token-window"},
			Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-token-window"},
			Payload: map[string]any{
				"command_name":      "mode.set",
				"mode":              "maintenance_awake",
				"command_token":     0x4401,
				"command_window_id": window,
			},
		}
		if _, err := router.Ingest(ctx, command, "local"); err != nil {
			t.Fatalf("same target token should be reusable across command windows: %v", err)
		}
	}
	resolved, err := router.ResolveCommandIDByTokenForTarget(ctx, "sleepy-token-window", 0x4401)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "cmd-token-window-poll-002" {
		t.Fatalf("expected latest command window to resolve, got %s", resolved)
	}
}

func TestHeartbeatSummaryAndFileChunkSemantics(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	heartbeat := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-heartbeat-01",
		Kind:          "heartbeat",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "gateway-head-01"},
		Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
		Delivery: &contracts.DeliverySpec{
			IngressMeta: map[string]any{"host_link": "usb_cdc", "bearer": "lora_direct"},
		},
		Payload: map[string]any{"gateway_id": "gw-alpha", "status": "lora_ingress"},
	}
	if _, err := router.Ingest(ctx, heartbeat, "usb-gw-alpha"); err != nil {
		t.Fatal(err)
	}
	record, err := router.LatestHeartbeatBySubject(ctx, "gateway", "gw-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.HeartbeatKey != "gateway:gw-alpha" || record.GatewayID != "gw-alpha" || record.SubjectKind != "gateway" || record.SubjectID != "gw-alpha" || !record.Live || record.HostLink != "usb_cdc" || record.Bearer != "lora_direct" {
		t.Fatalf("unexpected heartbeat record: %+v", record)
	}
	legacyRecord, err := router.LatestHeartbeat(ctx, "gw-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if legacyRecord == nil || legacyRecord.HeartbeatKey != "gateway:gw-alpha" {
		t.Fatalf("expected legacy bare gateway lookup to resolve gateway key, got %+v", legacyRecord)
	}
	strictBad := *heartbeat
	strictBad.MessageID = "msg-heartbeat-strict-bad"
	strictBad.Payload = map[string]any{"gateway_id": "gw-alpha", "status": "live", "production": true}
	if _, err := router.Ingest(ctx, &strictBad, "usb-gw-alpha"); err == nil {
		t.Fatal("production heartbeat without subject_kind/subject_id must be rejected")
	}
	legacyLiveNode := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-heartbeat-live-node",
		Kind:          "heartbeat",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "node-live-status"},
		Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
		Payload:       map[string]any{"status": "live", "live": true},
	}
	if _, err := router.Ingest(ctx, legacyLiveNode, "lora-gw-alpha"); err != nil {
		t.Fatal(err)
	}
	liveNodeRecord, err := router.LatestHeartbeatBySubject(ctx, "node", "node-live-status")
	if err != nil {
		t.Fatal(err)
	}
	if liveNodeRecord == nil || liveNodeRecord.SubjectKind != "node" {
		t.Fatalf("legacy status=live without gateway_id should infer node, got %+v", liveNodeRecord)
	}
	nodeHeartbeat := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-heartbeat-node-01",
		Kind:          "heartbeat",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "node-gw-alpha"},
		Target:        contracts.TargetRef{Kind: "host", Value: "site-router"},
		Payload:       map[string]any{"subject_kind": "node", "subject_id": "gw-alpha", "status": "onair_heartbeat", "live": true},
	}
	if _, err := router.Ingest(ctx, nodeHeartbeat, "lora-gw-alpha"); err != nil {
		t.Fatal(err)
	}
	nodeRecord, err := router.LatestHeartbeatBySubject(ctx, "node", "gw-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if nodeRecord == nil || nodeRecord.HeartbeatKey != "node:gw-alpha" || nodeRecord.SubjectKind != "node" {
		t.Fatalf("expected node heartbeat to keep separate key, got %+v", nodeRecord)
	}
	heartbeats, err := router.ListHeartbeats(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(heartbeats) != 3 {
		t.Fatalf("expected gateway and node heartbeats, got %+v", heartbeats)
	}
	nodeHeartbeats, err := router.ListHeartbeats(ctx, "node", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeHeartbeats) != 2 {
		t.Fatalf("expected node-filtered heartbeat list, got %+v", nodeHeartbeats)
	}
	gatewayRecord, err := router.LatestHeartbeatBySubject(ctx, "gateway", "gw-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if gatewayRecord == nil || gatewayRecord.MessageID != "msg-heartbeat-01" {
		t.Fatalf("node heartbeat should not overwrite gateway heartbeat, got %+v", gatewayRecord)
	}

	summary := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-summary-01",
		Kind:          "fabric_summary",
		Priority:      "normal",
		Source:        contracts.SourceRef{HardwareID: "router-01"},
		Target:        contracts.TargetRef{Kind: "site", Value: "site-a"},
		Payload:       map[string]any{"summary_scope": "site-a", "healthy_nodes": 4, "degraded_nodes": 1},
	}
	if _, err := router.Ingest(ctx, summary, "mesh-root"); err != nil {
		t.Fatal(err)
	}
	summaryRecord, err := router.LatestFabricSummary(ctx, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if summaryRecord == nil || summaryRecord.Payload["healthy_nodes"].(float64) != 4 {
		t.Fatalf("unexpected summary record: %+v", summaryRecord)
	}

	messageIDs := []string{"msg-file-chunk-01", "msg-file-chunk-02", "msg-file-chunk-03"}
	for idx, messageID := range messageIDs {
		chunk := &contracts.Envelope{
			SchemaVersion: "1.0.0",
			MessageID:     messageID,
			Kind:          "file_chunk",
			Priority:      "bulk",
			CorrelationID: "file-demo-01",
			Source:        contracts.SourceRef{HardwareID: "controller-ota"},
			Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-ota-01"},
			Payload: map[string]any{
				"file_id":      "file-demo-01",
				"chunk_index":  idx,
				"total_chunks": 3,
			},
		}
		if _, err := router.Ingest(ctx, chunk, "ota-host"); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := router.FileChunkProgress(ctx, "file-demo-01")
	if err != nil {
		t.Fatal(err)
	}
	if progress == nil || progress.ReceivedChunks != 3 || !progress.Complete {
		t.Fatalf("unexpected file chunk progress: %+v", progress)
	}

	old := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339Nano)
	if _, err := router.db.ExecContext(ctx, `UPDATE heartbeat_ledger SET updated_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := router.db.ExecContext(ctx, `UPDATE file_chunk_ledger SET updated_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	result, err := router.Compact(ctx, RetentionPolicy{HeartbeatRetentionDays: 30, FileChunkRetentionDays: 3}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedHeartbeats != 3 || result.DeletedFileChunks != 3 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
}

func TestFileChunkTotalMismatchIsRejected(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-file-mismatch-01",
		Kind:          "file_chunk",
		Priority:      "bulk",
		CorrelationID: "file-mismatch-01",
		Source:        contracts.SourceRef{HardwareID: "controller-ota"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-ota-02"},
		Payload:       map[string]any{"file_id": "file-mismatch-01", "chunk_index": 0, "total_chunks": 3},
	}
	if _, err := router.Ingest(ctx, first, "ota-host"); err != nil {
		t.Fatal(err)
	}
	second := *first
	second.MessageID = "msg-file-mismatch-02"
	second.Payload = map[string]any{"file_id": "file-mismatch-01", "chunk_index": 1, "total_chunks": 4}
	if _, err := router.Ingest(ctx, &second, "ota-host"); err == nil {
		t.Fatal("expected mismatched total_chunks to be rejected")
	}
}

func TestOutboundAttemptLedgerTracksPathChoices(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	envelope := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-attempt-01",
		Kind:          "event",
		Priority:      "critical",
		EventID:       "evt-attempt-01",
		Source:        contracts.SourceRef{HardwareID: "sensor-attempt-01"},
		Target:        contracts.TargetRef{Kind: "service", Value: "alerts"},
		Payload:       map[string]any{"alarm_code": "water"},
	}
	queueID, err := router.EnqueueOutbound(ctx, envelope, "")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := router.RecordOutboundAttempt(ctx, queueID, "lora", "gw-a", "direct-primary", map[string]any{"rssi": -91})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.UpdateOutboundAttempt(ctx, attempt.AttemptID, "sent_ok", map[string]any{"airtime_ms": 512}); err != nil {
		t.Fatal(err)
	}
	attempts, err := router.ListOutboundAttempts(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != "sent_ok" || attempts[0].GatewayID != "gw-a" {
		t.Fatalf("unexpected attempts: %+v", attempts)
	}
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano)
	if _, err := router.db.ExecContext(ctx, `UPDATE outbox_queue SET status = 'dead', updated_at = ? WHERE id = ?`, old, queueID); err != nil {
		t.Fatal(err)
	}
	result, err := router.Compact(ctx, RetentionPolicy{DeadQueueRetentionDays: 14}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedDeadQueueItems != 1 || result.DeletedDeadQueueAttempts != 1 {
		t.Fatalf("expected dead queue and attempt retention cleanup, got %+v", result)
	}
	attempts, err = router.ListOutboundAttempts(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("dead queue compaction must remove orphaned attempts, got %+v", attempts)
	}
}

func TestFileChunkProgressRequiresContiguousCoverageAndLatestUpdate(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	first := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-z-last",
		Kind:          "file_chunk",
		Priority:      "bulk",
		CorrelationID: "file-gap-01",
		Source:        contracts.SourceRef{HardwareID: "controller-gap"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-gap"},
		Payload:       map[string]any{"file_id": "file-gap-01", "chunk_index": 1, "total_chunks": 3},
	}
	second := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-a-last",
		Kind:          "file_chunk",
		Priority:      "bulk",
		CorrelationID: "file-gap-01",
		Source:        contracts.SourceRef{HardwareID: "controller-gap"},
		Target:        contracts.TargetRef{Kind: "node", Value: "sleepy-gap"},
		Payload:       map[string]any{"file_id": "file-gap-01", "chunk_index": 2, "total_chunks": 3},
	}
	if _, err := router.Ingest(ctx, first, "gap-host"); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Ingest(ctx, second, "gap-host"); err != nil {
		t.Fatal(err)
	}
	progress, err := router.FileChunkProgress(ctx, "file-gap-01")
	if err != nil {
		t.Fatal(err)
	}
	if progress.Complete {
		t.Fatalf("expected incomplete chunk progress, got %+v", progress)
	}
	if progress.LastMessageID != "msg-a-last" {
		t.Fatalf("expected last_message_id to track latest update, got %+v", progress)
	}
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
