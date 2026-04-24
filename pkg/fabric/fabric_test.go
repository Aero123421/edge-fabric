package fabric

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFabricOpenLocalPublishStateAndEmitEvent(t *testing.T) {
	client, err := OpenLocal(filepath.Join(t.TempDir(), "site.db"), "app-fabric-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	if ack, err := client.PublishState(context.Background(), State{
		Source: "temp-fabric-01",
		Key:    "temperature.c",
		Value:  24.5,
	}); err != nil || ack.Status != "persisted" {
		t.Fatalf("unexpected state result ack=%+v err=%v", ack, err)
	}
	if ack, err := client.EmitEvent(context.Background(), Event{
		EventID:  "evt-fabric-motion-01",
		Source:   "motion-fabric-01",
		Type:     EventMotionDetected,
		Severity: Critical,
		Bucket:   3,
	}); err != nil || ack.Status != "persisted" {
		t.Fatalf("unexpected event result ack=%+v err=%v", ack, err)
	}
	first, err := client.EmitEvent(context.Background(), Event{
		IdempotencyKey: "boot-1:seq-7",
		Source:         "motion-fabric-01",
		Type:           EventMotionDetected,
		Severity:       Critical,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.EmitEvent(context.Background(), Event{
		IdempotencyKey: "boot-1:seq-7",
		Source:         "motion-fabric-01",
		Type:           EventMotionDetected,
		Severity:       Critical,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.AckedMessageID != first.AckedMessageID {
		t.Fatalf("expected idempotency key duplicate, first=%+v second=%+v", first, second)
	}
}

func TestFabricDeviceProfileAndSleepyBuilder(t *testing.T) {
	client, err := OpenLocal(filepath.Join(t.TempDir(), "site.db"), "app-fabric-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	if err := RegisterDeviceProfile(
		context.Background(),
		client,
		"sleepy-fabric-01",
		MotionSensorBatteryProfile(),
		221,
		WithRole("sleepy_leaf"),
		WithPrimaryBearer("lora"),
		WithFallbackBearer("ble_maintenance"),
	); err != nil {
		t.Fatal(err)
	}
	result, err := client.SleepyCommand("sleepy-fabric-01").
		ThresholdSet(42).
		CommandID("cmd-fabric-threshold-01").
		ExpiresIn(5 * time.Minute).
		SendResult(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted || result.QueueID == 0 || result.CommandID != "cmd-fabric-threshold-01" {
		t.Fatalf("unexpected sleepy command result %+v", result)
	}
	if !result.ReadyToSend || result.RouteStatus != "ready_to_send" || result.SelectedBearer != "lora_direct" || !result.PayloadFit {
		t.Fatalf("expected app-facing route result, got %+v", result)
	}
}

func TestFabricRejectsOutOfRangeShortID(t *testing.T) {
	client, err := OpenLocal(filepath.Join(t.TempDir(), "site.db"), "app-fabric-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	if err := RegisterDeviceProfile(context.Background(), client, "sleepy-fabric-bad-short", MotionSensorBatteryProfile(), 70000); err == nil {
		t.Fatal("expected out-of-range short ID to be rejected")
	}
}

func TestBuiltInProfilesMatchPolicyArtifact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "policy", "device-profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Profiles map[string]struct {
			PowerClass       string            `json:"power_class"`
			WakeClass        string            `json:"wake_class"`
			AllowedRoles     []string          `json:"allowed_roles"`
			SupportedBearers []string          `json:"supported_bearers"`
			DefaultRoutes    map[string]string `json:"default_routes"`
			Forbidden        map[string]bool   `json:"forbidden"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []DeviceProfile{
		MotionSensorBatteryProfile(),
		LeakSensorSleepyProfile(),
		PoweredServoControllerProfile(),
	} {
		expected, ok := artifact.Profiles[profile.ID]
		if !ok {
			t.Fatalf("profile %s missing from artifact", profile.ID)
		}
		if profile.PowerClass != expected.PowerClass ||
			profile.WakeClass != expected.WakeClass ||
			!reflect.DeepEqual(profile.AllowedRoles, expected.AllowedRoles) ||
			!reflect.DeepEqual(profile.SupportedBearers, expected.SupportedBearers) ||
			!reflect.DeepEqual(profile.DefaultRoutes, expected.DefaultRoutes) ||
			!reflect.DeepEqual(profile.Forbidden, expected.Forbidden) {
			t.Fatalf("profile %s drifted from artifact: %+v expected %+v", profile.ID, profile, expected)
		}
	}
}
