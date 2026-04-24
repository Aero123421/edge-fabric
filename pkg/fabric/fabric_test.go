package fabric

import (
	"context"
	"path/filepath"
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
