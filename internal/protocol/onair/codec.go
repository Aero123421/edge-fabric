package onair

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Version byte = 1

	FlagSummary byte = 1 << 0

	TypeState         byte = 1
	TypeEvent         byte = 2
	TypeCommandResult byte = 3
	TypePendingDigest byte = 4
	TypeTinyPoll      byte = 5
	TypeCompactCommand byte = 6
	TypeHeartbeat     byte = 7

	StateKeyNodePower byte = 1

	StateValueUnknown byte = 0
	StateValueAwake   byte = 1
	StateValueSleep   byte = 2

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
)

const HeaderSize = 8

type Packet struct {
	Version       byte
	LogicalType   byte
	Flags         byte
	Sequence      byte
	SourceShortID uint16
	TargetShortID uint16
	Body          []byte
}

type StateBody struct {
	KeyToken  byte
	ValueToken byte
	EventWake bool
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
	out := make([]byte, HeaderSize+len(packet.Body))
	out[0] = packet.Version
	out[1] = packet.LogicalType
	out[2] = packet.Flags
	out[3] = packet.Sequence
	binary.LittleEndian.PutUint16(out[4:6], packet.SourceShortID)
	binary.LittleEndian.PutUint16(out[6:8], packet.TargetShortID)
	copy(out[8:], packet.Body)
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
		Body:          append([]byte(nil), frame[8:]...),
	}
	if packet.Version != Version {
		return nil, fmt.Errorf("unsupported on-air version: %d", packet.Version)
	}
	return packet, nil
}

func (p *Packet) Summary() bool {
	return p != nil && (p.Flags&FlagSummary) != 0
}

func EncodeState(sourceShortID uint16, summary bool, body StateBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypeState,
		Flags:         flags,
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

func EncodeCommandResult(sourceShortID uint16, summary bool, body CommandResultBody) ([]byte, error) {
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

func EncodePendingDigest(sourceShortID uint16, summary bool, body PendingDigestBody) ([]byte, error) {
	flags := byte(0)
	if summary {
		flags |= FlagSummary
	}
	return Encode(Packet{
		LogicalType:   TypePendingDigest,
		Flags:         flags,
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

func EncodeTinyPoll(sourceShortID uint16, body TinyPollBody) ([]byte, error) {
	return Encode(Packet{
		LogicalType:   TypeTinyPoll,
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

func EncodeCompactCommand(targetShortID uint16, summary bool, body CompactCommandBody) ([]byte, error) {
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

func boolToByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
