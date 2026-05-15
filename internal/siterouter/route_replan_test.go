package siterouter

import (
	"context"
	"testing"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func TestReplanQueuedRoutesMovesPendingToReady(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	target := "node-replan-ready"
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-replan-pending",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-replan-pending",
		Source:        contracts.SourceRef{HardwareID: "controller-replan"},
		Target:        contracts.TargetRef{Kind: "node", Value: target},
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
	if record.RouteStatus != "route_pending" {
		t.Fatalf("expected initial route_pending, got %+v", record)
	}
	upsertPolicyNode(t, router, target, "powered_leaf", "wifi", nil)
	result, err := router.ReplanQueuedRoutes(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 1 || result.ReadyToSend != 1 {
		t.Fatalf("unexpected replan result: %+v", result)
	}
	record, err = router.OutboxRoutePlan(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RouteStatus != "ready_to_send" || record.SelectedBearer != "wifi_ip" {
		t.Fatalf("expected ready route after replan, got %+v", record)
	}
	leases, err := router.LeaseOutbound(ctx, "worker-replan-ready", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].QueueID != queueID {
		t.Fatalf("expected replanned queue to be leaseable, got %+v", leases)
	}
}

func TestReplanQueuedRoutesKeepsBlockedUntilInputsChange(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	target := "node-replan-blocked"
	upsertPolicyNode(t, router, target, "powered_leaf", "lora", intPtr(331))
	command := &contracts.Envelope{
		SchemaVersion: "1.0.0",
		MessageID:     "msg-replan-blocked",
		Kind:          "command",
		Priority:      "control",
		CommandID:     "cmd-replan-blocked",
		Source:        contracts.SourceRef{HardwareID: "controller-replan"},
		Target:        contracts.TargetRef{Kind: "node", Value: target},
		Delivery:      &contracts.DeliverySpec{RouteClass: "local_control"},
		Payload:       map[string]any{"command_name": "relay.set"},
	}
	queueID, err := router.EnqueueOutbound(ctx, command, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := router.ReplanQueuedRoutes(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if result.RouteBlocked != 1 || result.ReadyToSend != 0 {
		t.Fatalf("expected blocked replan result, got %+v", result)
	}
	record, err := router.OutboxRoutePlan(ctx, queueID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RouteStatus != "route_blocked" || record.RouteReason != "bearer_forbidden_by_route_class" {
		t.Fatalf("expected blocked route to remain blocked, got %+v", record)
	}
}

func TestUpdateQueueRoutePlanDoesNotReportConflictAsUpdated(t *testing.T) {
	router := openTestRouter(t)
	ctx := context.Background()
	queueID := enqueueReadyServiceItem(t, router, "msg-replan-conflict", "evt-replan-conflict")
	leases, err := router.LeaseOutbound(ctx, "worker-replan-conflict", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].QueueID != queueID {
		t.Fatalf("expected one lease, got %+v", leases)
	}
	_, updated, err := router.updateQueueRoutePlan(ctx, queueID, &RoutePlan{
		Bearer:     "host_local",
		PayloadFit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("leased queue item must not be counted as an updated replan")
	}
}
