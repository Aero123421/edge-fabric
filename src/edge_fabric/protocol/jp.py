from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class JpSafeProfile:
    name: str
    bandwidth_khz: int
    spreading_factor: int
    intended_use: str
    total_payload_cap: int
    cad_only_allowed: bool


_REPO_ROOT = Path(__file__).resolve().parents[3]
_JP_PROFILES_PATH = _REPO_ROOT / "contracts" / "protocol" / "jp-safe-profiles.json"
_JP_PROFILES_DATA = json.loads(_JP_PROFILES_PATH.read_text(encoding="utf-8"))

_INTENDED_USE = {
    "JP125_LONG_SF10": "critical alert / summary heartbeat",
    "JP125_BAL_SF9": "sparse event / state / result",
    "JP250_FAST_SF8": "short-range powered fallback / bridge uplink",
    "JP250_CTRL_SF9": "compact control / powered fallback",
}

JP_SAFE_PROFILES: dict[str, JpSafeProfile] = {
    item["name"]: JpSafeProfile(
        name=item["name"],
        bandwidth_khz=item["bandwidth_khz"],
        spreading_factor=item["spreading_factor"],
        intended_use=_INTENDED_USE[item["name"]],
        total_payload_cap=item["total_payload_cap"],
        cad_only_allowed=bool(_JP_PROFILES_DATA["cad_only_allowed"]),
    )
    for item in _JP_PROFILES_DATA["profiles"]
}

DIRECT_UPLINK_OVERHEAD_BYTES = 14
RELAYED_UPLINK_OVERHEAD_BYTES = 18


def body_cap_for_profile(profile_name: str, *, relayed: bool = False) -> int:
    profile = JP_SAFE_PROFILES.get(profile_name)
    if profile is None:
        raise ValueError(f"unknown JP-safe profile: {profile_name}")
    overhead = RELAYED_UPLINK_OVERHEAD_BYTES if relayed else DIRECT_UPLINK_OVERHEAD_BYTES
    return profile.total_payload_cap - overhead


def validate_body_fit(profile_name: str, body_size: int, *, relayed: bool = False) -> bool:
    return body_size <= body_cap_for_profile(profile_name, relayed=relayed)
