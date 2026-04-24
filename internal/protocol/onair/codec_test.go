package onair

import "testing"

func TestStateRoundTrip(t *testing.T) {
	raw, err := EncodeState(201, false, StateBody{
		KeyToken:   StateKeyNodePower,
		ValueToken: StateValueAwake,
		EventWake:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if packet.SourceShortID != 201 || packet.Summary() {
		t.Fatalf("unexpected header: %+v", packet)
	}
	body, err := DecodeState(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.KeyToken != StateKeyNodePower || body.ValueToken != StateValueAwake || body.EventWake {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestCompactCommandFitsJP125LongSF10(t *testing.T) {
	raw, err := EncodeCompactCommand(201, false, CompactCommandBody{
		CommandToken: 0x1201,
		CommandKind:  CommandKindThresholdSet,
		Argument:     42,
		ExpiresInSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 24 {
		t.Fatalf("compact command must fit total payload cap, got %d", len(raw))
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	body, err := DecodeCompactCommand(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.CommandToken != 0x1201 || body.Argument != 42 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestPendingDigestRoundTrip(t *testing.T) {
	raw, err := EncodePendingDigest(201, true, PendingDigestBody{
		PendingCount: 2,
		Flags:        PendingFlagUrgent | PendingFlagExpiresSoon,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !packet.Summary() {
		t.Fatal("expected summary flag")
	}
	body, err := DecodePendingDigest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.PendingCount != 2 || body.Flags != (PendingFlagUrgent|PendingFlagExpiresSoon) {
		t.Fatalf("unexpected digest: %+v", body)
	}
}

func TestEventRoundTrip(t *testing.T) {
	raw, err := EncodeEvent(201, false, EventBody{
		EventCode:   EventCodeMotionDetected,
		Severity:    EventSeverityCritical,
		ValueBucket: 9,
		Flags:       EventFlagEventWake | EventFlagLatched,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if packet.LogicalType != TypeEvent || packet.SourceShortID != 201 {
		t.Fatalf("unexpected event header: %+v", packet)
	}
	body, err := DecodeEvent(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.EventCode != EventCodeMotionDetected || body.Severity != EventSeverityCritical ||
		body.ValueBucket != 9 || body.Flags != (EventFlagEventWake|EventFlagLatched) {
		t.Fatalf("unexpected event body: %+v", body)
	}
}

func TestHeartbeatRoundTrip(t *testing.T) {
	raw, err := EncodeHeartbeat(201, true, HeartbeatBody{
		Health:        HeartbeatHealthDegraded,
		BatteryBucket: 81,
		LinkQuality:   42,
		UptimeBucket:  7,
		Flags:         HeartbeatFlagLowPower | HeartbeatFlagMaintenanceAwake,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if packet.LogicalType != TypeHeartbeat || !packet.Summary() {
		t.Fatalf("unexpected heartbeat header: %+v", packet)
	}
	body, err := DecodeHeartbeat(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.Health != HeartbeatHealthDegraded || body.BatteryBucket != 81 ||
		body.LinkQuality != 42 || body.UptimeBucket != 7 ||
		body.Flags != (HeartbeatFlagLowPower|HeartbeatFlagMaintenanceAwake) {
		t.Fatalf("unexpected heartbeat body: %+v", body)
	}
}
