"""Archive format invariants beyond checksum integrity (no archive extraction)."""
import re
from pathlib import Path


def validate(directory, manifest, error, safe_path, digest):
    def require(condition, message="Invalid checkpoint structure"):
        if not condition:
            raise error(message)

    require(isinstance(manifest, dict))
    require(set(manifest) == {"schema_version", "kind", "inventory", "files", "included_ignored", "excluded_ignored", "artifacts"})
    info = manifest["inventory"]
    require(isinstance(info, dict) and info.get("kind") == "inventory" and info.get("schema_version") == 1)
    require(isinstance(info.get("refs"), dict))
    require(info.get("head") is None or bool(re.fullmatch(r"[a-f0-9]{40}|[a-f0-9]{64}", info["head"])))
    require(all(isinstance(v, str) and re.fullmatch(r"[a-f0-9]{40}|[a-f0-9]{64}", v) for v in info["refs"].values()))
    for key in ("included_ignored", "excluded_ignored"):
        require(isinstance(manifest[key], list))
        for name in manifest[key]:
            require(isinstance(name, str))
            safe_path(name)
    artifacts = manifest["artifacts"]
    require(isinstance(artifacts, dict))
    for name, checksum in artifacts.items():
        require(isinstance(name, str) and isinstance(checksum, str))
        safe_path(name)
        require(bool(re.fullmatch(r"[a-f0-9]{64}", checksum)))
    required = {"index", "index-entries.z", "staged.patch", "unstaged.patch"}
    if info["head"] or info["refs"]:
        required.add("repository.bundle")
    # Check metadata files before reading any paths derived from them.
    for name in required:
        p = directory / name
        require(p.is_file() and not p.is_symlink(), "Missing or invalid required artifact")
    entries = (directory / "index-entries.z").read_bytes()
    if entries:
        require(entries.endswith(b"\0"))
    indexed_names = set()
    for entry in entries.split(b"\0"):
        if not entry:
            continue
        metadata, name = entry.split(b"\t", 1)
        mode, oid, stage = metadata.split()
        require(mode in (b"100644", b"100755", b"120000"))
        require(stage in (b"0", b"1", b"2", b"3"))
        require(bool(re.fullmatch(rb"[a-f0-9]{40}|[a-f0-9]{64}", oid)))
        import os
        indexed_names.add(safe_path(os.fsdecode(name)))
        required.add("objects/" + oid.decode())
    require(isinstance(manifest["files"], list))
    names = set()
    for record in manifest["files"]:
        require(isinstance(record, dict) and isinstance(record.get("path"), str))
        name = safe_path(record["path"])
        require(name not in names)
        names.add(name)
        kind = record.get("type")
        require(kind in ("file", "symlink", "missing"))
        p = directory / "files" / name
        for parent in p.parents:
            if parent == directory:
                break
            require(not parent.is_symlink(), "Symlink archive ancestor")
        if kind == "missing":
            require(set(record) == {"path", "type"})
            require(not p.exists() and not p.is_symlink())
            continue
        require(isinstance(record.get("sha256"), str) and bool(re.fullmatch(r"[a-f0-9]{64}", record["sha256"])))
        required.add("files/" + name)
        if kind == "file":
            require(set(record) == {"path", "type", "sha256", "mode"})
            require(type(record.get("mode")) is int and 0 <= record["mode"] <= 0o7777)
            require(p.is_file() and not p.is_symlink())
        else:
            require(set(record) == {"path", "type", "sha256", "target"})
            require(isinstance(record.get("target"), str) and p.is_symlink())
    require(indexed_names <= names, "Indexed file missing from manifest")
    require(set(artifacts) == required, "Missing or unexpected artifact declarations")
    for name in required:
        p = directory / name
        for parent in p.parents:
            if parent == directory:
                break
            require(not parent.is_symlink(), "Symlink archive ancestor")
        if not name.startswith("files/"):
            require(p.is_file() and not p.is_symlink())
