from __future__ import annotations

import json
import sys
from pathlib import Path


def load_json(path: Path) -> dict:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise RuntimeError(f"Missing JSON artifact: {path}") from exc
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Invalid JSON in {path}: {exc}") from exc


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    required = [
        root / "README.md",
        root / "pyproject.toml",
        root / "src" / "edge_fabric",
        root / "contracts" / "protocol" / "jp-safe-profiles.json",
        root / "contracts" / "protocol" / "onair-v1.json",
        root / "contracts" / "protocol" / "compact-codecs.json",
        root / "contracts" / "protocol" / "heartbeat-wire.json",
        root / "contracts" / "protocol" / "sleepy-command-policy.json",
        root / "cmd" / "sleepy-cycle-demo",
        root / "docs" / "KNOWN_LIMITATIONS.md",
        root / "docs" / "SUPPORT_MATRIX.md",
        root / ".gitattributes",
        root / "edge-fabric-esp32sx1262-v3-mesh",
    ]

    print(f"Python: {sys.version.split()[0]}")
    if sys.version_info < (3, 12):
        print("Python 3.12+ is required")
        return 1

    missing = [path for path in required if not path.exists()]
    if missing:
        for path in missing:
            print(f"Missing: {path}")
        return 1

    forbidden_root_artifacts = [
        root / ".tmp-host-agent.db",
        root / ".tmp-site-router.db",
        root / "direct-slice-demo.db",
        root / "sleepy-cycle-demo.db",
    ]
    present_forbidden = [path for path in forbidden_root_artifacts if path.exists()]
    if present_forbidden:
        for path in present_forbidden:
            print(f"Release artifact must not live at repo root: {path.name}")
        return 1

    sleepy_demo = (root / "cmd" / "sleepy-cycle-demo" / "main.go").read_text(encoding="utf-8")
    stale_tokens = ['[]byte("D|', '[]byte("P|', '[]byte("R|']
    if any(token in sleepy_demo for token in stale_tokens):
        print("sleepy-cycle-demo is still using legacy pipe-delimited payloads")
        return 1

    try:
        sleepy_fixture = load_json(root / "contracts" / "fixtures" / "command-sleepy-threshold-set.json")
        maintenance_fixture = load_json(root / "contracts" / "fixtures" / "command-sleepy-maintenance-sync.json")
        sleepy_manifest = load_json(root / "contracts" / "fixtures" / "manifest-sleepy-leaf.json")
        sleepy_policy = load_json(root / "contracts" / "protocol" / "sleepy-command-policy.json")
    except RuntimeError as exc:
        print(exc)
        return 1
    if sleepy_fixture["delivery"]["route_class"] != "sleepy_tiny_control":
        print("sleepy threshold fixture must use delivery.route_class=sleepy_tiny_control")
        return 1
    payload = sleepy_fixture["payload"]
    if "command_token" not in payload or not isinstance(payload["command_token"], int):
        print("sleepy threshold fixture must carry integer payload.command_token")
        return 1
    if "value" not in payload or not isinstance(payload["value"], int):
        print("sleepy threshold fixture must carry integer payload.value")
        return 1
    if "threshold" in payload:
        print("sleepy threshold fixture must not use legacy payload.threshold")
        return 1
    if sleepy_fixture["target"]["value"] != sleepy_manifest["hardware_id"]:
        print("sleepy threshold fixture target must match manifest-sleepy-leaf hardware_id")
        return 1
    if maintenance_fixture["target"]["value"] != sleepy_manifest["hardware_id"]:
        print("sleepy maintenance fixture target must match manifest-sleepy-leaf hardware_id")
        return 1
    if maintenance_fixture["delivery"]["route_class"] not in sleepy_policy["maintenance_route_classes"]:
        print("sleepy maintenance fixture route_class must stay within maintenance_route_classes")
        return 1
    if maintenance_fixture["payload"]["command_name"] not in sleepy_policy["maintenance_only_commands"]:
        print("sleepy maintenance fixture command_name must stay within maintenance_only_commands")
        return 1

    readme = (root / "README.md").read_text(encoding="utf-8")
    if "contracts/protocol/onair-v1.json" not in readme:
        print("README must point to contracts/protocol/onair-v1.json as binary on-air artifact")
        return 1
    if "binary on-air の正本ではなく" not in readme:
        print("README must mark Python reference as non-authoritative for binary on-air")
        return 1

    print("Repository layout looks healthy.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
