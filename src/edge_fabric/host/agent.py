from __future__ import annotations

import json
import time
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from edge_fabric.contracts.models import FabricEnvelope
from edge_fabric.host.site_router import PersistAck, SiteRouter
from edge_fabric.protocol.compact_codec import frame_type_spec, shape_for
from edge_fabric.protocol.usb_cdc import decode_frame, encode_frame


USB_FRAME_FABRIC_ENVELOPE_JSON = 1
USB_FRAME_GATEWAY_HEARTBEAT_JSON = 2
USB_FRAME_COMPACT_BINARY = 3
USB_FRAME_SUMMARY_BINARY = 4


def _utcnow_iso() -> str:
    return datetime.now(UTC).isoformat()


@dataclass(frozen=True)
class IngressObservation:
    ingress_id: str
    session_id: str
    transport: str
    received_at: str
    frame_type: int | None = None
    rssi: int | None = None
    snr: float | None = None
    hop_count: int | None = None


@dataclass(frozen=True)
class AgentRelayResult:
    status: str
    ack: PersistAck | None
    spooled: bool
    observation: IngressObservation


class HostAgent:
    """Host-side relay that normalizes ingress and forwards to Site Router."""

    def __init__(self, router: SiteRouter, *, spool_path: str | Path):
        self.router = router
        self.spool_path = Path(spool_path)
        self.spool_path.parent.mkdir(parents=True, exist_ok=True)
        self.rejects_path = self.spool_path.with_suffix(".rejected.jsonl")
        self.heartbeat_path = self.spool_path.with_suffix(".heartbeat.json")

    def relay_usb_frame(
        self,
        *,
        ingress_id: str,
        session_id: str,
        frame: bytes,
        metadata: dict[str, Any] | None = None,
    ) -> AgentRelayResult:
        frame_type, payload = decode_frame(frame)
        observation = IngressObservation(
            ingress_id=ingress_id,
            session_id=session_id,
            transport="usb_cdc",
            received_at=_utcnow_iso(),
            frame_type=frame_type,
            rssi=self._safe_int((metadata or {}).get("rssi")),
            snr=self._safe_float((metadata or {}).get("snr")),
            hop_count=self._safe_int((metadata or {}).get("hop_count")),
        )
        if frame_type == USB_FRAME_GATEWAY_HEARTBEAT_JSON:
            heartbeat_payload = json.loads(payload.decode("utf-8"))
            self.heartbeat_path.write_text(
                json.dumps(
                    {
                        "observation": asdict(observation),
                        "payload": heartbeat_payload,
                    },
                    sort_keys=True,
                ),
                encoding="utf-8",
            )
            return AgentRelayResult(
                status="heartbeat_recorded",
                ack=None,
                spooled=False,
                observation=observation,
            )
        if frame_type in {USB_FRAME_COMPACT_BINARY, USB_FRAME_SUMMARY_BINARY}:
            envelope_dict, status = self._decode_compact_summary_payload(frame_type, payload)
            if envelope_dict is None:
                return AgentRelayResult(
                    status=status,
                    ack=None,
                    spooled=False,
                    observation=observation,
                )
            return self._relay_envelope_dict(
                envelope_dict=envelope_dict,
                ingress_id=ingress_id,
                session_id=session_id,
                transport="usb_cdc",
                observation=observation,
            )
        if frame_type != USB_FRAME_FABRIC_ENVELOPE_JSON:
            raise ValueError(f"unsupported USB frame type: {frame_type}")

        envelope_dict = json.loads(payload.decode("utf-8"))
        return self._relay_envelope_dict(
            envelope_dict=envelope_dict,
            ingress_id=ingress_id,
            session_id=session_id,
            transport="usb_cdc",
            observation=observation,
        )

    def relay_direct_ip(
        self,
        *,
        ingress_id: str,
        session_id: str,
        envelope_dict: dict[str, Any],
        metadata: dict[str, Any] | None = None,
    ) -> AgentRelayResult:
        observation = IngressObservation(
            ingress_id=ingress_id,
            session_id=session_id,
            transport="wifi_ip",
            received_at=_utcnow_iso(),
            rssi=self._safe_int((metadata or {}).get("rssi")),
            snr=self._safe_float((metadata or {}).get("snr")),
            hop_count=self._safe_int((metadata or {}).get("hop_count")),
        )
        return self._relay_envelope_dict(
            envelope_dict=envelope_dict,
            ingress_id=ingress_id,
            session_id=session_id,
            transport="wifi_ip",
            observation=observation,
        )

    def _relay_envelope_dict(
        self,
        *,
        envelope_dict: dict[str, Any],
        ingress_id: str,
        session_id: str,
        transport: str,
        observation: IngressObservation,
    ) -> AgentRelayResult:
        envelope = FabricEnvelope.from_dict(envelope_dict)
        enriched = envelope.to_dict()
        enriched.setdefault("delivery", {})
        ingress_meta = {
            "transport": transport,
            "session_id": session_id,
            "received_at": observation.received_at,
            "ingress_id": ingress_id,
        }
        if observation.rssi is not None:
            ingress_meta["rssi"] = observation.rssi
        if observation.snr is not None:
            ingress_meta["snr"] = observation.snr
        if observation.hop_count is not None:
            ingress_meta["hop_count"] = observation.hop_count
        enriched["delivery"]["ingress_metadata"] = ingress_meta

        try:
            ack = self.router.ingest(enriched, ingress_id=ingress_id)
            return AgentRelayResult(
                status=ack.status,
                ack=ack,
                spooled=False,
                observation=observation,
            )
        except ValueError as exc:
            self._append_reject_record(
                {
                    "record_type": "reject",
                    "observation": asdict(observation),
                    "ingress_id": ingress_id,
                    "transport": transport,
                    "session_id": session_id,
                    "envelope": enriched,
                    "error": repr(exc),
                }
            )
            return AgentRelayResult(
                status="rejected",
                ack=None,
                spooled=False,
                observation=observation,
            )
        except Exception as exc:
            self._append_spool_record(
                {
                    "record_type": "envelope",
                    "observation": asdict(observation),
                    "ingress_id": ingress_id,
                    "transport": transport,
                    "session_id": session_id,
                    "envelope": enriched,
                    "error": repr(exc),
                }
            )
            return AgentRelayResult(
                status="spooled",
                ack=None,
                spooled=True,
                observation=observation,
            )

    def flush_spool(self) -> int:
        if not self.spool_path.exists():
            return 0
        remaining: list[dict[str, Any]] = []
        flushed = 0
        for line in self.spool_path.read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError as exc:
                self._append_reject_record(
                    {
                        "record_type": "reject",
                        "error": repr(exc),
                        "raw_line": line,
                    }
                )
                continue
            if record.get("record_type") != "envelope":
                continue
            try:
                self.router.ingest(record["envelope"], ingress_id=record["ingress_id"])
                flushed += 1
            except ValueError as exc:
                record["error"] = repr(exc)
                self._append_reject_record(record)
            except Exception as exc:
                record["error"] = repr(exc)
                remaining.append(record)
        if remaining:
            self.spool_path.write_text(
                "".join(json.dumps(item, sort_keys=True) + "\n" for item in remaining),
                encoding="utf-8",
            )
        else:
            self.spool_path.unlink(missing_ok=True)
        return flushed

    def diagnostics(self) -> dict[str, Any]:
        spool_records = 0
        if self.spool_path.exists():
            spool_records = len(
                [line for line in self.spool_path.read_text(encoding="utf-8").splitlines() if line.strip()]
            )
        reject_records = 0
        if self.rejects_path.exists():
            reject_records = len(
                [line for line in self.rejects_path.read_text(encoding="utf-8").splitlines() if line.strip()]
            )
        heartbeat = None
        if self.heartbeat_path.exists():
            heartbeat = json.loads(self.heartbeat_path.read_text(encoding="utf-8"))
        return {
            "spool_records": spool_records,
            "spool_path": str(self.spool_path),
            "reject_records": reject_records,
            "rejects_path": str(self.rejects_path),
            "last_heartbeat": heartbeat,
        }

    def encode_envelope_frame(self, envelope_dict: dict[str, Any]) -> bytes:
        return encode_frame(
            USB_FRAME_FABRIC_ENVELOPE_JSON,
            json.dumps(envelope_dict, sort_keys=True).encode("utf-8"),
        )

    def encode_heartbeat_frame(self, heartbeat_dict: dict[str, Any]) -> bytes:
        return encode_frame(
            USB_FRAME_GATEWAY_HEARTBEAT_JSON,
            json.dumps(heartbeat_dict, sort_keys=True).encode("utf-8"),
        )

    def _append_spool_record(self, record: dict[str, Any]) -> None:
        with self.spool_path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(record, sort_keys=True) + "\n")

    def _append_reject_record(self, record: dict[str, Any]) -> None:
        with self.rejects_path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(record, sort_keys=True) + "\n")

    def _decode_compact_summary_payload(
        self, frame_type: int, payload: bytes
    ) -> tuple[dict[str, Any] | None, str]:
        text = payload.decode("utf-8").strip()
        if not text:
            raise ValueError("empty compact payload")
        parts = text.split("|")
        now = _utcnow_iso()
        message_id = f"msg-compact-{time.time_ns()}"
        frame_spec = frame_type_spec(frame_type)
        if parts[0] == "S" and len(parts) >= 5 and parts[1] and parts[2]:
            return (
                {
                    "schema_version": "1.0.0",
                    "message_id": message_id,
                    "kind": "state",
                    "priority": "normal",
                    "occurred_at": now,
                    "source": {"hardware_id": parts[1]},
                    "target": {"kind": "service", "value": "state"},
                    "payload": {
                        "state_key": parts[2],
                        "value": parts[3],
                        "event_wake": parts[4] == "1",
                        "shape": shape_for("S", frame_type),
                        "wire_shape": frame_spec.wire_shape,
                        "codec_family": frame_spec.name,
                    },
                },
                "",
            )
        if parts[0] == "E" and len(parts) >= 4 and parts[1] and parts[2]:
            return (
                {
                    "schema_version": "1.0.0",
                    "message_id": message_id,
                    "kind": "event",
                    "priority": "critical",
                    "event_id": parts[2],
                    "occurred_at": now,
                    "source": {"hardware_id": parts[1]},
                    "target": {"kind": "service", "value": "events"},
                    "payload": {
                        "value": parts[3],
                        "shape": shape_for("E", frame_type),
                        "wire_shape": frame_spec.wire_shape,
                        "codec_family": frame_spec.name,
                    },
                },
                "",
            )
        if parts[0] == "R" and len(parts) >= 5 and parts[1] and parts[2]:
            return (
                {
                    "schema_version": "1.0.0",
                    "message_id": message_id,
                    "kind": "command_result",
                    "priority": "control",
                    "command_id": parts[2],
                    "occurred_at": now,
                    "source": {"hardware_id": parts[1]},
                    "target": {"kind": "client", "value": "sleepy-node-sdk"},
                    "payload": {
                        "command_id": parts[2],
                        "phase": parts[3],
                        "result": parts[4],
                        "shape": shape_for("R", frame_type),
                        "wire_shape": frame_spec.wire_shape,
                        "codec_family": frame_spec.name,
                    },
                },
                "",
            )
        if parts[0] == "D":
            return None, "digest_recorded"
        if parts[0] == "P":
            return None, "poll_recorded"
        raise ValueError("unsupported compact payload")

    @staticmethod
    def _safe_int(value: Any) -> int | None:
        if value is None:
            return None
        return int(value)

    @staticmethod
    def _safe_float(value: Any) -> float | None:
        if value is None:
            return None
        return float(value)
