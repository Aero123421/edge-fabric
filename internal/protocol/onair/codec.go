package onair

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Version byte = 1

	FlagSummary  byte = 1 << 0
	FlagRelayExt byte = 1 << 1

	TypeState          byte = 1
	TypeEvent          byte = 2
	TypeCommandResult  byte = 3
	TypePendingDigest  byte = 4
	TypeTinyPoll       byte = 5
	TypeCompactCommand byte = 6
	TypeHeartbeat      byte = 7

	StateKeyNodePower byte = 1

	StateValueUnknown byte = 0
	StateValueAwake   byte = 1
	StateValueSleep   byte = 2

	EventCodeBatteryLow       byte = 1
	EventCodeMotionDetected   byte = 2
	EventCodeLeakDetected     byte = 3
	EventCodeTamper           byte = 4
	EventCodeThresholdCrossed byte = 5

	EventSeverityInfo     byte = 1
	EventSeverityWarning  byte = 2
	EventSeverityCritical byte = 3

	EventFlagEventWake byte = 1 << 0
	EventFlagLatched   byte = 1 << 1

	CommandKindMaintenanceOn  byte = 1
	CommandKindMaintenanceOff byte = 2
	CommandKindThresholdSet   byte = 3
	CommandKindQuietSet       byte = 4
	CommandKindAlarmClear     byte = 5
	CommandKindSamplingSet    byte = 6

	PhaseAccepted  byte = 1
	PhaseExecuting byte = 2
	PhaseSucceeded byte = 3
	PhaseFailed    byte = 4
	PhaseRejected  byte = 5
	PhaseExpired   byte = 6

	ReasonOK          byte = 1
	ReasonService     byte = 2
	ReasonMaintenance byte = 3
	ReasonStale       byte = 4
	ReasonBadCommand  byte = 5
	ReasonUnsupported byte = 6

	ServiceLevelEventualNextPoll byte = 1

	PendingFlagUrgent      byte = 1 << 0
	PendingFlagExpiresSoon byte = 1 << 1

	HeartbeatHealthOK       byte = 1
	HeartbeatHealthDegraded byte = 2
	HeartbeatHealthCritical byte = 3

	HeartbeatFlagEventWake        byte = 1 << 0
	HeartbeatFlagMaintenanceAwake byte = 1 << 1
	HeartbeatFlagLowPower         byte = 1 << 2
)

const HeaderSize = 8
const RelayExtensionSize = 7

type Packet struct {
	Version       byte
	LogicalType   byte
	Flags         byte
	Sequence      byte
	SourceShortID uint16
	TargetShortID uint16
	Relay         *RelayExtension
	Body          []byte
}

type RelayExtension struct {
	OriginShortID      uint16
	PreviousHopShortID uint16
	TTL                byte
	HopCount           byte
	RouteHint          byte
}

type StateBody struct {
	KeyToken   byte
	ValueToken byte
	EventWake  bool
}

type EventBody struct {
	EventCode   byte
	Severity    byte
	ValueBucket byte
	Flags       byte
}

type CommandResultBody struct {
	CommandToken uint16
	PhaseToken   byte
	ReasonToken  byte
}

type PendingDigestBody struct {
	PendingCount uint8
	Flags        byte
}

type TinyPollBody struct {
	ServiceLevel byte
}

type CompactCommandBody struct {
	CommandToken uint16
	CommandKind  byte
	Argument     byte
	ExpiresInSec byte
}

type HeartbeatBody struct {
	Health        byte
	BatteryBucket byte
	LinkQuality   byte
	UptimeBucket  byte
	Flags         byte
}

func Encode(packet Packet) ([]byte, error) {
	if packet.Version == 0 {
		packet.Version = Version
	}
	if packet.Version != Version {
		return nil, fmt.Errorf("unsupported on-air version: %d", packet.Version)
	}
	if len(packet.Body) == 0 {
		return nil, errors.New("on-air body is required")
	}
	if packet.Relay == nil && packet.Flags&FlagRelayExt != 0 {
		return nil, errors.New("relay extension flag requires relay metadata")
	}
	relaySize := 0
	if packet.Relay != nil {
		if packet.Relay.TTL == 0 {
			return nil, errors.New("relay extension ttl exhausted")
		}
		packet.Flags |= FlagRelayExt
		relaySize = RelayExtensionSize
	}
	out := make([]byte, HeaderSize+relaySize+len(packet.Body))
	out[0] = packet.Version
	out[1] = packet.LogicalType
	out[2] = packet.Flags
	out[3] = packet.Sequence
	binary.LittleEndian.PutUint16(out[4:6], packet.SourceShortID)
	binary.LittleEndian.PutUint16(out[6:8], packet.TargetShortID)
	bodyOffset := HeaderSize
	if packet.Relay != nil {
		binary.LittleEndian.PutUint16(out[8:10], packet.Relay.OriginShortID)
		binary.LittleEndian.PutUint16(out[10:12], packet.Relay.PreviousHopShortID)
		out[12] = packet.Relay.TTL
		out[13] = packet.Relay.HopCount
		out[14] = packet.Relay.RouteHint
		bodyOffset += RelayExtensionSize
	}
	copy(out[bodyOffset:], packet.Body)
	return out, nil
}

func Decode(frame []byte) (*Packet, error) {
	if len(frame) < HeaderSize+1 {
		return nil, errors.New("on-air frame too short")
	}
	packet := &Packet{
		Version:       frame[0],
		LogicalType:   frame[1],
		Flags:         frame[2],
		Sequence:      frame[3],
		SourceShortID: binary.LittleEndian.Uint16(frame[4:6]),
		TargetShortID: binary.LittleEndian.Uint16(frame[6:8]),
	}
	if packet.Version != Version {
		return nil, fmt.Errorf("unsupported on-air version: %d", packet.Version)
	}
	bodyOffset := HeaderSize
	if packet.RelayExtension() {
		if len(frame) < HeaderSize+RelayExtensionSize+1 {
			return nil, errors.New("on-air relay extension frame too short")
		}
		packet.Relay = &RelayExtension{
			OriginShortID:      binary.LittleEndian.Uint16(frame[8:10]),
			PreviousHopShortID: binary.LittleEndian.Uint16(frame[10:12]),
			TTL:                frame[12],
			HopCount:           frame[13],
			RouteHint:          frame[14],
		}
		bodyOffset += RelayExtensionSize
	}
	packet.Body = append([]byte(nil), frame[bodyOffset:]...)
	return packet, nil
}

func (p *Packet) Summary() bool {
	return p != nil && (p.Flags&FlagSummary) != 0
}

func (p *Packet) RelayExtension() bool {
	return p != nil && (p.Flags&FlagRelayExt) != 0
}

func BuildRelayForward(packet *Packet, relayShortID uint16) (*Packet, error) {
	if packet == nil {
		return nil, errors.New("relay forward requires packet")
	}
	if relayShortID == 0 {
		return nil, errors.New("relay forward requires relay short id")
	}
	originShortID := packet.SourceShortID
	ttl := byte(2)
	hopCount := byte(0)
	routeHint := byte(0)
	if packet.Relay != nil {
		originShortID = packet.Relay.OriginShortID
		ttl = packet.Relay.TTL
		hopCount = packet.Relay.HopCount
		routeHint = packet.Relay.RouteHint
	}
	if originShortID == 0 {
		return nil, errors.New("relay forward requires origin short id")
	}
	if ttl <= 1 {
		return nil, errors.New("relay ttl exhausted")
	}
	forwarded := *packet
	forwarded.Flags |= FlagRelayExt
	forwarded.SourceShortID = relayShortID
	forwarded.Body = append([]byte(nil), packet.Body...)
	forwarded.Relay = &RelayExtension{
		OriginShortID:      originShortID,
		PreviousHopShortID: relayShortID,
		TTL:                ttl - 1,
		HopCount:           hopCount + 1,
		RouteHint:          routeHint,
	}
	return &forwarded, nil
}

func EncodeState(sourceShortID uint16, summary bool, sequence byte, body StateBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypeState,
		Flags:         flags,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body: []byte{
			body.KeyToken,
			body.ValueToken,
			boolToByte(body.EventWake),
		},
	})
}

func DecodeState(packet *Packet) (*StateBody, error) {
	if packet == nil || packet.LogicalType != TypeState || len(packet.Body) != 3 {
		return nil, errors.New("invalid state frame")
	}
	return &StateBody{
		KeyToken:   packet.Body[0],
		ValueToken: packet.Body[1],
		EventWake:  packet.Body[2] != 0,
	}, nil
}

func EncodeEvent(sourceShortID uint16, summary bool, sequence byte, body EventBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypeEvent,
		Flags:         flags,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body: []byte{
			body.EventCode,
			body.Severity,
			body.ValueBucket,
			body.Flags,
		},
	})
}

func DecodeEvent(packet *Packet) (*EventBody, error) {
	if packet == nil || packet.LogicalType != TypeEvent || len(packet.Body) != 4 {
		return nil, errors.New("invalid event frame")
	}
	return &EventBody{
		EventCode:   packet.Body[0],
		Severity:    packet.Body[1],
		ValueBucket: packet.Body[2],
		Flags:       packet.Body[3],
	}, nil
}

func EncodeCommandResult(sourceShortID uint16, summary bool, sequence byte, body CommandResultBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], body.CommandToken)
	payload[2] = body.PhaseToken
	payload[3] = body.ReasonToken
	return Encode(Packet{
		LogicalType:   TypeCommandResult,
		Flags:         flags,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body:          payload,
	})
}

func DecodeCommandResult(packet *Packet) (*CommandResultBody, error) {
	if packet == nil || packet.LogicalType != TypeCommandResult || len(packet.Body) != 4 {
		return nil, errors.New("invalid command_result frame")
	}
	return &CommandResultBody{
		CommandToken: binary.LittleEndian.Uint16(packet.Body[0:2]),
		PhaseToken:   packet.Body[2],
		ReasonToken:  packet.Body[3],
	}, nil
}

func EncodePendingDigest(sourceShortID uint16, summary bool, sequence byte, body PendingDigestBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypePendingDigest,
		Flags:         flags,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body: []byte{
			byte(body.PendingCount),
			body.Flags,
		},
	})
}

func DecodePendingDigest(packet *Packet) (*PendingDigestBody, error) {
	if packet == nil || packet.LogicalType != TypePendingDigest || len(packet.Body) != 2 {
		return nil, errors.New("invalid pending_digest frame")
	}
	return &PendingDigestBody{
		PendingCount: uint8(packet.Body[0]),
		Flags:        packet.Body[1],
	}, nil
}

func EncodeTinyPoll(sourceShortID uint16, sequence byte, body TinyPollBody) ([]byte, error) {
	return Encode(Packet{
		LogicalType:   TypeTinyPoll,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body:          []byte{body.ServiceLevel},
	})
}

func DecodeTinyPoll(packet *Packet) (*TinyPollBody, error) {
	if packet == nil || packet.LogicalType != TypeTinyPoll || len(packet.Body) != 1 {
		return nil, errors.New("invalid tiny_poll frame")
	}
	return &TinyPollBody{ServiceLevel: packet.Body[0]}, nil
}

func EncodeCompactCommand(targetShortID uint16, summary bool, sequence byte, body CompactCommandBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	payload := make([]byte, 5)
	binary.LittleEndian.PutUint16(payload[0:2], body.CommandToken)
	payload[2] = body.CommandKind
	payload[3] = body.Argument
	payload[4] = body.ExpiresInSec
	return Encode(Packet{
		LogicalType:   TypeCompactCommand,
		Flags:         flags,
		Sequence:      sequence,
		TargetShortID: targetShortID,
		Body:          payload,
	})
}

func DecodeCompactCommand(packet *Packet) (*CompactCommandBody, error) {
	if packet == nil || packet.LogicalType != TypeCompactCommand || len(packet.Body) != 5 {
		return nil, errors.New("invalid compact_command frame")
	}
	return &CompactCommandBody{
		CommandToken: binary.LittleEndian.Uint16(packet.Body[0:2]),
		CommandKind:  packet.Body[2],
		Argument:     packet.Body[3],
		ExpiresInSec: packet.Body[4],
	}, nil
}

func EncodeHeartbeat(sourceShortID uint16, summary bool, sequence byte, body HeartbeatBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypeHeartbeat,
		Flags:         flags,
		Sequence:      sequence,
		SourceShortID: sourceShortID,
		Body: []byte{
			body.Health,
			body.BatteryBucket,
			body.LinkQuality,
			body.UptimeBucket,
			body.Flags,
		},
	})
}

func DecodeHeartbeat(packet *Packet) (*HeartbeatBody, error) {
	if packet == nil || packet.LogicalType != TypeHeartbeat || len(packet.Body) != 5 {
		return nil, errors.New("invalid heartbeat frame")
	}
	return &HeartbeatBody{
		Health:        packet.Body[0],
		BatteryBucket: packet.Body[1],
		LinkQuality:   packet.Body[2],
		UptimeBucket:  packet.Body[3],
		Flags:         packet.Body[4],
	}, nil
}

func boolToByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
