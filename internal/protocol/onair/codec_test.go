package onair

import "testing"

func TestStateRoundTrip(t *testing.T) {
	raw, err := EncodeState(201, false, 7, StateBody{
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
	if packet.SourceShortID != 201 || packet.Sequence != 7 || packet.Summary() {
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
	raw, err := EncodeCompactCommand(201, false, 8, CompactCommandBody{
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
	raw, err := EncodePendingDigest(201, true, 9, PendingDigestBody{
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
	if packet.Sequence != 9 || !packet.Summary() {
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
	raw, err := EncodeEvent(201, false, 10, EventBody{
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
	if packet.LogicalType != TypeEvent || packet.SourceShortID != 201 || packet.Sequence != 10 {
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
	raw, err := EncodeHeartbeat(201, true, 11, HeartbeatBody{
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
	if packet.LogicalType != TypeHeartbeat || packet.Sequence != 11 || !packet.Summary() {
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

func TestRelayExtensionRoundTrip(t *testing.T) {
	raw, err := Encode(Packet{
		LogicalType:   TypeEvent,
		Sequence:      42,
		SourceShortID: 302,
		TargetShortID: 1,
		Relay: &RelayExtension{
			OriginShortID:      201,
			PreviousHopShortID: 302,
			TTL:                2,
			HopCount:           1,
			RouteHint:          7,
		},
		Body: []byte{EventCodeMotionDetected, EventSeverityCritical, 4, EventFlagEventWake},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != HeaderSize+RelayExtensionSize+4 {
		t.Fatalf("unexpected relay frame size %d", len(raw))
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !packet.RelayExtension() || packet.Relay == nil {
		t.Fatalf("expected relay extension, got %+v", packet)
	}
	if packet.SourceShortID != 302 || packet.Relay.OriginShortID != 201 ||
		packet.Relay.PreviousHopShortID != 302 || packet.Relay.TTL != 2 ||
		packet.Relay.HopCount != 1 || packet.Relay.RouteHint != 7 {
		t.Fatalf("unexpected relay extension: %+v", packet.Relay)
	}
	body, err := DecodeEvent(packet)
	if err != nil {
		t.Fatal(err)
	}
	if body.EventCode != EventCodeMotionDetected || body.Severity != EventSeverityCritical {
		t.Fatalf("unexpected relayed event body: %+v", body)
	}
}

func TestBuildRelayForwardUpdatesHopState(t *testing.T) {
	raw, err := EncodeEvent(201, false, 77, EventBody{
		EventCode:   EventCodeLeakDetected,
		Severity:    EventSeverityCritical,
		ValueBucket: 2,
		Flags:       EventFlagEventWake,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	packet.Relay = &RelayExtension{OriginShortID: 201, TTL: 2, RouteHint: 5}
	forwarded, err := BuildRelayForward(packet, 302)
	if err != nil {
		t.Fatal(err)
	}
	if forwarded.SourceShortID != 302 || forwarded.Relay == nil ||
		forwarded.Relay.OriginShortID != 201 || forwarded.Relay.PreviousHopShortID != 302 ||
		forwarded.Relay.TTL != 1 || forwarded.Relay.HopCount != 1 || forwarded.Relay.RouteHint != 5 {
		t.Fatalf("unexpected forwarded packet: %+v relay=%+v", forwarded, forwarded.Relay)
	}
	if _, err := BuildRelayForward(&Packet{SourceShortID: 201, Relay: &RelayExtension{OriginShortID: 201, TTL: 0}, Body: []byte{1}}, 302); err == nil {
		t.Fatal("expected ttl exhausted error")
	}
	if _, err := BuildRelayForward(&Packet{SourceShortID: 201, Relay: &RelayExtension{OriginShortID: 201, TTL: 1}, Body: []byte{1}}, 302); err == nil {
		t.Fatal("expected ttl=1 to be exhausted before another forward")
	}
}

func TestRelayExtensionRejectsExhaustedTTL(t *testing.T) {
	_, err := Encode(Packet{
		LogicalType:   TypeEvent,
		Sequence:      43,
		SourceShortID: 302,
		TargetShortID: 1,
		Relay: &RelayExtension{
			OriginShortID:      201,
			PreviousHopShortID: 302,
			TTL:                0,
			HopCount:           2,
			RouteHint:          7,
		},
		Body: []byte{EventCodeMotionDetected, EventSeverityCritical, 4, EventFlagEventWake},
	})
	if err == nil {
		t.Fatal("relay_extension_v1 must reject frames whose ttl is already exhausted before forwarding")
	}
}

func TestRelayExtensionFlagRequiresMetadata(t *testing.T) {
	_, err := Encode(Packet{
		LogicalType:   TypeEvent,
		Flags:         FlagRelayExt,
		Sequence:      44,
		SourceShortID: 201,
		TargetShortID: 1,
		Body:          []byte{EventCodeMotionDetected, EventSeverityCritical, 4, EventFlagEventWake},
	})
	if err == nil {
		t.Fatal("expected relay extension flag without relay metadata to fail encode")
	}
}
