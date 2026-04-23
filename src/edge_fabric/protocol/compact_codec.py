from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class FrameTypeSpec:
    name: str
    wire_shape: str


@dataclass(frozen=True)
class LogicalKindSpec:
    kind: str
    shape_by_frame_type: dict[str, str]
    ingest_behavior: str | None = None


_REPO_ROOT = Path(__file__).resolve().parents[3]
_REGISTRY_PATH = _REPO_ROOT / "contracts" / "protocol" / "compact-codecs.json"
_REGISTRY = json.loads(_REGISTRY_PATH.read_text(encoding="utf-8"))

FRAME_TYPES: dict[int, FrameTypeSpec] = {
    int(frame_type): FrameTypeSpec(
        name=spec["name"],
        wire_shape=spec["wire_shape"],
    )
    for frame_type, spec in _REGISTRY["frame_types"].items()
}

LOGICAL_KINDS: dict[str, LogicalKindSpec] = {
    logical_key: LogicalKindSpec(
        kind=spec["kind"],
        shape_by_frame_type=dict(spec.get("shape_by_frame_type", {})),
        ingest_behavior=spec.get("ingest_behavior"),
    )
    for logical_key, spec in _REGISTRY["logical_kinds"].items()
}


def frame_type_spec(frame_type: int) -> FrameTypeSpec:
    try:
        return FRAME_TYPES[frame_type]
    except KeyError as exc:
        raise ValueError(f"unknown compact codec frame type: {frame_type}") from exc


def shape_for(logical_key: str, frame_type: int) -> str:
    try:
        spec = LOGICAL_KINDS[logical_key]
    except KeyError as exc:
        raise ValueError(f"unknown compact logical kind: {logical_key}") from exc
    try:
        return spec.shape_by_frame_type[str(frame_type)]
    except KeyError as exc:
        raise ValueError(
            f"no compact shape for logical kind {logical_key} and frame type {frame_type}"
        ) from exc
