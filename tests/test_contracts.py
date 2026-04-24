from __future__ import annotations

import json
import re
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
        self.assertEqual(artifact["track"], "legacy_reference")
        self.assertEqual(frame_type_spec(3).name, artifact["frame_types"]["3"]["name"])
        self.assertEqual(frame_type_spec(4).wire_shape, artifact["frame_types"]["4"]["wire_shape"])
        self.assertEqual(shape_for("S", 3), "state_compact_v1")
        self.assertEqual(shape_for("R", 4), "command_result_summary_v1")

    def test_onair_artifact_declares_mainline_binary_contract(self) -> None:
        artifact = self._load_json("contracts/protocol/onair-v1.json")
        self.assertEqual(artifact["track"], "mainline")
        self.assertEqual(artifact["header"]["version"], 1)
        self.assertEqual(artifact["header"]["size_bytes"], 8)
        self.assertEqual(artifact["logical_types"]["1"]["name"], "state")
        self.assertEqual(artifact["logical_types"]["2"]["implementation_status"], "active")
        self.assertEqual(artifact["logical_types"]["3"]["name"], "command_result")
        self.assertEqual(artifact["logical_types"]["7"]["implementation_status"], "active")
        self.assertIn("sequence_semantics", artifact["header"])
        self.assertEqual(artifact["relay_extension"]["implementation_status"], "planned")
        self.assertEqual(
            artifact["event"]["body_layout"],
            ["event_code:u8", "severity:u8", "value_bucket:u8", "flags:u8"],
        )
        self.assertEqual(
            artifact["heartbeat"]["body_layout"],
            ["health:u8", "battery_bucket:u8", "link_quality:u8", "uptime_bucket:u8", "flags:u8"],
        )

    def test_onair_artifact_matches_go_and_firmware_constants(self) -> None:
        artifact = self._load_json("contracts/protocol/onair-v1.json")
        go_source = (ROOT / "internal" / "protocol" / "onair" / "codec.go").read_text(encoding="utf-8")
        firmware_header = (
            ROOT
            / "firmware"
            / "esp-idf"
            / "components"
            / "fabric_proto"
            / "include"
            / "fabric_proto"
            / "fabric_proto.h"
        ).read_text(encoding="utf-8")

        def go_byte(name: str) -> int:
            pattern = re.escape(name) + r"\s+byte\s*=\s*([^\n]+)"
            match = re.search(pattern, go_source)
            self.assertIsNotNone(match, name)
            expr = match.group(1).strip()
            if "<<" in expr:
                left, right = [part.strip() for part in expr.split("<<")]
                return int(left) << int(right)
            return int(expr)

        def c_value(name: str) -> int:
            def eval_expr(expr: str) -> int:
                expr = expr.strip().strip("()")
                expr = expr.replace("u", "").replace("U", "")
                if "<<" in expr:
                    left, right = [part.strip() for part in expr.split("<<")]
                    return int(left) << int(right)
                return int(expr)

            match = re.search(rf"#define\s+{re.escape(name)}\s+([^\n]+)", firmware_header)
            if match is not None:
                return eval_expr(match.group(1))
            match = re.search(rf"{re.escape(name)}\s*=\s*([^,\n]+)", firmware_header)
            self.assertIsNotNone(match, name)
            return eval_expr(match.group(1))

        self.assertEqual(artifact["header"]["version"], go_byte("Version"))
        self.assertEqual(artifact["header"]["version"], c_value("EF_ONAIR_VERSION"))
        self.assertEqual(artifact["header"]["size_bytes"], c_value("EF_ONAIR_HEADER_SIZE"))
        self.assertEqual(artifact["flags"]["summary"], go_byte("FlagSummary"))
        self.assertEqual(artifact["flags"]["summary"], c_value("EF_ONAIR_FLAG_SUMMARY"))

        logical_go = {
            "1": go_byte("TypeState"),
            "2": go_byte("TypeEvent"),
            "3": go_byte("TypeCommandResult"),
            "4": go_byte("TypePendingDigest"),
            "5": go_byte("TypeTinyPoll"),
            "6": go_byte("TypeCompactCommand"),
            "7": go_byte("TypeHeartbeat"),
        }
        logical_c = {
            "1": c_value("EF_ONAIR_TYPE_STATE"),
            "2": c_value("EF_ONAIR_TYPE_EVENT"),
            "3": c_value("EF_ONAIR_TYPE_COMMAND_RESULT"),
            "4": c_value("EF_ONAIR_TYPE_PENDING_DIGEST"),
            "5": c_value("EF_ONAIR_TYPE_TINY_POLL"),
            "6": c_value("EF_ONAIR_TYPE_COMPACT_COMMAND"),
            "7": c_value("EF_ONAIR_TYPE_HEARTBEAT"),
        }
        self.assertEqual(logical_go, {key: int(key) for key in artifact["logical_types"].keys()})
        self.assertEqual(logical_c, {key: int(key) for key in artifact["logical_types"].keys()})

        self.assertEqual(int(next(iter(artifact["state"]["keys"].keys()))), go_byte("StateKeyNodePower"))
        self.assertEqual(int(next(iter(artifact["state"]["keys"].keys()))), c_value("EF_ONAIR_STATE_KEY_NODE_POWER"))
        for key, const_name in {
            "0": "StateValueUnknown",
            "1": "StateValueAwake",
            "2": "StateValueSleep",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "0": "EF_ONAIR_STATE_VALUE_UNKNOWN",
            "1": "EF_ONAIR_STATE_VALUE_AWAKE",
            "2": "EF_ONAIR_STATE_VALUE_SLEEP",
        }.items():
            self.assertEqual(int(key), c_value(const_name))

        for key, const_name in {
            "1": "EventCodeBatteryLow",
            "2": "EventCodeMotionDetected",
            "3": "EventCodeLeakDetected",
            "4": "EventCodeTamper",
            "5": "EventCodeThresholdCrossed",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_EVENT_CODE_BATTERY_LOW",
            "2": "EF_ONAIR_EVENT_CODE_MOTION_DETECTED",
            "3": "EF_ONAIR_EVENT_CODE_LEAK_DETECTED",
            "4": "EF_ONAIR_EVENT_CODE_TAMPER",
            "5": "EF_ONAIR_EVENT_CODE_THRESHOLD_CROSSED",
        }.items():
            self.assertEqual(int(key), c_value(const_name))
        for key, const_name in {
            "1": "EventSeverityInfo",
            "2": "EventSeverityWarning",
            "3": "EventSeverityCritical",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_EVENT_SEVERITY_INFO",
            "2": "EF_ONAIR_EVENT_SEVERITY_WARNING",
            "3": "EF_ONAIR_EVENT_SEVERITY_CRITICAL",
        }.items():
            self.assertEqual(int(key), c_value(const_name))
        self.assertEqual(int("1"), go_byte("EventFlagEventWake"))
        self.assertEqual(int("2"), go_byte("EventFlagLatched"))
        self.assertEqual(int("1"), c_value("EF_ONAIR_EVENT_FLAG_EVENT_WAKE"))
        self.assertEqual(int("2"), c_value("EF_ONAIR_EVENT_FLAG_LATCHED"))

        for key, const_name in {
            "1": "CommandKindMaintenanceOn",
            "2": "CommandKindMaintenanceOff",
            "3": "CommandKindThresholdSet",
            "4": "CommandKindQuietSet",
            "5": "CommandKindAlarmClear",
            "6": "CommandKindSamplingSet",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_COMMAND_KIND_MAINTENANCE_ON",
            "2": "EF_ONAIR_COMMAND_KIND_MAINTENANCE_OFF",
            "3": "EF_ONAIR_COMMAND_KIND_THRESHOLD_SET",
            "4": "EF_ONAIR_COMMAND_KIND_QUIET_SET",
            "5": "EF_ONAIR_COMMAND_KIND_ALARM_CLEAR",
            "6": "EF_ONAIR_COMMAND_KIND_SAMPLING_SET",
        }.items():
            self.assertEqual(int(key), c_value(const_name))

        for key, const_name in {
            "1": "PhaseAccepted",
            "2": "PhaseExecuting",
            "3": "PhaseSucceeded",
            "4": "PhaseFailed",
            "5": "PhaseRejected",
            "6": "PhaseExpired",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_PHASE_ACCEPTED",
            "2": "EF_ONAIR_PHASE_EXECUTING",
            "3": "EF_ONAIR_PHASE_SUCCEEDED",
            "4": "EF_ONAIR_PHASE_FAILED",
            "5": "EF_ONAIR_PHASE_REJECTED",
            "6": "EF_ONAIR_PHASE_EXPIRED",
        }.items():
            self.assertEqual(int(key), c_value(const_name))

        for key, const_name in {
            "1": "ReasonOK",
            "2": "ReasonService",
            "3": "ReasonMaintenance",
            "4": "ReasonStale",
            "5": "ReasonBadCommand",
            "6": "ReasonUnsupported",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_REASON_OK",
            "2": "EF_ONAIR_REASON_SERVICE",
            "3": "EF_ONAIR_REASON_MAINTENANCE",
            "4": "EF_ONAIR_REASON_STALE",
            "5": "EF_ONAIR_REASON_BAD_COMMAND",
            "6": "EF_ONAIR_REASON_UNSUPPORTED",
        }.items():
            self.assertEqual(int(key), c_value(const_name))

        self.assertEqual(
            int(next(iter(artifact["tiny_poll"]["service_levels"].keys()))),
            go_byte("ServiceLevelEventualNextPoll"),
        )
        self.assertEqual(
            int(next(iter(artifact["tiny_poll"]["service_levels"].keys()))),
            c_value("EF_ONAIR_SERVICE_LEVEL_EVENTUAL_NEXT_POLL"),
        )
        self.assertEqual(int("1"), go_byte("PendingFlagUrgent"))
        self.assertEqual(int("2"), go_byte("PendingFlagExpiresSoon"))
        self.assertEqual(int("1"), c_value("EF_ONAIR_PENDING_FLAG_URGENT"))
        self.assertEqual(int("2"), c_value("EF_ONAIR_PENDING_FLAG_EXPIRES_SOON"))
        for key, const_name in {
            "1": "HeartbeatHealthOK",
            "2": "HeartbeatHealthDegraded",
            "3": "HeartbeatHealthCritical",
        }.items():
            self.assertEqual(int(key), go_byte(const_name))
        for key, const_name in {
            "1": "EF_ONAIR_HEARTBEAT_HEALTH_OK",
            "2": "EF_ONAIR_HEARTBEAT_HEALTH_DEGRADED",
            "3": "EF_ONAIR_HEARTBEAT_HEALTH_CRITICAL",
        }.items():
            self.assertEqual(int(key), c_value(const_name))
        self.assertEqual(int("1"), go_byte("HeartbeatFlagEventWake"))
        self.assertEqual(int("2"), go_byte("HeartbeatFlagMaintenanceAwake"))
        self.assertEqual(int("4"), go_byte("HeartbeatFlagLowPower"))
        self.assertEqual(int("1"), c_value("EF_ONAIR_HEARTBEAT_FLAG_EVENT_WAKE"))
        self.assertEqual(int("2"), c_value("EF_ONAIR_HEARTBEAT_FLAG_MAINTENANCE_AWAKE"))
        self.assertEqual(int("4"), c_value("EF_ONAIR_HEARTBEAT_FLAG_LOW_POWER"))

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
        self.assertEqual(accepted["delivery"]["route_class"], "sleepy_tiny_control")
        self.assertIn("command_token", accepted["payload"])
        self.assertIn("value", accepted["payload"])
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
        firmware_source = (
            ROOT / "firmware" / "esp-idf" / "gateway-head" / "main" / "gateway_head_runtime.c"
        ).read_text(encoding="utf-8")
        self.assertEqual(artifact["track"], "legacy_reference")
        self.assertEqual(artifact["mainline_onair_status"], "active")
        self.assertEqual(artifact["usb_frame_type"], 2)
        self.assertEqual(
            artifact["gateway_json_shapes"]["status_heartbeat_v1"],
            ["gateway_id", "subject_kind", "subject_id", "live", "status", "value"],
        )
        self.assertEqual(
            artifact["gateway_json_shapes"]["lora_ingress_v1"],
            ["gateway_id", "subject_kind", "subject_id", "live", "status", "rssi", "snr"],
        )
        self.assertIn("lora_ingress", firmware_source)
        self.assertIn("rssi", firmware_source)
        self.assertIn("snr", firmware_source)
        self.assertEqual(shape_for("H", 4), artifact["lora_compact_shapes"]["4"])

    def test_policy_artifacts_capture_safe_defaults(self) -> None:
        roles = self._load_json("contracts/policy/role-policy.json")
        routes = self._load_json("contracts/policy/route-classes.json")
        profiles = self._load_json("contracts/policy/device-profiles.json")
        self.assertFalse(roles["roles"]["sleepy_leaf"]["may_relay"])
        self.assertFalse(roles["roles"]["sleepy_leaf"]["requires_always_on"])
        sleepy_route = routes["route_classes"]["sleepy_tiny_control"]
        self.assertEqual(sleepy_route["allowed_bearers"], ["lora_direct"])
        self.assertFalse(sleepy_route["allow_relay"])
        motion = profiles["profiles"]["motion_sensor_battery_v1"]
        self.assertEqual(motion["allowed_roles"], ["sleepy_leaf"])
        self.assertTrue(motion["forbidden"]["relay"])
        for profile in profiles["profiles"].values():
            for route_class in profile["default_routes"].values():
                self.assertIn(route_class, routes["route_classes"])

    def test_fixture_route_classes_exist_in_policy_artifact(self) -> None:
        routes = self._load_json("contracts/policy/route-classes.json")["route_classes"]
        for fixture in (
            "contracts/fixtures/command-servo-set-angle.json",
            "contracts/fixtures/command-sleepy-threshold-set.json",
            "contracts/fixtures/command-sleepy-maintenance-sync.json",
        ):
            envelope = self._load_json(fixture)
            route_class = envelope.get("delivery", {}).get("route_class")
            if route_class:
                self.assertIn(route_class, routes, fixture)

    def test_policy_artifacts_are_referenced_by_go_runtime(self) -> None:
        routes = self._load_json("contracts/policy/route-classes.json")["route_classes"]
        router_source = (ROOT / "internal" / "siterouter" / "router.go").read_text(
            encoding="utf-8"
        )
        for route_class in routes:
            self.assertIn(route_class, router_source)
        for role, policy in self._load_json("contracts/policy/role-policy.json")[
            "roles"
        ].items():
            if policy.get("requires_always_on"):
                self.assertIn(role, router_source)
        fabric_source = (ROOT / "pkg" / "fabric" / "profiles.go").read_text(encoding="utf-8")
        profiles = self._load_json("contracts/policy/device-profiles.json")["profiles"]
        for profile_id in profiles:
            self.assertIn(profile_id, fabric_source)


if __name__ == "__main__":
    unittest.main()
