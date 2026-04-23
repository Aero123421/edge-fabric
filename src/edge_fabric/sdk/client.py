from __future__ import annotations

import time
from typing import Any
from uuid import uuid4

from edge_fabric.contracts.enums import MessageKind, Priority, TargetKind
from edge_fabric.contracts.models import FabricEnvelope, NodeManifest, RoleLease, SourceRef, TargetRef
from edge_fabric.host.site_router import PersistAck, SiteRouter


class LocalSiteRouterClient:
    """Thin local client that preserves the route-insensitive API shape."""

    def __init__(self, router: SiteRouter, *, source_id: str = "local-client") -> None:
        self.router = router
        self.source_id = source_id

    def publish_state(
        self,
        *,
        hardware_id: str,
        state_key: str,
        payload: dict[str, Any],
        priority: Priority = Priority.NORMAL,
    ) -> PersistAck:
        envelope = FabricEnvelope(
            schema_version="1.0.0",
            message_id=_new_message_id(),
            kind=MessageKind.STATE,
            priority=priority,
            source=SourceRef(hardware_id=hardware_id),
            target=TargetRef(kind=TargetKind.SERVICE, value="state"),
            payload={"state_key": state_key, **payload},
        )
        return self.router.ingest(envelope)

    def emit_event(
        self,
        *,
        hardware_id: str,
        event_id: str,
        service: str,
        payload: dict[str, Any],
        priority: Priority = Priority.CRITICAL,
    ) -> PersistAck:
        envelope = FabricEnvelope(
            schema_version="1.0.0",
            message_id=_new_message_id(),
            kind=MessageKind.EVENT,
            priority=priority,
            source=SourceRef(hardware_id=hardware_id),
            target=TargetRef(kind=TargetKind.SERVICE, value=service),
            event_id=event_id,
            payload=payload,
        )
        return self.router.ingest(envelope)

    def issue_command(
        self,
        *,
        command_id: str,
        target_node: str,
        payload: dict[str, Any],
        priority: Priority = Priority.CONTROL,
    ) -> tuple[PersistAck, int]:
        envelope = FabricEnvelope(
            schema_version="1.0.0",
            message_id=_new_message_id(),
            kind=MessageKind.COMMAND,
            priority=priority,
            source=SourceRef(hardware_id=self.source_id),
            target=TargetRef(kind=TargetKind.NODE, value=target_node),
            command_id=command_id,
            payload=payload,
        )
        return self.router.issue_command(envelope, ingress_id="sdk-local")

    def observe_command(self, command_id: str) -> str | None:
        return self.router.command_state(command_id)

    def register_manifest(self, hardware_id: str, manifest_or_dict: NodeManifest | dict[str, Any]) -> None:
        self.router.upsert_manifest(hardware_id, manifest_or_dict)

    def register_lease(self, hardware_id: str, lease_or_dict: RoleLease | dict[str, Any]) -> None:
        self.router.upsert_lease(hardware_id, lease_or_dict)


def _new_message_id() -> str:
    return f"msg-{time.time_ns()}-{uuid4().hex[:8]}"
