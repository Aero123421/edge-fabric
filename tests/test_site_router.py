from __future__ import annotations

import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path

from edge_fabric.contracts.enums import MessageKind, Priority, TargetKind
from edge_fabric.contracts.models import FabricEnvelope, SourceRef, TargetRef
from edge_fabric.host.site_router import SiteRouter


class SiteRouterTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "site-router.db"
        self.router = SiteRouter(self.db_path)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_duplicate_event_dedupes_by_event_id(self) -> None:
        first = {
            "schema_version": "1.0.0",
            "message_id": "msg-001",
            "kind": "event",
            "priority": "critical",
            "event_id": "evt-001",
            "source": {"hardware_id": "sensor-01"},
            "target": {"kind": "service", "value": "alerts"},
            "payload": {"alarm_code": "water"},
        }
        second = {
            **first,
            "message_id": "msg-002",
        }

        ack1 = self.router.ingest(first, ingress_id="gateway-a")
        ack2 = self.router.ingest(second, ingress_id="gateway-b")

        self.assertFalse(ack1.duplicate)
        self.assertTrue(ack2.duplicate)
        self.assertEqual(self.router.count_events(), 1)

    def test_queue_recovery_requeues_expired_sending_items(self) -> None:
        envelope = FabricEnvelope(
            schema_version="1.0.0",
            message_id="msg-queue-001",
            kind=MessageKind.EVENT,
            priority=Priority.CRITICAL,
            event_id="evt-queue-001",
            source=SourceRef(hardware_id="sensor-02"),
            target=TargetRef(kind=TargetKind.SERVICE, value="alerts"),
            payload={"alarm_code": "battery_low"},
        )
        queue_id = self.router.enqueue_outbound(envelope)
        leases = self.router.lease_outbound(worker_id="worker-a", limit=1, lease_seconds=1)
        self.assertEqual(queue_id, leases[0].queue_id)
        self.router.mark_sending(queue_id, worker_id="worker-a")

        recovered = self.router.requeue_expired_leases(
            now=datetime.now(UTC) + timedelta(seconds=2)
        )
        metrics = self.router.queue_metrics()

        self.assertEqual(recovered, 1)
        self.assertEqual(metrics["queued_count"], 1)
        self.assertEqual(metrics["sending_count"], 0)

    def test_duplicate_queue_key_is_idempotent(self) -> None:
        envelope = FabricEnvelope(
            schema_version="1.0.0",
            message_id="msg-queue-dup-001",
            kind=MessageKind.EVENT,
            priority=Priority.NORMAL,
            event_id="evt-queue-dup-001",
            source=SourceRef(hardware_id="sensor-03"),
            target=TargetRef(kind=TargetKind.SERVICE, value="alerts"),
            payload={"alarm_code": "door_open"},
        )
        first_id = self.router.enqueue_outbound(envelope)
        second_id = self.router.enqueue_outbound(envelope)
        self.assertEqual(first_id, second_id)

    def test_command_idempotency_and_result_phase(self) -> None:
        command = {
            "schema_version": "1.0.0",
            "message_id": "msg-command-001",
            "kind": "command",
            "priority": "control",
            "command_id": "cmd-001",
            "source": {"hardware_id": "controller-01"},
            "target": {"kind": "node", "value": "servo-01"},
            "payload": {"command_name": "servo.set_angle", "angle": 90},
        }
        duplicate = {**command, "message_id": "msg-command-002"}
        result = {
            "schema_version": "1.0.0",
            "message_id": "msg-command-result-001",
            "kind": "command_result",
            "priority": "control",
            "command_id": "cmd-001",
            "source": {"hardware_id": "servo-01"},
            "target": {"kind": "client", "value": "controller-01"},
            "payload": {"phase": "succeeded", "command_id": "cmd-001"},
        }

        ack1 = self.router.ingest(command)
        ack2 = self.router.ingest(duplicate)
        self.router.ingest(result)

        self.assertFalse(ack1.duplicate)
        self.assertTrue(ack2.duplicate)
        self.assertEqual(self.router.command_state("cmd-001"), "succeeded")

    def test_issue_command_returns_queue_and_specialized_semantics(self) -> None:
        command = {
            "schema_version": "1.0.0",
            "message_id": "msg-command-special-001",
            "kind": "command",
            "priority": "control",
            "command_id": "cmd-special-001",
            "source": {"hardware_id": "controller-10"},
            "target": {"kind": "node", "value": "sleepy-10"},
            "payload": {"command_name": "threshold.set", "value": 12, "command_token": 0x2210},
        }
        ack, queue_id = self.router.issue_command(command, ingress_id="sdk-local")
        self.assertEqual(ack.status, "persisted")
        self.assertGreater(queue_id, 0)
        self.assertEqual(self.router.command_state("cmd-special-001"), "issued")

    def test_latest_state_rebuild_restores_projection(self) -> None:
        first = {
            "schema_version": "1.0.0",
            "message_id": "msg-state-001",
            "kind": "state",
            "priority": "normal",
            "occurred_at": "2026-04-23T08:00:00+00:00",
            "source": {"hardware_id": "tank-01", "session_id": "sess-a", "seq_local": 1},
            "target": {"kind": "service", "value": "state"},
            "payload": {"state_key": "tank.level", "value": 50},
        }
        second = {
            **first,
            "message_id": "msg-state-002",
            "occurred_at": "2026-04-23T08:01:00+00:00",
            "source": {"hardware_id": "tank-01", "session_id": "sess-a", "seq_local": 2},
            "payload": {"state_key": "tank.level", "value": 75},
        }
        self.router.ingest(first)
        self.router.ingest(second)
        self.router.rebuild_latest_state()

        self.assertEqual(
            self.router.latest_state("tank-01", "tank.level"),
            {"state_key": "tank.level", "value": 75},
        )

    def test_older_state_does_not_overwrite_newer_projection(self) -> None:
        newer = {
            "schema_version": "1.0.0",
            "message_id": "msg-state-new",
            "kind": "state",
            "priority": "normal",
            "occurred_at": "2026-04-23T08:05:00+00:00",
            "source": {"hardware_id": "tank-02", "session_id": "sess-a", "seq_local": 5},
            "target": {"kind": "service", "value": "state"},
            "payload": {"state_key": "tank.level", "value": 80},
        }
        older = {
            **newer,
            "message_id": "msg-state-old",
            "occurred_at": "2026-04-23T08:01:00+00:00",
            "source": {"hardware_id": "tank-02", "session_id": "sess-a", "seq_local": 1},
            "payload": {"state_key": "tank.level", "value": 25},
        }
        self.router.ingest(newer)
        self.router.ingest(older)
        self.assertEqual(
            self.router.latest_state("tank-02", "tank.level"),
            {"state_key": "tank.level", "value": 80},
        )

    def test_invalid_command_result_phase_is_rejected(self) -> None:
        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-command-init",
                "kind": "command",
                "priority": "control",
                "command_id": "cmd-invalid-phase",
                "source": {"hardware_id": "controller-02"},
                "target": {"kind": "node", "value": "servo-02"},
                "payload": {"command_name": "servo.set_angle", "angle": 45},
            }
        )
        with self.assertRaises(ValueError):
            self.router.ingest(
                {
                    "schema_version": "1.0.0",
                    "message_id": "msg-command-bad-phase",
                    "kind": "command_result",
                    "priority": "control",
                    "command_id": "cmd-invalid-phase",
                    "source": {"hardware_id": "servo-02"},
                    "target": {"kind": "client", "value": "controller-02"},
                    "payload": {"phase": "bad_phase", "command_id": "cmd-invalid-phase"},
                }
            )

    def test_heartbeat_summary_and_file_chunk_records(self) -> None:
        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-heartbeat-01",
                "kind": "heartbeat",
                "priority": "normal",
                "source": {"hardware_id": "gateway-head-01"},
                "target": {"kind": "host", "value": "site-router"},
                "delivery": {"ingress_metadata": {"host_link": "usb_cdc", "bearer": "lora_direct"}},
                "payload": {"gateway_id": "gw-alpha", "live": True, "status": "lora_ingress"},
            },
            ingress_id="usb-gw-alpha",
        )
        heartbeat = self.router.latest_heartbeat("gw-alpha")
        assert heartbeat is not None
        self.assertTrue(heartbeat.live)
        self.assertEqual(heartbeat.host_link, "usb_cdc")

        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-summary-01",
                "kind": "fabric_summary",
                "priority": "normal",
                "source": {"hardware_id": "router-01"},
                "target": {"kind": "site", "value": "site-a"},
                "payload": {"summary_scope": "site-a", "healthy_nodes": 4, "degraded_nodes": 1},
            },
            ingress_id="mesh-root",
        )
        summary = self.router.latest_fabric_summary("site-a")
        assert summary is not None
        self.assertEqual(summary.payload["healthy_nodes"], 4)

        for idx in range(3):
            self.router.ingest(
                {
                    "schema_version": "1.0.0",
                    "message_id": f"msg-file-chunk-{idx}",
                    "kind": "file_chunk",
                    "priority": "bulk",
                    "correlation_id": "file-demo-01",
                    "source": {"hardware_id": "controller-ota"},
                    "target": {"kind": "node", "value": "sleepy-ota-01"},
                    "payload": {"file_id": "file-demo-01", "chunk_index": idx, "total_chunks": 3},
                },
                ingress_id="ota-host",
            )
        status = self.router.file_chunk_status("file-demo-01")
        self.assertEqual(status.received_chunks, 3)
        self.assertTrue(status.complete)

    def test_file_chunk_status_requires_contiguous_coverage_and_latest_update(self) -> None:
        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-z-last",
                "kind": "file_chunk",
                "priority": "bulk",
                "correlation_id": "file-gap-01",
                "source": {"hardware_id": "controller-gap"},
                "target": {"kind": "node", "value": "sleepy-gap"},
                "payload": {"file_id": "file-gap-01", "chunk_index": 1, "total_chunks": 3},
            },
            ingress_id="gap-host",
        )
        self.router.ingest(
            {
                "schema_version": "1.0.0",
                "message_id": "msg-a-last",
                "kind": "file_chunk",
                "priority": "bulk",
                "correlation_id": "file-gap-01",
                "source": {"hardware_id": "controller-gap"},
                "target": {"kind": "node", "value": "sleepy-gap"},
                "payload": {"file_id": "file-gap-01", "chunk_index": 2, "total_chunks": 3},
            },
            ingress_id="gap-host",
        )
        status = self.router.file_chunk_status("file-gap-01")
        self.assertFalse(status.complete)
        self.assertEqual(status.last_message_id, "msg-a-last")


if __name__ == "__main__":
    unittest.main()
