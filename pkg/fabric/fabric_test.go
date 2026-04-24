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
	if err := RegisterDeviceProfile(context.Background(), client, "sleepy-fabric-01", MotionSensorBatteryProfile(), 221); err != nil {
		t.Fatal(err)
	}
	ack, queueID, err := client.SleepyCommand("sleepy-fabric-01").
		ThresholdSet(42).
		CommandID("cmd-fabric-threshold-01").
		ExpiresIn(5 * time.Minute).
		Send(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "persisted" || queueID == 0 {
		t.Fatalf("unexpected sleepy command result ack=%+v queueID=%d", ack, queueID)
	}
}
