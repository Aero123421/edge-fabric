package siterouter

import (
	"context"
	"errors"
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
	if _, _, err := router.IssueCommand(ctx, command, "local", ""); err != nil {
		t.Fatalf("expected sleepy_tiny_control planning to pass, got %v", err)
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
		Payload: map[string]any{"gateway_id": "gw-alpha", "live": true, "status": "lora_ingress"},
	}
	if _, err := router.Ingest(ctx, heartbeat, "usb-gw-alpha"); err != nil {
		t.Fatal(err)
	}
	record, err := router.LatestHeartbeat(ctx, "gw-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.GatewayID != "gw-alpha" || !record.Live || record.HostLink != "usb_cdc" || record.Bearer != "lora_direct" {
		t.Fatalf("unexpected heartbeat record: %+v", record)
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
