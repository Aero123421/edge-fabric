from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from edge_fabric.host.agent import (
    USB_FRAME_COMPACT_BINARY,
    USB_FRAME_FABRIC_ENVELOPE_JSON,
    USB_FRAME_GATEWAY_HEARTBEAT_JSON,
    USB_FRAME_SUMMARY_BINARY,
    HostAgent,
)
from edge_fabric.host.site_router import SiteRouter
from edge_fabric.protocol.usb_cdc import decode_frame, encode_frame


class FailingSiteRouter:
    def ingest(self, *_args, **_kwargs):
        raise RuntimeError("router unavailable")


class HostAgentTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.temp_path = Path(self.temp_dir.name)
        self.router = SiteRouter(self.temp_path / "site-router.db")
        self.agent = HostAgent(self.router, spool_path=self.temp_path / "host-agent-spool.jsonl")

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_usb_frame_event_relays_into_site_router(self) -> None:
        envelope = {
            "schema_version": "1.0.0",
            "message_id": "msg-usb-001",
            "kind": "event",
            "priority": "critical",
            "event_id": "evt-usb-001",
            "source": {"hardware_id": "battery-01", "session_id": "sess-01", "seq_local": 3},
            "target": {"kind": "service", "value": "alerts"},
            "payload": {"alarm_code": "battery_low"},
        }
        frame = self.agent.encode_envelope_frame(envelope)

        result = self.agent.relay_usb_frame(
            ingress_id="gateway-usb-01",
            session_id="usb-session-01",
            frame=frame,
            metadata={"rssi": -111, "snr": 6.25, "hop_count": 0},
        )

        self.assertEqual(result.status, "persisted")
        self.assertEqual(self.router.count_events(), 1)

    def test_direct_ip_state_relays_into_projection(self) -> None:
        envelope = {
            "schema_version": "1.0.0",
            "message_id": "msg-wifi-001",
            "kind": "state",
            "priority": "normal",
            "source": {"hardware_id": "powered-01", "session_id": "sess-02", "seq_local": 9},
            "target": {"kind": "service", "value": "state"},
            "payload": {"state_key": "tank.level", "value": 77},
        }

        result = self.agent.relay_direct_ip(
            ingress_id="wifi-direct-01",
            session_id="wifi-session-01",
            envelope_dict=envelope,
            metadata={"rssi": -43},
        )

        self.assertEqual(result.status, "persisted")
        self.assertEqual(
            self.router.latest_state("powered-01", "tank.level"),
            {"state_key": "tank.level", "value": 77},
        )

    def test_router_failure_spools_and_flushes(self) -> None:
        failing_agent = HostAgent(
            FailingSiteRouter(), spool_path=self.temp_path / "failing-spool.jsonl"
        )
        envelope = {
            "schema_version": "1.0.0",
            "message_id": "msg-spool-001",
            "kind": "event",
            "priority": "critical",
            "event_id": "evt-spool-001",
            "source": {"hardware_id": "battery-02"},
            "target": {"kind": "service", "value": "alerts"},
            "payload": {"alarm_code": "water"},
        }

        result = failing_agent.relay_direct_ip(
            ingress_id="wifi-direct-fail",
            session_id="wifi-session-fail",
            envelope_dict=envelope,
        )

        self.assertEqual(result.status, "spooled")
        self.assertEqual(failing_agent.diagnostics()["spool_records"], 1)

        recovery_agent = HostAgent(
            self.router, spool_path=self.temp_path / "failing-spool.jsonl"
        )
        flushed = recovery_agent.flush_spool()
        self.assertEqual(flushed, 1)
        self.assertEqual(self.router.count_events(), 1)

    def test_heartbeat_frame_is_recorded_to_spool(self) -> None:
        frame = self.agent.encode_heartbeat_frame({"gateway_id": "gw-01", "live": True})
        frame_type, _ = decode_frame(frame)
        self.assertEqual(frame_type, USB_FRAME_GATEWAY_HEARTBEAT_JSON)

        result = self.agent.relay_usb_frame(
            ingress_id="gateway-usb-01",
            session_id="heartbeat-session-01",
            frame=frame,
        )

        self.assertEqual(result.status, "heartbeat_recorded")
        self.assertEqual(self.agent.diagnostics()["spool_records"], 0)
        self.assertEqual(self.agent.diagnostics()["last_heartbeat"]["payload"]["gateway_id"], "gw-01")

    def test_encode_envelope_frame_uses_expected_frame_type(self) -> None:
        frame = self.agent.encode_envelope_frame(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-frame-001",
                "kind": "event",
                "priority": "critical",
                "event_id": "evt-frame-001",
                "source": {"hardware_id": "battery-03"},
                "target": {"kind": "service", "value": "alerts"},
                "payload": {"alarm_code": "battery_low"},
            }
        )
        frame_type, _payload = decode_frame(frame)
        self.assertEqual(frame_type, USB_FRAME_FABRIC_ENVELOPE_JSON)

    def test_malformed_input_is_rejected_not_spooled(self) -> None:
        result = self.agent.relay_direct_ip(
            ingress_id="wifi-direct-02",
            session_id="wifi-session-02",
            envelope_dict={
                "schema_version": "1.0.0",
                "message_id": "msg-bad-command-result",
                "kind": "command_result",
                "priority": "control",
                "command_id": "cmd-bad-02",
                "source": {"hardware_id": "servo-03"},
                "target": {"kind": "client", "value": "controller-03"},
                "payload": {"phase": "bad_phase", "command_id": "cmd-bad-02"},
            },
        )
        self.assertEqual(result.status, "rejected")
        self.assertEqual(self.agent.diagnostics()["spool_records"], 0)
        self.assertEqual(self.agent.diagnostics()["reject_records"], 1)

    def test_flush_spool_skips_corrupt_line_and_keeps_recovering(self) -> None:
        self.agent.spool_path.write_text(
            '{"record_type":"envelope","ingress_id":"gw","envelope":{"schema_version":"1.0.0","message_id":"msg-ok","kind":"event","priority":"critical","event_id":"evt-ok","source":{"hardware_id":"sensor-ok"},"target":{"kind":"service","value":"alerts"},"payload":{"alarm_code":"ok"}}}\n'
            '{"broken":\n',
            encoding="utf-8",
        )
        flushed = self.agent.flush_spool()
        self.assertEqual(flushed, 1)
        self.assertEqual(self.router.count_events(), 1)
        self.assertEqual(self.agent.diagnostics()["reject_records"], 1)

    def test_compact_summary_command_result_relays_into_router(self) -> None:
        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-command-compact-init",
                "kind": "command",
                "priority": "control",
                "command_id": "cmd-compact-001",
                "source": {"hardware_id": "controller-compact"},
                "target": {"kind": "node", "value": "sleepy-leaf-01"},
                "payload": {"command_name": "mode.set", "mode": "eco"},
            },
            ingress_id="local",
        )
        frame = encode_frame(
            USB_FRAME_SUMMARY_BINARY,
            b"R|sleepy-leaf-01|cmd-compact-001|succeeded|ok",
        )

        result = self.agent.relay_usb_frame(
            ingress_id="gateway-usb-compact",
            session_id="compact-session-01",
            frame=frame,
        )

        self.assertEqual(result.status, "persisted")
        self.assertEqual(self.router.command_state("cmd-compact-001"), "succeeded")

    def test_compact_summary_decode_preserves_wire_shape(self) -> None:
        envelope, status = self.agent._decode_compact_summary_payload(
            USB_FRAME_COMPACT_BINARY,
            b"S|sleepy-leaf-01|node.power|awake|1",
        )

        self.assertEqual(status, "")
        assert envelope is not None
        self.assertEqual(envelope["payload"]["shape"], "state_compact_v1")
        self.assertEqual(envelope["payload"]["wire_shape"], "compact_v1")
        self.assertEqual(envelope["payload"]["codec_family"], "compact_binary_v1")

    def test_summary_event_relays_into_router(self) -> None:
        frame = encode_frame(
            USB_FRAME_SUMMARY_BINARY,
            b"E|sleepy-leaf-02|evt-compact-001|leak",
        )

        result = self.agent.relay_usb_frame(
            ingress_id="gateway-usb-summary",
            session_id="summary-session-01",
            frame=frame,
        )

        self.assertEqual(result.status, "persisted")
        self.assertEqual(self.router.count_events(), 1)

    def test_digest_and_poll_frames_are_recorded_only(self) -> None:
        for payload in (b"D|sleepy-leaf-01|1", b"P|sleepy-leaf-01|TP|ENP"):
            frame = encode_frame(USB_FRAME_SUMMARY_BINARY, payload)
            result = self.agent.relay_usb_frame(
                ingress_id="gateway-usb-summary",
                session_id="summary-session-02",
                frame=frame,
            )
            self.assertIn(result.status, {"digest_recorded", "poll_recorded"})
        self.assertEqual(self.router.count_events(), 0)

    def test_invalid_compact_payload_is_rejected(self) -> None:
        with self.assertRaises(ValueError):
            self.agent._decode_compact_summary_payload(
                USB_FRAME_COMPACT_BINARY,
                b"R|missing-fields",
            )

    def test_summary_command_result_uses_summary_metadata(self) -> None:
        envelope, status = self.agent._decode_compact_summary_payload(
            USB_FRAME_SUMMARY_BINARY,
            b"R|sleepy-leaf-01|cmd-sum-001|succeeded|ok",
        )

        self.assertEqual(status, "")
        assert envelope is not None
        self.assertEqual(envelope["payload"]["shape"], "command_result_summary_v1")
        self.assertEqual(envelope["payload"]["wire_shape"], "summary_v1")
        self.assertEqual(envelope["payload"]["codec_family"], "summary_binary_v1")


if __name__ == "__main__":
    unittest.main()
