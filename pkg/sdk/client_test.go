package sdk

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aero123421/edge-fabric/internal/siterouter"
	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

func openRouter(t *testing.T) *siterouter.Router {
	t.Helper()
	router, err := siterouter.Open(filepath.Join(t.TempDir(), "site-router.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
	})
	return router
}

func TestIssueCommandBuildsPendingDigest(t *testing.T) {
	router := openRouter(t)
	client := NewLocalSiteRouterClient(router, "controller-sdk")
	if err := client.RegisterManifest(context.Background(), "sleepy-sdk-01", &contracts.Manifest{
		HardwareID:          "sleepy-sdk-01",
		DeviceFamily:        "xiao-esp32s3-sx1262",
		PowerClass:          "primary_battery",
		WakeClass:           "sleepy_event",
		SupportedBearers:    []string{"lora", "ble_maintenance"},
		AllowedNetworkRoles: []string{"sleepy_leaf"},
		Firmware:            map[string]any{"app": "0.1.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterLease(context.Background(), "sleepy-sdk-01", &contracts.Lease{
		RoleLeaseID:      "lease-sdk-01",
		SiteID:           "site-a",
		LogicalBindingID: "binding-sleepy-sdk-01",
		FabricShortID:    intPtr(201),
		EffectiveRole:    "sleepy_leaf",
		PrimaryBearer:    "lora",
	}); err != nil {
		t.Fatal(err)
	}
	ack, queueID, err := client.IssueCommand(context.Background(), "sleepy-sdk-01", map[string]any{
		"command_name": "mode.set",
		"mode":         "maintenance_awake",
	}, CommandOptions{
		ServiceLevel:     "eventual_next_poll",
		ExpectedDelivery: "next_poll",
		IdempotencyKey:   "cmd-sdk-001",
		ExpiresAt:        time.Now().UTC().Add(10 * time.Minute),
		RouteClass:       "sleepy_tiny_control",
		Priority:         "control",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "persisted" || queueID == 0 {
		t.Fatalf("unexpected issue result: ack=%+v queueID=%d", ack, queueID)
	}
	state, err := client.ObserveCommand(context.Background(), "cmd-sdk-001")
	if err != nil {
		t.Fatal(err)
	}
	if state != "issued" {
		t.Fatalf("expected issued state, got %s", state)
	}
	digest, err := client.PendingCommandDigest(context.Background(), "sleepy-sdk-01", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if digest.PendingCount != 1 || digest.NewestCommandID != "cmd-sdk-001" {
		t.Fatalf("unexpected digest: %+v", digest)
	}
}

func TestOpenLocalSiteExposesUsableExternalEntryPoint(t *testing.T) {
	client, err := OpenLocalSite(filepath.Join(t.TempDir(), "site-router.db"), "controller-sdk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	ack, err := client.PublishState(context.Background(), "sensor-sdk-01", "temperature.c", map[string]any{"value": 24.5}, "")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "persisted" || ack.AckedMessageID == "" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

func intPtr(value int) *int {
	return &value
}
