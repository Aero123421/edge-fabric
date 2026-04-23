from __future__ import annotations

import json
import unittest
from pathlib import Path

from edge_fabric.contracts.enums import AckPhase
from edge_fabric.contracts.models import FabricEnvelope, NodeManifest, RoleLease
from edge_fabric.protocol.compact_codec import frame_type_spec, shape_for
from edge_fabric.protocol.jp import body_cap_for_profile
from edge_fabric.protocol.usb_cdc import decode_frame, encode_frame


ROOT = Path(__file__).resolve().parent.parent


class ContractTests(unittest.TestCase):
    def _load_json(self, relative_path: str) -> dict:
        return json.loads((ROOT / relative_path).read_text(encoding="utf-8"))

    def test_fixture_envelopes_parse(self) -> None:
        for relative_path in (
            "contracts/fixtures/event-battery-alert.json",
            "contracts/fixtures/state-powered-level.json",
            "contracts/fixtures/command-servo-set-angle.json",
            "contracts/fixtures/command-sleepy-threshold-set.json",
            "contracts/fixtures/command-sleepy-maintenance-sync.json",
        ):
            envelope = FabricEnvelope.from_dict(self._load_json(relative_path))
            self.assertEqual(envelope.schema_version, "1.0.0")

    def test_manifest_and_lease_parse(self) -> None:
        manifest = NodeManifest.from_dict(
            self._load_json("contracts/fixtures/manifest-sleepy-leaf.json")
        )
        lease = RoleLease.from_dict(self._load_json("contracts/fixtures/lease-sleepy-leaf.json"))
        self.assertEqual(manifest.hardware_id, "battery-leaf-01")
        self.assertEqual(lease.effective_role, "sleepy_leaf")

    def test_jp_profile_caps_match_docs(self) -> None:
        self.assertEqual(body_cap_for_profile("JP125_LONG_SF10"), 10)
        self.assertEqual(body_cap_for_profile("JP125_LONG_SF10", relayed=True), 6)
        self.assertEqual(body_cap_for_profile("JP125_BAL_SF9"), 34)

    def test_usb_cdc_frame_roundtrip(self) -> None:
        payload = b"hello-fabric"
        encoded = encode_frame(3, payload)
        frame_type, decoded = decode_frame(encoded)
        self.assertEqual(frame_type, 3)
        self.assertEqual(decoded, payload)

    def test_usb_cdc_frame_rejects_trailing_bytes(self) -> None:
        payload = b"hello-fabric"
        encoded = encode_frame(3, payload) + b"x"
        with self.assertRaises(ValueError):
            decode_frame(encoded)

    def test_usb_cdc_frame_rejects_header_tamper(self) -> None:
        payload = b"hello-fabric"
        encoded = bytearray(encode_frame(3, payload))
        encoded[3] = 2
        with self.assertRaises(ValueError):
            decode_frame(bytes(encoded))

    def test_ack_phase_artifact_stays_in_sync(self) -> None:
        artifact = self._load_json("contracts/protocol/ack-phases.json")
        phases = set(artifact["persist_ack"] + artifact["command_phase_ack"])
        self.assertIn(AckPhase.PERSISTED.value, phases)
        self.assertIn(AckPhase.SUCCEEDED.value, phases)

    def test_unknown_jp_profile_is_rejected(self) -> None:
        with self.assertRaises(ValueError):
            body_cap_for_profile("UNKNOWN_PROFILE")

    def test_compact_codec_artifact_stays_in_sync(self) -> None:
        artifact = self._load_json("contracts/protocol/compact-codecs.json")
        self.assertEqual(frame_type_spec(3).name, artifact["frame_types"]["3"]["name"])
        self.assertEqual(frame_type_spec(4).wire_shape, artifact["frame_types"]["4"]["wire_shape"])
        self.assertEqual(shape_for("S", 3), "state_compact_v1")
        self.assertEqual(shape_for("R", 4), "command_result_summary_v1")

    def test_sleepy_command_policy_artifact_covers_fixtures(self) -> None:
        policy = self._load_json("contracts/protocol/sleepy-command-policy.json")
        accepted = self._load_json("contracts/fixtures/command-sleepy-threshold-set.json")
        maintenance = self._load_json("contracts/fixtures/command-sleepy-maintenance-sync.json")
        self.assertIn(
            accepted["payload"]["command_name"],
            policy["sleepy_safe_commands"],
        )
        self.assertEqual(
            accepted["payload"]["service_level"],
            policy["default_service_level"],
        )
        self.assertIn(
            maintenance["payload"]["command_name"],
            policy["maintenance_only_commands"],
        )
        self.assertIn(
            maintenance["delivery"]["route_class"],
            policy["maintenance_route_classes"],
        )

    def test_heartbeat_wire_artifact_matches_usb_frame_type(self) -> None:
        artifact = self._load_json("contracts/protocol/heartbeat-wire.json")
        self.assertEqual(artifact["usb_frame_type"], 2)
        self.assertEqual(shape_for("H", 4), artifact["lora_compact_shapes"]["4"])


if __name__ == "__main__":
    unittest.main()
