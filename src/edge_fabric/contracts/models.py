from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Any

from edge_fabric.contracts.enums import (
    Bearer,
    MessageKind,
    NetworkRole,
    PowerClass,
    Priority,
    TargetKind,
    WakeClass,
)


def _require_str(data: dict[str, Any], key: str) -> str:
    value = data.get(key)
    if not isinstance(value, str) or not value:
        raise ValueError(f"'{key}' must be a non-empty string")
    return value


def _optional_str(data: dict[str, Any], key: str) -> str | None:
    value = data.get(key)
    if value is None:
        return None
    if not isinstance(value, str) or not value:
        raise ValueError(f"'{key}' must be a non-empty string when present")
    return value


def _optional_int(data: dict[str, Any], key: str) -> int | None:
    value = data.get(key)
    if value is None:
        return None
    if not isinstance(value, int):
        raise ValueError(f"'{key}' must be an integer when present")
    return value


def _enum_value(enum_cls: type, raw: str, label: str):
    try:
        return enum_cls(raw)
    except ValueError as exc:
        raise ValueError(f"invalid {label}: {raw}") from exc


def _list_of_str(data: dict[str, Any], key: str) -> tuple[str, ...]:
    value = data.get(key)
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise ValueError(f"'{key}' must be a list[str]")
    return tuple(value)


@dataclass(frozen=True)
class SourceRef:
    hardware_id: str
    session_id: str | None = None
    seq_local: int | None = None
    fabric_short_id: int | None = None
    network_role: str | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "SourceRef":
        return cls(
            hardware_id=_require_str(data, "hardware_id"),
            session_id=_optional_str(data, "session_id"),
            seq_local=_optional_int(data, "seq_local"),
            fabric_short_id=_optional_int(data, "fabric_short_id"),
            network_role=_optional_str(data, "network_role"),
        )


@dataclass(frozen=True)
class TargetRef:
    kind: TargetKind
    value: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "TargetRef":
        return cls(
            kind=_enum_value(TargetKind, _require_str(data, "kind"), "target kind"),
            value=_require_str(data, "value"),
        )


@dataclass(frozen=True)
class DeliverySpec:
    route_class: str | None = None
    allow_relay: bool | None = None
    allow_redundant: bool | None = None
    hop_limit: int | None = None
    expires_at: str | None = None
    ingress_metadata: dict[str, Any] | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any] | None) -> "DeliverySpec | None":
        if data is None:
            return None
        if not isinstance(data, dict):
            raise ValueError("'delivery' must be an object")
        hop_limit = _optional_int(data, "hop_limit")
        if hop_limit is not None and not 0 <= hop_limit <= 8:
            raise ValueError("'hop_limit' must be between 0 and 8")
        return cls(
            route_class=_optional_str(data, "route_class"),
            allow_relay=data.get("allow_relay"),
            allow_redundant=data.get("allow_redundant"),
            hop_limit=hop_limit,
            expires_at=_optional_str(data, "expires_at"),
            ingress_metadata=data.get("ingress_metadata"),
        )


@dataclass(frozen=True)
class MeshMeta:
    mesh_domain_id: str | None = None
    hop_count: int | None = None
    last_hop: str | None = None
    ingress_gateway_id: str | None = None
    relay_trace: tuple[str, ...] = ()

    @classmethod
    def from_dict(cls, data: dict[str, Any] | None) -> "MeshMeta | None":
        if data is None:
            return None
        if not isinstance(data, dict):
            raise ValueError("'mesh_meta' must be an object")
        hop_count = _optional_int(data, "hop_count")
        if hop_count is not None and hop_count < 0:
            raise ValueError("'hop_count' must be >= 0")
        relay_trace_raw = data.get("relay_trace", [])
        if not isinstance(relay_trace_raw, list) or not all(
            isinstance(item, str) for item in relay_trace_raw
        ):
            raise ValueError("'relay_trace' must be a list[str]")
        return cls(
            mesh_domain_id=_optional_str(data, "mesh_domain_id"),
            hop_count=hop_count,
            last_hop=_optional_str(data, "last_hop"),
            ingress_gateway_id=_optional_str(data, "ingress_gateway_id"),
            relay_trace=tuple(relay_trace_raw),
        )


@dataclass(frozen=True)
class AckInfo:
    ack_phase: str | None = None
    acked_message_id: str | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any] | None) -> "AckInfo | None":
        if data is None:
            return None
        if not isinstance(data, dict):
            raise ValueError("'ack' must be an object")
        return cls(
            ack_phase=_optional_str(data, "ack_phase"),
            acked_message_id=_optional_str(data, "acked_message_id"),
        )


@dataclass(frozen=True)
class FabricEnvelope:
    schema_version: str
    message_id: str
    kind: MessageKind
    priority: Priority
    source: SourceRef
    target: TargetRef
    payload: dict[str, Any]
    event_id: str | None = None
    command_id: str | None = None
    correlation_id: str | None = None
    occurred_at: str | None = None
    delivery: DeliverySpec | None = None
    mesh_meta: MeshMeta | None = None
    ack: AckInfo | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "FabricEnvelope":
        source = data.get("source")
        target = data.get("target")
        payload = data.get("payload")
        if not isinstance(source, dict):
            raise ValueError("'source' must be an object")
        if not isinstance(target, dict):
            raise ValueError("'target' must be an object")
        if not isinstance(payload, dict):
            raise ValueError("'payload' must be an object")
        return cls(
            schema_version=_require_str(data, "schema_version"),
            message_id=_require_str(data, "message_id"),
            kind=_enum_value(MessageKind, _require_str(data, "kind"), "message kind"),
            priority=_enum_value(Priority, _require_str(data, "priority"), "priority"),
            source=SourceRef.from_dict(source),
            target=TargetRef.from_dict(target),
            payload=payload,
            event_id=_optional_str(data, "event_id"),
            command_id=_optional_str(data, "command_id"),
            correlation_id=_optional_str(data, "correlation_id"),
            occurred_at=_optional_str(data, "occurred_at"),
            delivery=DeliverySpec.from_dict(data.get("delivery")),
            mesh_meta=MeshMeta.from_dict(data.get("mesh_meta")),
            ack=AckInfo.from_dict(data.get("ack")),
        )

    def to_dict(self) -> dict[str, Any]:
        result = asdict(self)
        result["kind"] = self.kind.value
        result["priority"] = self.priority.value
        result["source"] = asdict(self.source)
        result["target"] = {"kind": self.target.kind.value, "value": self.target.value}
        if self.delivery is not None:
            result["delivery"] = asdict(self.delivery)
        if self.mesh_meta is not None:
            result["mesh_meta"] = asdict(self.mesh_meta)
            result["mesh_meta"]["relay_trace"] = list(self.mesh_meta.relay_trace)
        if self.ack is not None:
            result["ack"] = asdict(self.ack)
        return {key: value for key, value in result.items() if value is not None}

    @property
    def dedupe_key(self) -> str:
        if self.kind is MessageKind.EVENT and self.event_id:
            return f"event:{self.event_id}"
        if self.kind is MessageKind.COMMAND and self.command_id:
            return f"command:{self.command_id}"
        return f"message:{self.message_id}"


@dataclass(frozen=True)
class NodeManifest:
    hardware_id: str
    device_family: str
    power_class: PowerClass
    wake_class: WakeClass
    supported_bearers: tuple[Bearer, ...]
    allowed_network_roles: tuple[NetworkRole, ...]
    firmware: dict[str, Any]
    device_class: str | None = None
    relay_capabilities: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "NodeManifest":
        supported = tuple(
            _enum_value(Bearer, item, "bearer") for item in _list_of_str(data, "supported_bearers")
        )
        roles = tuple(
            _enum_value(NetworkRole, item, "network role")
            for item in _list_of_str(data, "allowed_network_roles")
        )
        firmware = data.get("firmware")
        if not isinstance(firmware, dict):
            raise ValueError("'firmware' must be an object")
        return cls(
            hardware_id=_require_str(data, "hardware_id"),
            device_family=_require_str(data, "device_family"),
            device_class=_optional_str(data, "device_class"),
            power_class=_enum_value(PowerClass, _require_str(data, "power_class"), "power_class"),
            wake_class=_enum_value(WakeClass, _require_str(data, "wake_class"), "wake_class"),
            supported_bearers=supported,
            allowed_network_roles=roles,
            firmware=firmware,
            relay_capabilities=data.get("relay_capabilities", {}),
        )


@dataclass(frozen=True)
class RoleLease:
    role_lease_id: str
    site_id: str
    logical_binding_id: str
    effective_role: str
    primary_bearer: str
    fabric_short_id: int | None = None
    mesh_domain_id: str | None = None
    fallback_bearer: str | None = None
    preferred_gateways: tuple[str, ...] = ()
    preferred_mesh_roots: tuple[str, ...] = ()
    preferred_lora_parents: tuple[str, ...] = ()

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "RoleLease":
        return cls(
            role_lease_id=_require_str(data, "role_lease_id"),
            site_id=_require_str(data, "site_id"),
            logical_binding_id=_require_str(data, "logical_binding_id"),
            fabric_short_id=_optional_int(data, "fabric_short_id"),
            mesh_domain_id=_optional_str(data, "mesh_domain_id"),
            effective_role=_require_str(data, "effective_role"),
            primary_bearer=_require_str(data, "primary_bearer"),
            fallback_bearer=_optional_str(data, "fallback_bearer"),
            preferred_gateways=tuple(data.get("preferred_gateways", [])),
            preferred_mesh_roots=tuple(data.get("preferred_mesh_roots", [])),
            preferred_lora_parents=tuple(data.get("preferred_lora_parents", [])),
        )
