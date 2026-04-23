from __future__ import annotations

import subprocess
import sys
from pathlib import Path


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    artifacts = root / ".artifacts"
    artifacts.mkdir(exist_ok=True)
    output = artifacts / "edge-fabric-src.zip"
    if output.exists():
        output.unlink()
    result = subprocess.run(
        ["git", "archive", "--format=zip", f"--output={output}", "HEAD"],
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        if result.stderr:
            print(result.stderr.strip(), file=sys.stderr)
        return result.returncode
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
