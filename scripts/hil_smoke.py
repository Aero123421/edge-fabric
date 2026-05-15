#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import unittest
import zlib
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable


MAGIC = b"EF"
USB_VERSION = 1
FRAME_ENVELOPE_JSON = 1
FRAME_HEARTBEAT_JSON = 2
FRAME_COMPACT_BINARY = 3
FRAME_SUMMARY_BINARY = 4
FRAME_GATEWAY_ACK_JSON = 5

ONAIR_VERSION = 1
ONAIR_TYPE_COMPACT_COMMAND = 6
COMMAND_KIND_MAINTENANCE_ON = 1


class HilSkip(RuntimeError):
    pass


@dataclass
class HilConfig:
    gateway_port: str | None = None
    leaf_log_port: str | None = None
    relay_log_port: str | None = None
    baud: int = 115200
    read_timeout_s: float = 0.1
    heartbeat_timeout_s: float = 15.0
    ack_timeout_s: float = 8.0
    dtr_settle_s: float = 0.25
    dtr_backpressure_window_s: float = 6.0


@dataclass
class UsbFrame:
    frame_type: int
    payload: bytes


def encode_usb_frame(frame_type: int, payload: bytes) -> bytes:
    if not 0 <= frame_type <= 0xFF:
        raise ValueError("frame_type must fit in one byte")
    if len(payload) > 0xFFFF:
        raise ValueError("payload too large")
    header = MAGIC + bytes([USB_VERSION, frame_type]) + len(payload).to_bytes(2, "little")
    crc = zlib.crc32(header + payload).to_bytes(4, "little")
    return header + payload + crc


def decode_usb_frame(frame: bytes) -> UsbFrame:
    if len(frame) < 10:
        raise ValueError("frame too short")
    if frame[:2] != MAGIC:
        raise ValueError("invalid magic")
    if frame[2] != USB_VERSION:
        raise ValueError("unsupported version")
    payload_len = int.from_bytes(frame[4:6], "little")
    if len(frame) != 10 + payload_len:
        raise ValueError("length mismatch")
    expected_crc = int.from_bytes(frame[6 + payload_len : 10 + payload_len], "little")
    if zlib.crc32(frame[: 6 + payload_len]) != expected_crc:
        raise ValueError("crc mismatch")
    return UsbFrame(frame[3], frame[6 : 6 + payload_len])


def iter_usb_frames_from_buffer(buffer: bytearray) -> Iterable[UsbFrame]:
    while True:
        magic_at = buffer.find(MAGIC)
        if magic_at < 0:
            del buffer[:]
            return
        if magic_at > 0:
            del buffer[:magic_at]
        if len(buffer) < 6:
            return
        payload_len = int.from_bytes(buffer[4:6], "little")
        total_len = 10 + payload_len
        if len(buffer) < total_len:
            return
        candidate = bytes(buffer[:total_len])
        del buffer[:total_len]
        try:
            yield decode_usb_frame(candidate)
        except ValueError:
            del buffer[:1]


def build_compact_command(target_short_id: int = 0x7001, sequence: int = 1) -> bytes:
    command_token = 0x1234
    header = bytes([ONAIR_VERSION, ONAIR_TYPE_COMPACT_COMMAND, 0, sequence & 0xFF])
    header += (0).to_bytes(2, "little")
    header += target_short_id.to_bytes(2, "little")
    body = command_token.to_bytes(2, "little")
    body += bytes([COMMAND_KIND_MAINTENANCE_ON, 0, 30])
    return header + body


def parse_json_payload(frame: UsbFrame) -> dict:
    return json.loads(frame.payload.decode("utf-8"))


def load_config(path: str | None) -> HilConfig:
    data: dict = {}
    if path:
        with Path(path).open("r", encoding="utf-8") as handle:
            data = json.load(handle)
    return HilConfig(
        gateway_port=data.get("gateway_port") or os.getenv("EDGE_FABRIC_HIL_GATEWAY_PORT"),
        leaf_log_port=data.get("leaf_log_port") or os.getenv("EDGE_FABRIC_HIL_LEAF_LOG_PORT"),
        relay_log_port=data.get("relay_log_port") or os.getenv("EDGE_FABRIC_HIL_RELAY_LOG_PORT"),
        baud=int(data.get("baud") or os.getenv("EDGE_FABRIC_HIL_BAUD", "115200")),
        heartbeat_timeout_s=float(data.get("heartbeat_timeout_s", 15.0)),
        ack_timeout_s=float(data.get("ack_timeout_s", 8.0)),
        dtr_settle_s=float(data.get("dtr_settle_s", 0.25)),
        dtr_backpressure_window_s=float(data.get("dtr_backpressure_window_s", 6.0)),
    )


def require_hil_enabled(force: bool) -> None:
    enabled = os.getenv("EDGE_FABRIC_HIL", "").lower() in {"1", "true", "yes", "on"}
    if not enabled and not force:
        raise HilSkip("EDGE_FABRIC_HIL is not enabled")


def import_serial_module():
    try:
        import serial  # type: ignore
    except ImportError as exc:
        raise RuntimeError("pyserial is required for HIL runs: python -m pip install pyserial") from exc
    return serial


def open_serial(port: str, cfg: HilConfig):
    serial = import_serial_module()
    return serial.Serial(port=port, baudrate=cfg.baud, timeout=cfg.read_timeout_s, write_timeout=1.0)


def wait_for_usb_frame(
    serial_port,
    wanted_types: set[int],
    timeout_s: float,
    predicate: Callable[[UsbFrame], bool] | None = None,
) -> UsbFrame:
    deadline = time.monotonic() + timeout_s
    buffer = bytearray()
    while time.monotonic() < deadline:
        chunk = serial_port.read(256)
        if chunk:
            buffer.extend(chunk)
            for frame in iter_usb_frames_from_buffer(buffer):
                if frame.frame_type in wanted_types and (predicate is None or predicate(frame)):
                    return frame
    raise TimeoutError(f"timed out waiting for USB frame types {sorted(wanted_types)}")


def gateway_ack_matches(frame: UsbFrame, *, require_radio_sent: bool) -> bool:
    status = parse_json_payload(frame).get("status")
    if require_radio_sent:
        return status == "radio_sent"
    return status in {"gateway_accepted", "radio_sent"}


def wait_for_log_pattern(serial_port, patterns: tuple[str, ...], timeout_s: float) -> str:
    deadline = time.monotonic() + timeout_s
    buffer = ""
    while time.monotonic() < deadline:
        chunk = serial_port.read(256)
        if chunk:
            buffer += chunk.decode("utf-8", errors="replace")
            for pattern in patterns:
                if pattern in buffer:
                    return pattern
    raise TimeoutError(f"timed out waiting for log pattern: {patterns}")


def run_gateway_smoke(cfg: HilConfig, *, require_radio_sent: bool = False) -> list[str]:
    if not cfg.gateway_port:
        raise RuntimeError("gateway_port is required for gateway-smoke")
    checks: list[str] = []
    with open_serial(cfg.gateway_port, cfg) as gateway:
        gateway.dtr = True
        time.sleep(cfg.dtr_settle_s)
        gateway.reset_input_buffer()

        heartbeat = wait_for_usb_frame(gateway, {FRAME_HEARTBEAT_JSON}, cfg.heartbeat_timeout_s)
        heartbeat_payload = parse_json_payload(heartbeat)
        if heartbeat_payload.get("subject_kind") != "gateway":
            raise AssertionError(f"unexpected heartbeat payload: {heartbeat_payload}")
        checks.append(f"heartbeat status={heartbeat_payload.get('status')} dtr={heartbeat_payload.get('usb_dtr')}")

        before_backpressure = int(heartbeat_payload.get("usb_tx_backpressure", 0))
        gateway.dtr = False
        time.sleep(cfg.dtr_backpressure_window_s)
        gateway.dtr = True
        time.sleep(cfg.dtr_settle_s)
        heartbeat = wait_for_usb_frame(gateway, {FRAME_HEARTBEAT_JSON}, cfg.heartbeat_timeout_s)
        after_payload = parse_json_payload(heartbeat)
        after_backpressure = int(after_payload.get("usb_tx_backpressure", 0))
        if after_payload.get("usb_dtr") != "true":
            raise AssertionError(f"DTR did not recover to true in heartbeat: {after_payload}")
        if after_backpressure < before_backpressure:
            raise AssertionError("usb_tx_backpressure counter went backwards")
        checks.append(f"dtr recovery backpressure={after_backpressure}")

        command = build_compact_command()
        gateway.write(encode_usb_frame(FRAME_COMPACT_BINARY, command))
        ack = wait_for_usb_frame(
            gateway,
            {FRAME_GATEWAY_ACK_JSON},
            cfg.ack_timeout_s,
            lambda frame: gateway_ack_matches(frame, require_radio_sent=require_radio_sent),
        )
        ack_payload = parse_json_payload(ack)
        checks.append(f"gateway ack status={ack_payload.get('status')}")
    return checks


def run_gateway_node_smoke(cfg: HilConfig) -> list[str]:
    checks = run_gateway_smoke(cfg, require_radio_sent=True)
    if not cfg.leaf_log_port:
        checks.append("leaf log check skipped: leaf_log_port not configured")
        return checks
    with open_serial(cfg.leaf_log_port, cfg) as leaf:
        leaf.dtr = True
        pattern = wait_for_log_pattern(
            leaf,
            ("entering deep sleep", "entering light sleep", "cycle complete"),
            cfg.heartbeat_timeout_s,
        )
        checks.append(f"leaf log observed: {pattern}")
    return checks


def run_gateway_relay_node_smoke(cfg: HilConfig) -> list[str]:
    checks = run_gateway_node_smoke(cfg)
    if not cfg.relay_log_port:
        checks.append("relay log check skipped: relay_log_port not configured")
        return checks
    with open_serial(cfg.relay_log_port, cfg) as relay:
        relay.dtr = True
        pattern = wait_for_log_pattern(relay, ("relay", "lora", "forward"), cfg.heartbeat_timeout_s)
        checks.append(f"relay log observed: {pattern}")
    return checks


def run_suite(name: str, cfg: HilConfig) -> list[str]:
    if name == "gateway-smoke":
        return run_gateway_smoke(cfg)
    if name == "gateway-node-smoke":
        return run_gateway_node_smoke(cfg)
    if name == "gateway-relay-node":
        return run_gateway_relay_node_smoke(cfg)
    raise ValueError(f"unknown suite: {name}")


class HilSmokeSelfTest(unittest.TestCase):
    def test_usb_frame_round_trip(self) -> None:
        frame = encode_usb_frame(FRAME_GATEWAY_ACK_JSON, b'{"status":"radio_sent"}')
        decoded = decode_usb_frame(frame)
        self.assertEqual(decoded.frame_type, FRAME_GATEWAY_ACK_JSON)
        self.assertEqual(parse_json_payload(decoded)["status"], "radio_sent")

    def test_stream_parser_skips_noise(self) -> None:
        encoded = encode_usb_frame(FRAME_HEARTBEAT_JSON, b'{"subject_kind":"gateway"}')
        buffer = bytearray(b"noise" + encoded)
        frames = list(iter_usb_frames_from_buffer(buffer))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].frame_type, FRAME_HEARTBEAT_JSON)

    def test_compact_command_shape(self) -> None:
        command = build_compact_command()
        self.assertEqual(command[0], ONAIR_VERSION)
        self.assertEqual(command[1], ONAIR_TYPE_COMPACT_COMMAND)
        self.assertEqual(len(command), 13)

    def test_radio_path_suite_requires_radio_sent_ack(self) -> None:
        accepted = UsbFrame(FRAME_GATEWAY_ACK_JSON, b'{"status":"gateway_accepted"}')
        sent = UsbFrame(FRAME_GATEWAY_ACK_JSON, b'{"status":"radio_sent"}')
        self.assertTrue(gateway_ack_matches(accepted, require_radio_sent=False))
        self.assertFalse(gateway_ack_matches(accepted, require_radio_sent=True))
        self.assertTrue(gateway_ack_matches(sent, require_radio_sent=True))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Edge Fabric HIL smoke harness")
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("list", help="list available HIL suites")
    subparsers.add_parser("self-test", help="run host-side harness unit tests")

    run_parser = subparsers.add_parser("run", help="run a gated HIL suite")
    run_parser.add_argument("--suite", required=True, choices=["gateway-smoke", "gateway-node-smoke", "gateway-relay-node"])
    run_parser.add_argument("--config", help="JSON config file")
    run_parser.add_argument("--force", action="store_true", help="run without EDGE_FABRIC_HIL=1")

    args = parser.parse_args(argv)
    if args.command == "list":
        print("gateway-smoke")
        print("gateway-node-smoke")
        print("gateway-relay-node")
        return 0
    if args.command == "self-test":
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(HilSmokeSelfTest)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
        return 0 if result.wasSuccessful() else 1

    cfg = load_config(args.config)
    try:
        require_hil_enabled(args.force)
        checks = run_suite(args.suite, cfg)
    except HilSkip as exc:
        print(f"SKIP: {exc}")
        return 0
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 1
    for check in checks:
        print(f"PASS: {check}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
