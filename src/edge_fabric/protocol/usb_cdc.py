from __future__ import annotations

import zlib


MAGIC = b"EF"
VERSION = 1


def encode_frame(frame_type: int, payload: bytes) -> bytes:
    if not 0 <= frame_type <= 255:
        raise ValueError("frame_type must fit in one byte")
    if len(payload) > 65535:
        raise ValueError("payload too large for USB CDC frame")
    header = MAGIC + VERSION.to_bytes(1, "little") + frame_type.to_bytes(1, "little")
    length = len(payload).to_bytes(2, "little")
    crc = zlib.crc32(header + length + payload).to_bytes(4, "little")
    return header + length + payload + crc


def decode_frame(frame: bytes) -> tuple[int, bytes]:
    if len(frame) < 10:
        raise ValueError("frame too short")
    if frame[:2] != MAGIC:
        raise ValueError("invalid frame magic")
    version = frame[2]
    if version != VERSION:
        raise ValueError(f"unsupported frame version: {version}")
    frame_type = frame[3]
    length = int.from_bytes(frame[4:6], "little")
    if len(frame) != 10 + length:
        raise ValueError("frame length does not match payload length")
    payload = frame[6 : 6 + length]
    crc = int.from_bytes(frame[6 + length : 10 + length], "little")
    if zlib.crc32(frame[: 6 + length]) != crc:
        raise ValueError("payload CRC mismatch")
    return frame_type, payload
