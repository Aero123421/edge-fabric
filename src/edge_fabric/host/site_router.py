from __future__ import annotations

import json
import sqlite3
import time
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from edge_fabric.contracts.enums import AckPhase
from edge_fabric.contracts.enums import MessageKind, Priority
from edge_fabric.contracts.models import FabricEnvelope, NodeManifest, RoleLease


def _utcnow() -> datetime:
    return datetime.now(UTC)


def _to_iso(value: datetime) -> str:
    return value.isoformat()


@dataclass(frozen=True)
class PersistAck:
    acked_message_id: str
    status: str
    duplicate: bool


@dataclass(frozen=True)
class QueueLease:
    queue_id: int
    message_id: str | None
    lease_owner: str
    lease_until: str
    envelope: FabricEnvelope


@dataclass(frozen=True)
class HeartbeatRecord:
    heartbeat_key: str
    gateway_id: str | None
    source_hardware_id: str
    ingress_id: str
    host_link: str | None
    bearer: str | None
    status: str | None
    live: bool
    message_id: str
    updated_at: str
    payload: dict[str, Any]


@dataclass(frozen=True)
class FabricSummaryRecord:
    summary_key: str
    source_hardware_id: str
    message_id: str
    updated_at: str
    payload: dict[str, Any]


@dataclass(frozen=True)
class FileChunkStatus:
    file_id: str
    received_chunks: int
    total_chunks: int
    highest_chunk_index: int
    last_message_id: str | None
    last_updated_at: str | None
    complete: bool


class SiteRouter:
    """Durable single-writer core backed by SQLite."""

    def __init__(self, db_path: str | Path, *, max_retry_count: int = 5):
        self.db_path = str(db_path)
        self.max_retry_count = max_retry_count
        self._initialize()

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL")
        conn.execute("PRAGMA synchronous=FULL")
        return conn

    @contextmanager
    def _session(self):
        conn = self._connect()
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()

    def _initialize(self) -> None:
        with self._session() as conn:
            conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS messages (
                    message_id TEXT PRIMARY KEY,
                    kind TEXT NOT NULL,
                    dedupe_key TEXT NOT NULL,
                    event_id TEXT,
                    command_id TEXT,
                    occurred_at TEXT,
                    source_hardware_id TEXT NOT NULL,
                    ingress_id TEXT NOT NULL,
                    envelope_json TEXT NOT NULL,
                    persisted_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS event_ledger (
                    event_id TEXT PRIMARY KEY,
                    message_id TEXT NOT NULL UNIQUE,
                    occurred_at TEXT,
                    priority TEXT NOT NULL,
                    source_hardware_id TEXT NOT NULL,
                    envelope_json TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS latest_state (
                    source_hardware_id TEXT NOT NULL,
                    state_key TEXT NOT NULL,
                    message_id TEXT NOT NULL,
                    occurred_at TEXT,
                    session_id TEXT,
                    seq_local INTEGER,
                    payload_json TEXT NOT NULL,
                    PRIMARY KEY (source_hardware_id, state_key)
                );

                CREATE TABLE IF NOT EXISTS command_ledger (
                    command_id TEXT PRIMARY KEY,
                    command_token INTEGER,
                    message_id TEXT NOT NULL UNIQUE,
                    state TEXT NOT NULL,
                    envelope_json TEXT NOT NULL,
                    created_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS command_execution (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    command_id TEXT NOT NULL,
                    phase TEXT NOT NULL,
                    message_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    created_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS outbox_queue (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    queue_key TEXT UNIQUE,
                    message_id TEXT,
                    target_kind TEXT NOT NULL,
                    target_value TEXT NOT NULL,
                    priority TEXT NOT NULL,
                    envelope_json TEXT NOT NULL,
                    status TEXT NOT NULL,
                    lease_owner TEXT,
                    lease_until TEXT,
                    retry_count INTEGER NOT NULL DEFAULT 0,
                    dead_reason TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS node_manifest (
                    hardware_id TEXT PRIMARY KEY,
                    power_class TEXT NOT NULL,
                    wake_class TEXT NOT NULL,
                    allowed_network_roles_json TEXT NOT NULL,
                    supported_bearers_json TEXT NOT NULL,
                    manifest_json TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS node_lease (
                    hardware_id TEXT PRIMARY KEY,
                    logical_binding_id TEXT NOT NULL,
                    fabric_short_id INTEGER,
                    effective_role TEXT NOT NULL,
                    primary_bearer TEXT NOT NULL,
                    fallback_bearer TEXT,
                    lease_json TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS heartbeat_ledger (
                    heartbeat_key TEXT PRIMARY KEY,
                    gateway_id TEXT,
                    source_hardware_id TEXT NOT NULL,
                    ingress_id TEXT NOT NULL,
                    host_link TEXT,
                    bearer TEXT,
                    status TEXT,
                    live INTEGER NOT NULL DEFAULT 0,
                    message_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS fabric_summary_latest (
                    summary_key TEXT PRIMARY KEY,
                    source_hardware_id TEXT NOT NULL,
                    message_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS file_chunk_ledger (
                    file_id TEXT NOT NULL,
                    chunk_index INTEGER NOT NULL,
                    total_chunks INTEGER NOT NULL,
                    source_hardware_id TEXT NOT NULL,
                    message_id TEXT NOT NULL UNIQUE,
                    payload_json TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    PRIMARY KEY (file_id, chunk_index)
                );
                """
            )

    def ingest(self, envelope_or_dict: FabricEnvelope | dict[str, Any], *, ingress_id: str = "local") -> PersistAck:
        envelope = (
            envelope_or_dict
            if isinstance(envelope_or_dict, FabricEnvelope)
            else FabricEnvelope.from_dict(envelope_or_dict)
        )
        persisted_at = _to_iso(_utcnow())
        try:
            with self._session() as conn:
                duplicate_of = self._lookup_duplicate(conn, envelope)
                if duplicate_of is not None:
                    return PersistAck(acked_message_id=duplicate_of, status="duplicate", duplicate=True)

                conn.execute(
                    """
                    INSERT INTO messages (
                        message_id, kind, dedupe_key, event_id, command_id, occurred_at,
                        source_hardware_id, ingress_id, envelope_json, persisted_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        envelope.message_id,
                        envelope.kind.value,
                        envelope.dedupe_key,
                        envelope.event_id,
                        envelope.command_id,
                        envelope.occurred_at,
                        envelope.source.hardware_id,
                        ingress_id,
                        json.dumps(envelope.to_dict(), sort_keys=True),
                        persisted_at,
                    ),
                )

                if envelope.kind is MessageKind.EVENT and envelope.event_id:
                    conn.execute(
                        """
                        INSERT INTO event_ledger (
                            event_id, message_id, occurred_at, priority, source_hardware_id, envelope_json
                        ) VALUES (?, ?, ?, ?, ?, ?)
                        """,
                        (
                            envelope.event_id,
                            envelope.message_id,
                            envelope.occurred_at,
                            envelope.priority.value,
                            envelope.source.hardware_id,
                            json.dumps(envelope.to_dict(), sort_keys=True),
                        ),
                    )
                elif envelope.kind is MessageKind.STATE:
                    self._upsert_latest_state(conn, envelope)
                elif envelope.kind is MessageKind.COMMAND and envelope.command_id:
                    conn.execute(
                        """
                        INSERT INTO command_ledger (command_id, command_token, message_id, state, envelope_json, created_at)
                        VALUES (?, ?, ?, ?, ?, ?)
                        """,
                        (
                            envelope.command_id,
                            envelope.payload.get("command_token"),
                            envelope.message_id,
                            "issued",
                            json.dumps(envelope.to_dict(), sort_keys=True),
                            persisted_at,
                        ),
                    )
                elif envelope.kind is MessageKind.COMMAND_RESULT:
                    self._record_command_result(conn, envelope, persisted_at)
                elif envelope.kind is MessageKind.MANIFEST:
                    self._upsert_manifest(conn, envelope, persisted_at)
                elif envelope.kind is MessageKind.LEASE:
                    self._upsert_lease(conn, envelope, persisted_at)
                elif envelope.kind is MessageKind.HEARTBEAT:
                    self._upsert_heartbeat(conn, envelope, ingress_id, persisted_at)
                elif envelope.kind is MessageKind.FABRIC_SUMMARY:
                    self._upsert_fabric_summary(conn, envelope, persisted_at)
                elif envelope.kind is MessageKind.FILE_CHUNK:
                    self._upsert_file_chunk(conn, envelope, persisted_at)

                return PersistAck(
                    acked_message_id=envelope.message_id,
                    status="persisted",
                    duplicate=False,
                )
        except sqlite3.IntegrityError:
            with self._session() as conn:
                duplicate_of = self._lookup_duplicate(conn, envelope)
                if duplicate_of is not None:
                    return PersistAck(acked_message_id=duplicate_of, status="duplicate", duplicate=True)
            raise

    def _lookup_duplicate(self, conn: sqlite3.Connection, envelope: FabricEnvelope) -> str | None:
        if envelope.kind is MessageKind.EVENT and envelope.event_id:
            row = conn.execute(
                "SELECT message_id FROM event_ledger WHERE event_id = ?",
                (envelope.event_id,),
            ).fetchone()
            if row:
                return str(row["message_id"])
        if envelope.kind is MessageKind.COMMAND and envelope.command_id:
            row = conn.execute(
                "SELECT message_id FROM command_ledger WHERE command_id = ?",
                (envelope.command_id,),
            ).fetchone()
            if row:
                return str(row["message_id"])
        row = conn.execute(
            "SELECT message_id FROM messages WHERE message_id = ?",
            (envelope.message_id,),
        ).fetchone()
        if row:
            return str(row["message_id"])
        return None

    def _upsert_latest_state(self, conn: sqlite3.Connection, envelope: FabricEnvelope) -> None:
        state_key = str(
            envelope.payload.get("state_key")
            or envelope.payload.get("key")
            or envelope.payload.get("metric")
            or "__default__"
        )
        incoming_key = self._state_order_key(envelope)
        existing = conn.execute(
            """
            SELECT occurred_at, session_id, seq_local
            FROM latest_state
            WHERE source_hardware_id = ? AND state_key = ?
            """,
            (envelope.source.hardware_id, state_key),
        ).fetchone()
        if existing is not None:
            current_key = self._existing_state_order_key(envelope, existing)
            if incoming_key <= current_key:
                return

        conn.execute(
            """
            INSERT INTO latest_state (
                source_hardware_id, state_key, message_id, occurred_at, session_id, seq_local, payload_json
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(source_hardware_id, state_key) DO UPDATE SET
                message_id = excluded.message_id,
                occurred_at = excluded.occurred_at,
                session_id = excluded.session_id,
                seq_local = excluded.seq_local,
                payload_json = excluded.payload_json
            """,
            (
                envelope.source.hardware_id,
                state_key,
                envelope.message_id,
                envelope.occurred_at,
                envelope.source.session_id,
                envelope.source.seq_local,
                json.dumps(envelope.payload, sort_keys=True),
            ),
        )

    def _state_order_key(self, envelope: FabricEnvelope) -> tuple[str, str, int, str]:
        occurred_at = envelope.occurred_at or ""
        session_id = envelope.source.session_id or ""
        seq_local = envelope.source.seq_local if envelope.source.seq_local is not None else -1
        if not session_id:
            seq_local = -1
        return (occurred_at, session_id, seq_local, envelope.message_id)

    def _existing_state_order_key(
        self, incoming: FabricEnvelope, existing: sqlite3.Row
    ) -> tuple[str, str, int, str]:
        session_id = str(existing["session_id"] or "")
        seq_local = int(existing["seq_local"]) if existing["seq_local"] is not None else -1
        if not incoming.source.session_id or incoming.source.session_id != session_id:
            seq_local = -1
        return (
            str(existing["occurred_at"] or ""),
            session_id,
            seq_local,
            "",
        )

    def _record_command_result(
        self,
        conn: sqlite3.Connection,
        envelope: FabricEnvelope,
        persisted_at: str,
    ) -> None:
        command_id = envelope.command_id or envelope.payload.get("command_id")
        if not command_id:
            command_token = envelope.payload.get("command_token")
            if isinstance(command_token, int):
                row = conn.execute(
                    "SELECT command_id FROM command_ledger WHERE command_token = ?",
                    (command_token,),
                ).fetchone()
                if row:
                    command_id = str(row["command_id"])
        if not isinstance(command_id, str) or not command_id:
            raise ValueError("command_result.command_id or payload.command_token is required")
        phase = str(envelope.payload.get("phase", "accepted"))
        if phase not in {item.value for item in AckPhase}:
            raise ValueError(f"invalid command_result phase: {phase}")
        row = conn.execute(
            "SELECT state FROM command_ledger WHERE command_id = ?",
            (command_id,),
        ).fetchone()
        if row is None:
            raise ValueError(f"command_result references unknown command_id: {command_id}")
        if str(row["state"]) == phase:
            return
        conn.execute(
            """
            INSERT INTO command_execution (command_id, phase, message_id, payload_json, created_at)
            VALUES (?, ?, ?, ?, ?)
            """,
            (
                command_id,
                phase,
                envelope.message_id,
                json.dumps(envelope.payload, sort_keys=True),
                persisted_at,
            ),
        )
        conn.execute(
            """
            UPDATE command_ledger
            SET state = ?
            WHERE command_id = ?
            """,
            (phase, command_id),
        )

    def latest_state(self, source_hardware_id: str, state_key: str) -> dict[str, Any] | None:
        with self._session() as conn:
            row = conn.execute(
                """
                SELECT payload_json FROM latest_state
                WHERE source_hardware_id = ? AND state_key = ?
                """,
                (source_hardware_id, state_key),
            ).fetchone()
            return json.loads(row["payload_json"]) if row else None

    def rebuild_latest_state(self) -> None:
        with self._session() as conn:
            conn.execute("DELETE FROM latest_state")
            rows = conn.execute(
                """
                SELECT envelope_json, persisted_at
                FROM messages
                WHERE kind = ?
                """,
                (MessageKind.STATE.value,),
            ).fetchall()
            envelopes = [
                (
                    FabricEnvelope.from_dict(json.loads(row["envelope_json"])),
                    str(row["persisted_at"]),
                )
                for row in rows
            ]
            envelopes.sort(
                key=lambda item: (
                    item[0].occurred_at or item[1],
                    item[0].source.session_id or "",
                    item[0].source.seq_local if item[0].source.seq_local is not None else -1,
                    item[0].message_id,
                )
            )
            for envelope, _ in envelopes:
                self._upsert_latest_state(conn, envelope)

    def command_state(self, command_id: str) -> str | None:
        with self._session() as conn:
            row = conn.execute(
                "SELECT state FROM command_ledger WHERE command_id = ?",
                (command_id,),
            ).fetchone()
            return str(row["state"]) if row else None

    def upsert_manifest(self, hardware_id: str, manifest_or_dict: NodeManifest | dict[str, Any]) -> None:
        manifest = (
            manifest_or_dict
            if isinstance(manifest_or_dict, NodeManifest)
            else NodeManifest.from_dict(manifest_or_dict)
        )
        hardware_id = hardware_id or manifest.hardware_id
        with self._session() as conn:
            self._upsert_manifest_record(conn, hardware_id, manifest, _to_iso(_utcnow()))

    def upsert_lease(self, hardware_id: str, lease_or_dict: RoleLease | dict[str, Any]) -> None:
        lease = (
            lease_or_dict
            if isinstance(lease_or_dict, RoleLease)
            else RoleLease.from_dict(lease_or_dict)
        )
        if not hardware_id:
            raise ValueError("hardware_id is required")
        with self._session() as conn:
            self._upsert_lease_record(conn, hardware_id, lease, _to_iso(_utcnow()))

    def issue_command(
        self,
        envelope_or_dict: FabricEnvelope | dict[str, Any],
        *,
        ingress_id: str = "local",
        queue_key: str | None = None,
    ) -> tuple[PersistAck, int]:
        envelope = (
            envelope_or_dict
            if isinstance(envelope_or_dict, FabricEnvelope)
            else FabricEnvelope.from_dict(envelope_or_dict)
        )
        if envelope.kind is not MessageKind.COMMAND:
            raise ValueError("issue_command requires kind=command")
        ack = self.ingest(envelope, ingress_id=ingress_id)
        queue_id = self.enqueue_outbound(
            envelope,
            queue_key=queue_key or (f"command:{envelope.command_id}" if envelope.command_id else None),
        )
        return ack, queue_id

    def latest_heartbeat(self, heartbeat_key: str) -> HeartbeatRecord | None:
        with self._session() as conn:
            row = conn.execute(
                """
                SELECT gateway_id, source_hardware_id, ingress_id, host_link, bearer, status, live, message_id, payload_json, updated_at
                FROM heartbeat_ledger
                WHERE heartbeat_key = ?
                """,
                (heartbeat_key,),
            ).fetchone()
            if row is None:
                return None
            return HeartbeatRecord(
                heartbeat_key=heartbeat_key,
                gateway_id=row["gateway_id"],
                source_hardware_id=str(row["source_hardware_id"]),
                ingress_id=str(row["ingress_id"]),
                host_link=row["host_link"],
                bearer=row["bearer"],
                status=row["status"],
                live=bool(row["live"]),
                message_id=str(row["message_id"]),
                updated_at=str(row["updated_at"]),
                payload=json.loads(str(row["payload_json"])),
            )

    def latest_fabric_summary(self, summary_key: str) -> FabricSummaryRecord | None:
        with self._session() as conn:
            row = conn.execute(
                """
                SELECT source_hardware_id, message_id, payload_json, updated_at
                FROM fabric_summary_latest
                WHERE summary_key = ?
                """,
                (summary_key,),
            ).fetchone()
            if row is None:
                return None
            return FabricSummaryRecord(
                summary_key=summary_key,
                source_hardware_id=str(row["source_hardware_id"]),
                message_id=str(row["message_id"]),
                updated_at=str(row["updated_at"]),
                payload=json.loads(str(row["payload_json"])),
            )

    def file_chunk_status(self, file_id: str) -> FileChunkStatus:
        with self._session() as conn:
            row = conn.execute(
                """
                SELECT COUNT(*) AS received_chunks,
                       COALESCE(MAX(total_chunks), 0) AS total_chunks,
                       COALESCE(MAX(chunk_index), -1) AS highest_chunk_index,
                       COALESCE(MIN(chunk_index), -1) AS min_chunk_index
                FROM file_chunk_ledger
                WHERE file_id = ?
                """,
                (file_id,),
            ).fetchone()
            received_chunks = int(row["received_chunks"])
            total_chunks = int(row["total_chunks"])
            latest = conn.execute(
                """
                SELECT COALESCE(message_id, '') AS last_message_id, COALESCE(updated_at, '') AS last_updated_at
                FROM file_chunk_ledger
                WHERE file_id = ?
                ORDER BY updated_at DESC, chunk_index DESC
                LIMIT 1
                """,
                (file_id,),
            ).fetchone()
            return FileChunkStatus(
                file_id=file_id,
                received_chunks=received_chunks,
                total_chunks=total_chunks,
                highest_chunk_index=int(row["highest_chunk_index"]),
                last_message_id=(str(latest["last_message_id"]) or None) if latest else None,
                last_updated_at=(str(latest["last_updated_at"]) or None) if latest else None,
                complete=(
                    total_chunks > 0
                    and received_chunks == total_chunks
                    and int(row["min_chunk_index"]) == 0
                    and int(row["highest_chunk_index"]) == total_chunks - 1
                ),
            )

    def count_events(self) -> int:
        with self._session() as conn:
            row = conn.execute("SELECT COUNT(*) AS count FROM event_ledger").fetchone()
            return int(row["count"])

    def enqueue_outbound(
        self,
        envelope_or_dict: FabricEnvelope | dict[str, Any],
        *,
        queue_key: str | None = None,
    ) -> int:
        envelope = (
            envelope_or_dict
            if isinstance(envelope_or_dict, FabricEnvelope)
            else FabricEnvelope.from_dict(envelope_or_dict)
        )
        now = _to_iso(_utcnow())
        queue_key = queue_key or f"message:{envelope.message_id}"
        with self._session() as conn:
            cursor = conn.execute(
                """
                INSERT INTO outbox_queue (
                    queue_key, message_id, target_kind, target_value, priority,
                    envelope_json, status, created_at, updated_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(queue_key) DO NOTHING
                """,
                (
                    queue_key,
                    envelope.message_id,
                    envelope.target.kind.value,
                    envelope.target.value,
                    envelope.priority.value,
                    json.dumps(envelope.to_dict(), sort_keys=True),
                    "queued",
                    now,
                    now,
                ),
            )
            if cursor.rowcount == 0:
                row = conn.execute(
                    "SELECT id FROM outbox_queue WHERE queue_key = ?",
                    (queue_key,),
                ).fetchone()
                if row is None:
                    raise ValueError("failed to resolve existing queue item")
                return int(row["id"])
            return int(cursor.lastrowid)

    def lease_outbound(
        self,
        *,
        worker_id: str,
        limit: int = 1,
        lease_seconds: int = 30,
        now: datetime | None = None,
    ) -> list[QueueLease]:
        now = now or _utcnow()
        lease_until = _to_iso(now + timedelta(seconds=lease_seconds))
        leased: list[QueueLease] = []
        with self._session() as conn:
            rows = conn.execute(
                """
                SELECT id, message_id, envelope_json
                FROM outbox_queue
                WHERE status = 'queued'
                ORDER BY CASE priority
                    WHEN ? THEN 0
                    WHEN ? THEN 1
                    WHEN ? THEN 2
                    ELSE 3
                END, created_at
                LIMIT ?
                """,
                (
                    Priority.CRITICAL.value,
                    Priority.CONTROL.value,
                    Priority.NORMAL.value,
                    limit,
                ),
            ).fetchall()
            for row in rows:
                conn.execute(
                    """
                    UPDATE outbox_queue
                    SET status = 'leased', lease_owner = ?, lease_until = ?, updated_at = ?
                    WHERE id = ?
                    """,
                    (worker_id, lease_until, _to_iso(now), row["id"]),
                )
                leased.append(
                    QueueLease(
                        queue_id=int(row["id"]),
                        message_id=row["message_id"],
                        lease_owner=worker_id,
                        lease_until=lease_until,
                        envelope=FabricEnvelope.from_dict(json.loads(row["envelope_json"])),
                    )
                )
        return leased

    def mark_sending(self, queue_id: int, *, worker_id: str) -> None:
        with self._session() as conn:
            cursor = conn.execute(
                """
                UPDATE outbox_queue
                SET status = 'sending', updated_at = ?
                WHERE id = ? AND lease_owner = ?
                """,
                (_to_iso(_utcnow()), queue_id, worker_id),
            )
            if cursor.rowcount != 1:
                raise ValueError("queue item is not leased by this worker")

    def ack_outbound(self, queue_id: int, *, worker_id: str) -> None:
        with self._session() as conn:
            cursor = conn.execute(
                """
                UPDATE outbox_queue
                SET status = 'acked', updated_at = ?
                WHERE id = ? AND lease_owner = ?
                """,
                (_to_iso(_utcnow()), queue_id, worker_id),
            )
            if cursor.rowcount != 1:
                raise ValueError("queue item is not leased by this worker")

    def move_to_dead(self, queue_id: int, *, worker_id: str, reason: str) -> None:
        with self._session() as conn:
            cursor = conn.execute(
                """
                UPDATE outbox_queue
                SET status = 'dead', dead_reason = ?, updated_at = ?
                WHERE id = ? AND lease_owner = ?
                """,
                (reason, _to_iso(_utcnow()), queue_id, worker_id),
            )
            if cursor.rowcount != 1:
                raise ValueError("queue item is not leased by this worker")

    def requeue_expired_leases(self, *, now: datetime | None = None) -> int:
        now = now or _utcnow()
        with self._session() as conn:
            conn.execute(
                """
                UPDATE outbox_queue
                SET status = 'dead',
                    dead_reason = 'retry_exhausted',
                    updated_at = ?
                WHERE status IN ('leased', 'sending')
                  AND lease_until IS NOT NULL
                  AND lease_until < ?
                  AND retry_count + 1 >= ?
                """,
                (_to_iso(now), _to_iso(now), self.max_retry_count),
            )
            cursor = conn.execute(
                """
                UPDATE outbox_queue
                SET status = 'queued',
                    lease_owner = NULL,
                    lease_until = NULL,
                    retry_count = retry_count + 1,
                    updated_at = ?
                WHERE status IN ('leased', 'sending')
                  AND lease_until IS NOT NULL
                  AND lease_until < ?
                  AND retry_count + 1 < ?
                """,
                (_to_iso(now), _to_iso(now), self.max_retry_count),
            )
            return int(cursor.rowcount)

    def queue_metrics(self) -> dict[str, int]:
        metrics = {
            "queued_count": 0,
            "leased_count": 0,
            "sending_count": 0,
            "acked_count": 0,
            "dead_count": 0,
        }
        with self._session() as conn:
            rows = conn.execute(
                "SELECT status, COUNT(*) AS count FROM outbox_queue GROUP BY status"
            ).fetchall()
            for row in rows:
                metrics[f"{row['status']}_count"] = int(row["count"])
        return metrics

    def _upsert_manifest(self, conn: sqlite3.Connection, envelope: FabricEnvelope, updated_at: str) -> None:
        manifest = NodeManifest.from_dict(envelope.payload)
        hardware_id = manifest.hardware_id or envelope.source.hardware_id
        self._upsert_manifest_record(conn, hardware_id, manifest, updated_at)

    def _upsert_manifest_record(
        self, conn: sqlite3.Connection, hardware_id: str, manifest: NodeManifest, updated_at: str
    ) -> None:
        payload = {
            "hardware_id": hardware_id,
            "device_family": manifest.device_family,
            "device_class": manifest.device_class,
            "power_class": manifest.power_class.value,
            "wake_class": manifest.wake_class.value,
            "supported_bearers": [item.value for item in manifest.supported_bearers],
            "allowed_network_roles": [item.value for item in manifest.allowed_network_roles],
            "firmware": manifest.firmware,
            "relay_capabilities": manifest.relay_capabilities,
        }
        conn.execute(
            """
            INSERT INTO node_manifest (
                hardware_id, power_class, wake_class, allowed_network_roles_json, supported_bearers_json, manifest_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(hardware_id) DO UPDATE SET
                power_class = excluded.power_class,
                wake_class = excluded.wake_class,
                allowed_network_roles_json = excluded.allowed_network_roles_json,
                supported_bearers_json = excluded.supported_bearers_json,
                manifest_json = excluded.manifest_json,
                updated_at = excluded.updated_at
            """,
            (
                hardware_id,
                manifest.power_class.value,
                manifest.wake_class.value,
                json.dumps([item.value for item in manifest.allowed_network_roles]),
                json.dumps([item.value for item in manifest.supported_bearers]),
                json.dumps(payload, sort_keys=True),
                updated_at,
            ),
        )

    def _upsert_lease(self, conn: sqlite3.Connection, envelope: FabricEnvelope, updated_at: str) -> None:
        lease = RoleLease.from_dict(envelope.payload)
        self._upsert_lease_record(conn, envelope.target.value, lease, updated_at)

    def _upsert_lease_record(
        self, conn: sqlite3.Connection, hardware_id: str, lease: RoleLease, updated_at: str
    ) -> None:
        manifest_row = conn.execute(
            "SELECT manifest_json FROM node_manifest WHERE hardware_id = ?",
            (hardware_id,),
        ).fetchone()
        if manifest_row is not None:
            manifest = NodeManifest.from_dict(json.loads(str(manifest_row["manifest_json"])))
            allowed_roles = {item.value for item in manifest.allowed_network_roles}
            if allowed_roles and lease.effective_role not in allowed_roles:
                raise ValueError(f"lease role {lease.effective_role} is not allowed for {hardware_id}")
            sleepy = "battery" in manifest.power_class.value.lower() or "sleep" in manifest.wake_class.value.lower()
            if sleepy and lease.effective_role in {"lora_relay", "mesh_router", "mesh_root", "ack_owner", "powered_leaf"}:
                raise ValueError(f"sleepy/battery node {hardware_id} must not take always-on role {lease.effective_role}")
        conn.execute(
            """
            INSERT INTO node_lease (
                hardware_id, logical_binding_id, fabric_short_id, effective_role, primary_bearer, fallback_bearer, lease_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(hardware_id) DO UPDATE SET
                logical_binding_id = excluded.logical_binding_id,
                fabric_short_id = excluded.fabric_short_id,
                effective_role = excluded.effective_role,
                primary_bearer = excluded.primary_bearer,
                fallback_bearer = excluded.fallback_bearer,
                lease_json = excluded.lease_json,
                updated_at = excluded.updated_at
            """,
            (
                hardware_id,
                lease.logical_binding_id,
                lease.fabric_short_id,
                lease.effective_role,
                lease.primary_bearer,
                lease.fallback_bearer,
                json.dumps(
                    {
                        "role_lease_id": lease.role_lease_id,
                        "site_id": lease.site_id,
                        "logical_binding_id": lease.logical_binding_id,
                        "fabric_short_id": lease.fabric_short_id,
                        "mesh_domain_id": lease.mesh_domain_id,
                        "effective_role": lease.effective_role,
                        "primary_bearer": lease.primary_bearer,
                        "fallback_bearer": lease.fallback_bearer,
                        "preferred_gateways": list(lease.preferred_gateways),
                        "preferred_mesh_roots": list(lease.preferred_mesh_roots),
                        "preferred_lora_parents": list(lease.preferred_lora_parents),
                    },
                    sort_keys=True,
                ),
                updated_at,
            ),
        )

    def _upsert_heartbeat(
        self, conn: sqlite3.Connection, envelope: FabricEnvelope, ingress_id: str, updated_at: str
    ) -> None:
        payload = envelope.payload
        heartbeat_key = str(payload.get("gateway_id") or envelope.source.hardware_id)
        ingress_meta = envelope.delivery.ingress_metadata if envelope.delivery else None
        conn.execute(
            """
            INSERT INTO heartbeat_ledger (
                heartbeat_key, gateway_id, source_hardware_id, ingress_id, host_link, bearer, status, live, message_id, payload_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(heartbeat_key) DO UPDATE SET
                gateway_id = excluded.gateway_id,
                source_hardware_id = excluded.source_hardware_id,
                ingress_id = excluded.ingress_id,
                host_link = excluded.host_link,
                bearer = excluded.bearer,
                status = excluded.status,
                live = excluded.live,
                message_id = excluded.message_id,
                payload_json = excluded.payload_json,
                updated_at = excluded.updated_at
            """,
            (
                heartbeat_key,
                payload.get("gateway_id"),
                envelope.source.hardware_id,
                ingress_id,
                ingress_meta.get("host_link") if isinstance(ingress_meta, dict) else None,
                ingress_meta.get("bearer") if isinstance(ingress_meta, dict) else None,
                payload.get("status"),
                1 if bool(payload.get("live")) else 0,
                envelope.message_id,
                json.dumps(payload, sort_keys=True),
                updated_at,
            ),
        )

    def _upsert_fabric_summary(self, conn: sqlite3.Connection, envelope: FabricEnvelope, updated_at: str) -> None:
        payload = envelope.payload
        summary_key = str(payload.get("summary_scope") or payload.get("site_id") or envelope.target.value or envelope.source.hardware_id)
        conn.execute(
            """
            INSERT INTO fabric_summary_latest (
                summary_key, source_hardware_id, message_id, payload_json, updated_at
            ) VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(summary_key) DO UPDATE SET
                source_hardware_id = excluded.source_hardware_id,
                message_id = excluded.message_id,
                payload_json = excluded.payload_json,
                updated_at = excluded.updated_at
            """,
            (
                summary_key,
                envelope.source.hardware_id,
                envelope.message_id,
                json.dumps(payload, sort_keys=True),
                updated_at,
            ),
        )

    def _upsert_file_chunk(self, conn: sqlite3.Connection, envelope: FabricEnvelope, updated_at: str) -> None:
        payload = envelope.payload
        file_id = str(payload.get("file_id") or envelope.correlation_id or f"{envelope.source.hardware_id}:{envelope.target.value}")
        chunk_index = payload.get("chunk_index")
        total_chunks = payload.get("total_chunks")
        if not isinstance(chunk_index, int) or not isinstance(total_chunks, int):
            raise ValueError("file_chunk.payload.chunk_index and total_chunks must be integers")
        conn.execute(
            """
            INSERT INTO file_chunk_ledger (
                file_id, chunk_index, total_chunks, source_hardware_id, message_id, payload_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(file_id, chunk_index) DO UPDATE SET
                total_chunks = excluded.total_chunks,
                source_hardware_id = excluded.source_hardware_id,
                message_id = excluded.message_id,
                payload_json = excluded.payload_json,
                updated_at = excluded.updated_at
            """,
            (
                file_id,
                chunk_index,
                total_chunks,
                envelope.source.hardware_id,
                envelope.message_id,
                json.dumps(payload, sort_keys=True),
                updated_at,
            ),
        )
