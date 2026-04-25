from __future__ import annotations

import argparse
import subprocess
import sys
import zipfile
from pathlib import Path


FORBIDDEN_PREFIXES = (
    ".git/",
    ".tools/",
    ".tmp/",
    ".pytest_cache/",
    ".artifacts/",
    "__pycache__/",
    "dist/",
    "build/",
)
FORBIDDEN_FILES = (
    ".tmp-host-agent.db",
    ".tmp-site-router.db",
    "direct-slice-demo.db",
    "direct-slice-demo.spool.jsonl",
    "direct-slice-demo.spool.heartbeat.json",
    "sleepy-cycle-demo.db",
    "sleepy-cycle-demo.spool.jsonl",
    "sleepy-cycle-demo.spool.heartbeat.json",
    "host-agent-spool.heartbeat.json",
    "host-agent-spool.jsonl",
    "site-router.db",
)
FORBIDDEN_SUFFIXES = (".pyc", ".pyo", ".egg-info", ".egg-info/", ".db-wal", ".db-shm")
FORBIDDEN_PATH_PARTS = (".egg-info/",)


def validate_export(output: Path) -> None:
    with zipfile.ZipFile(output) as archive:
        names = archive.namelist()
    for prefix in FORBIDDEN_PREFIXES:
        if any(name == prefix or name.startswith(prefix) or f"/{prefix}" in name for name in names):
            raise RuntimeError(f"forbidden export prefix detected: {prefix}")
    for filename in FORBIDDEN_FILES:
        if any(name == filename or name.endswith("/" + filename) for name in names):
            raise RuntimeError(f"forbidden export file detected: {filename}")
    for suffix in FORBIDDEN_SUFFIXES:
        if any(name.endswith(suffix) for name in names):
            raise RuntimeError(f"forbidden export suffix detected: {suffix}")
    for path_part in FORBIDDEN_PATH_PARTS:
        if any(path_part in name for name in names):
            raise RuntimeError(f"forbidden export path component detected: {path_part}")

def ensure_clean_worktree(root: Path) -> None:
    result = subprocess.run(
        ["git", "status", "--porcelain"],
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError("unable to inspect git worktree state")
    if result.stdout.strip():
        raise RuntimeError("refusing to export from a dirty worktree; commit or stash changes first")


def is_git_checkout(root: Path) -> bool:
    result = subprocess.run(
        ["git", "rev-parse", "--is-inside-work-tree"],
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    return result.returncode == 0 and result.stdout.strip() == "true"


def should_skip_path(relative: str) -> bool:
    normalized = relative.replace("\\", "/")
    for prefix in FORBIDDEN_PREFIXES:
        if normalized == prefix.rstrip("/") or normalized.startswith(prefix) or f"/{prefix}" in normalized:
            return True
    if normalized in FORBIDDEN_FILES or any(normalized.endswith("/" + name) for name in FORBIDDEN_FILES):
        return True
    if normalized.endswith(FORBIDDEN_SUFFIXES):
        return True
    return any(path_part in normalized for path_part in FORBIDDEN_PATH_PARTS)


def export_from_filesystem(root: Path, output: Path) -> None:
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(root.rglob("*")):
            if not path.is_file():
                continue
            relative = path.relative_to(root).as_posix()
            if should_skip_path(relative):
                continue
            archive.write(path, relative)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--allow-dirty", action="store_true")
    args = parser.parse_args(argv)
    root = Path(__file__).resolve().parent.parent
    git_checkout = is_git_checkout(root)
    if git_checkout and not args.allow_dirty:
        try:
            ensure_clean_worktree(root)
        except RuntimeError as exc:
            print(str(exc), file=sys.stderr)
            return 1
    artifacts = root / ".artifacts"
    artifacts.mkdir(exist_ok=True)
    output = artifacts / "edge-fabric-src.zip"
    if output.exists():
        output.unlink()
    if git_checkout:
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
    else:
        export_from_filesystem(root, output)
    try:
        validate_export(output)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
