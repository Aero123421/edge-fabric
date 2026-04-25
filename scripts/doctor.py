from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path


def load_json(path: Path) -> dict:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise RuntimeError(f"Missing JSON artifact: {path}") from exc
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Invalid JSON in {path}: {exc}") from exc


TOOL_TIMEOUT_SECONDS = 5
IDF_TOOL_TIMEOUT_SECONDS = 30


def optional_tool_version(
    command: list[str],
    env: dict[str, str] | None = None,
    timeout_seconds: int = TOOL_TIMEOUT_SECONDS,
) -> str:
    executable = shutil.which(command[0])
    if executable is None:
        return "missing"
    run_command = [executable, *command[1:]]
    if Path(executable).suffix.lower() == ".py":
        run_command = [sys.executable, executable, *command[1:]]
    try:
        result = subprocess.run(
            run_command,
            capture_output=True,
            text=True,
            check=False,
            timeout=timeout_seconds,
            env=env,
        )
    except OSError:
        return "error"
    except subprocess.TimeoutExpired:
        return "timeout"
    if result.returncode != 0:
        return "error"
    return (result.stdout or result.stderr).strip().splitlines()[0]

def parse_go_version_line(version_line: str) -> tuple[int, int] | None:
    match = re.search(r"go version go(\d+)\.(\d+)", version_line)
    if match is None:
        return None
    return int(match.group(1)), int(match.group(2))

def validate_legacy_payload_boundaries(root: Path) -> str | None:
    allowed = {
        (root / "src" / "edge_fabric" / "host" / "agent.py").resolve(),
        (root / "tests" / "test_host_agent.py").resolve(),
        (root / "scripts" / "doctor.py").resolve(),
    }
    source_roots = [
        root / "cmd",
        root / "internal",
        root / "firmware",
        root / "src",
        root / "tests",
        root / "scripts",
    ]
    markers = (
        'b"R|',
        'b"S|',
        'b"D|',
        'b"P|',
        'b"E|',
        'b"H|',
        'b"C|',
        '[]byte("R|',
        '[]byte("S|',
        '[]byte("D|',
        '[]byte("P|',
        '[]byte("E|',
        '[]byte("H|',
        '[]byte("C|',
        '"R|',
        '"S|',
        '"D|',
        '"P|',
        '"E|',
        '"H|',
        '"C|',
    )
    for source_root in source_roots:
        if not source_root.exists():
            continue
        for path in source_root.rglob("*"):
            if not path.is_file():
                continue
            if path.suffix.lower() not in {".py", ".go", ".c", ".h"}:
                continue
            try:
                text = path.read_text(encoding="utf-8")
            except UnicodeDecodeError:
                continue
            if any(marker in text for marker in markers) and path.resolve() not in allowed:
                return f"legacy pipe payload markers must stay confined to reference track: {path}"
    return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--track",
        choices=("layout", "python", "go", "firmware", "all"),
        default="layout",
        help="Select which toolchain track must be enforced. Default only validates layout/contracts.",
    )
    parser.add_argument("--require-go", action="store_true")
    parser.add_argument("--require-idf", action="store_true")
    args = parser.parse_args(argv)
    require_go = args.require_go or args.track in {"go", "all"}
    require_idf = args.require_idf or args.track in {"firmware", "all"}

    root = Path(__file__).resolve().parent.parent
    required = [
        root / "README.md",
        root / "flake.nix",
        root / "pyproject.toml",
        root / "src" / "edge_fabric",
        root / "contracts" / "protocol" / "jp-safe-profiles.json",
        root / "contracts" / "protocol" / "onair-v1.json",
        root / "contracts" / "protocol" / "compact-codecs.json",
        root / "contracts" / "protocol" / "heartbeat-wire.json",
        root / "contracts" / "protocol" / "sleepy-command-policy.json",
        root / "cmd" / "sleepy-cycle-demo",
        root / "docs" / "KNOWN_LIMITATIONS.md",
        root / "docs" / "NIX.md",
        root / "docs" / "SUPPORT_MATRIX.md",
        root / ".gitattributes",
        root / "edge-fabric-esp32sx1262-v3-mesh",
    ]

    print(f"Python: {sys.version.split()[0]}")
    if sys.version_info < (3, 12):
        print("Python 3.12+ is required")
        return 1
    go_env = dict(os.environ)
    go_env["GOTOOLCHAIN"] = "local"
    go_version = optional_tool_version(["go", "version"], env=go_env)
    idf_version = optional_tool_version(["idf.py", "--version"], timeout_seconds=IDF_TOOL_TIMEOUT_SECONDS)
    print(f"Go: {go_version}")
    print(f"idf.py: {idf_version}")
    if require_go and go_version in {"missing", "error", "timeout"}:
        print("Go toolchain is required and must be runnable for this doctor mode")
        return 1
    if require_idf and idf_version in {"missing", "error", "timeout"}:
        print("idf.py is required and must be runnable for this doctor mode")
        return 1
    parsed_go = parse_go_version_line(go_version)
    if require_go and parsed_go is None:
        print("Go version could not be parsed")
        return 1
    if parsed_go is not None and parsed_go < (1, 25):
        if require_go:
            print("Go 1.25+ is required")
            return 1
        print("Warning: Go 1.25+ is required for the Go mainline track")

    missing = [path for path in required if not path.exists()]
    if missing:
        for path in missing:
            print(f"Missing: {path}")
        return 1

    forbidden_root_artifacts = [
        root / ".tmp-host-agent.db",
        root / ".tmp-site-router.db",
        root / "direct-slice-demo.db",
        root / "direct-slice-demo.spool.jsonl",
        root / "direct-slice-demo.spool.heartbeat.json",
        root / "sleepy-cycle-demo.db",
        root / "sleepy-cycle-demo.spool.jsonl",
        root / "sleepy-cycle-demo.spool.heartbeat.json",
        root / "host-agent-spool.heartbeat.json",
    ]
    present_forbidden = [path for path in forbidden_root_artifacts if path.exists()]
    if present_forbidden:
        for path in present_forbidden:
            print(f"Release artifact must not live at repo root: {path.name}")
        return 1

    sleepy_demo = (root / "cmd" / "sleepy-cycle-demo" / "main.go").read_text(encoding="utf-8")
    stale_tokens = [
        '[]byte("D|',
        '[]byte("P|',
        '[]byte("R|',
        '[]byte("E|',
        '[]byte("H|',
        '[]byte("C|',
    ]
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
    if ".tools\\go-sdk\\bin\\go.exe" in readme or "./.tools/go-sdk/bin/go.exe" in readme:
        print("README must not rely on maintainer-local .tools Go path")
        return 1
    if "nix develop" not in readme:
        print("README must document the Nix development shell")
        return 1
    legacy_boundary_error = validate_legacy_payload_boundaries(root)
    if legacy_boundary_error:
        print(legacy_boundary_error)
        return 1

    print("Repository layout looks healthy.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
