from __future__ import annotations

import sys
from pathlib import Path


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    required = [
        root / "README.md",
        root / "pyproject.toml",
        root / "src" / "edge_fabric",
        root / "contracts" / "protocol" / "jp-safe-profiles.json",
        root / "contracts" / "protocol" / "compact-codecs.json",
        root / "contracts" / "protocol" / "heartbeat-wire.json",
        root / "contracts" / "protocol" / "sleepy-command-policy.json",
        root / "cmd" / "sleepy-cycle-demo",
        root / "docs" / "KNOWN_LIMITATIONS.md",
        root / "docs" / "SUPPORT_MATRIX.md",
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

    print("Repository layout looks healthy.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
