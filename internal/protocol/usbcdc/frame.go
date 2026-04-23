package usbcdc

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

var magic = []byte("EF")

const Version byte = 1

func EncodeFrame(frameType byte, payload []byte) ([]byte, error) {
	if len(payload) > 0xFFFF {
		return nil, errors.New("payload too large for USB CDC frame")
	}
	header := make([]byte, 6)
	copy(header[:2], magic)
	header[2] = Version
	header[3] = frameType
	binary.LittleEndian.PutUint16(header[4:6], uint16(len(payload)))
	crc := crc32.ChecksumIEEE(append(header, payload...))
	frame := make([]byte, 0, len(header)+len(payload)+4)
	frame = append(frame, header...)
	frame = append(frame, payload...)
	crcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBytes, crc)
	frame = append(frame, crcBytes...)
	return frame, nil
}

func DecodeFrame(frame []byte) (byte, []byte, error) {
	if len(frame) < 10 {
		return 0, nil, errors.New("frame too short")
	}
	if string(frame[:2]) != string(magic) {
		return 0, nil, errors.New("invalid frame magic")
	}
	if frame[2] != Version {
		return 0, nil, errors.New("unsupported frame version")
	}
	length := int(binary.LittleEndian.Uint16(frame[4:6]))
	if len(frame) != 10+length {
		return 0, nil, errors.New("frame length does not match payload length")
	}
	expectedCRC := binary.LittleEndian.Uint32(frame[len(frame)-4:])
	if crc32.ChecksumIEEE(frame[:len(frame)-4]) != expectedCRC {
		return 0, nil, errors.New("payload CRC mismatch")
	}
	payload := make([]byte, length)
	copy(payload, frame[6:6+length])
	return frame[3], payload, nil
}
