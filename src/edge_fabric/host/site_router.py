from __future__ import annotations

import json
import sqlite3
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from edge_fabric.contracts.enums import AckPhase
from edge_fabric.contracts.enums import MessageKind, Priority
from edge_fabric.contracts.models import FabricEnvelope


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
                        INSERT INTO command_ledger (command_id, message_id, state, envelope_json, created_at)
                        VALUES (?, ?, ?, ?, ?)
                        """,
                        (
                            envelope.command_id,
                            envelope.message_id,
                            "issued",
                            json.dumps(envelope.to_dict(), sort_keys=True),
                            persisted_at,
                        ),
                    )
                elif envelope.kind is MessageKind.COMMAND_RESULT:
                    self._record_command_result(conn, envelope, persisted_at)

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
            current_key = (
                existing["occurred_at"] or "",
                existing["session_id"] or "",
                int(existing["seq_local"]) if existing["seq_local"] is not None else -1,
                "",
            )
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
        return (
            envelope.occurred_at or "",
            envelope.source.session_id or "",
            envelope.source.seq_local if envelope.source.seq_local is not None else -1,
            envelope.message_id,
        )

    def _record_command_result(
        self,
        conn: sqlite3.Connection,
        envelope: FabricEnvelope,
        persisted_at: str,
    ) -> None:
        command_id = envelope.command_id or envelope.payload.get("command_id")
        if not isinstance(command_id, str) or not command_id:
            return
        phase = str(envelope.payload.get("phase", "accepted"))
        if phase not in {item.value for item in AckPhase}:
            raise ValueError(f"invalid command_result phase: {phase}")
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
