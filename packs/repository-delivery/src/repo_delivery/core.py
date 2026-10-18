"""Read-only Git inventory and private, verifiable preservation archives."""
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tempfile


class DeliveryError(Exception):
    """An operation cannot produce trustworthy evidence."""


def git(repo, *args, optional=False):
    env = dict(os.environ, GIT_OPTIONAL_LOCKS="0", LC_ALL="C")
    try:
        proc = subprocess.run(["git", "-C", str(repo), *args], capture_output=True, env=env)
    except FileNotFoundError as exc:
        raise DeliveryError("Git is required for this delivery operation; install Git and make it available on PATH.") from exc
    if proc.returncode and not optional:
        raise DeliveryError(f"Git operation failed: {args[0]}")
    return proc.stdout if proc.returncode == 0 else None


def root(repo):
    data = git(Path(repo).resolve(), "rev-parse", "--show-toplevel")
    return Path(os.fsdecode(data).removesuffix("\n")).resolve()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def encode(data):
    return base64.b64encode(data).decode("ascii")


def paths(data):
    return [os.fsdecode(x) for x in data.split(b"\0") if x]


def safe_path(name):
    p = PurePosixPath(name)
    if not name or p.is_absolute() or any(x in ("..", ".git") for x in p.parts) or str(p) != name:
        raise DeliveryError("Unsafe archive path")
    return name


def source_path(repo, name):
    safe_path(name)
    p = repo / name
    for parent in p.parents:
        if parent == repo:
            break
        if (parent / ".git").exists() or (parent / ".git").is_symlink():
            raise DeliveryError("Embedded repositories are unsupported")
    if any(parent.is_symlink() for parent in p.parents if parent != repo and repo in parent.parents):
        raise DeliveryError("Symlink ancestor in source")
    return p


def inspect(repo):
    repo = root(repo)
    head = git(repo, "rev-parse", "--verify", "HEAD", optional=True)
    branch = git(repo, "symbolic-ref", "--short", "HEAD", optional=True)
    refs = {}
    for line in git(repo, "for-each-ref", "--format=%(refname) %(objectname)").decode().splitlines():
        name, oid = line.split(" ", 1)
        refs[name] = oid
    return {"schema_version": 1, "kind": "inventory", "repository": str(repo),
            "head": head.decode().strip() if head else None,
            "branch": branch.decode().strip() if branch else None, "refs": refs,
            "status_base64": encode(git(repo, "status", "--porcelain=v1", "-z", "--untracked-files=all")),
            "worktrees_base64": encode(git(repo, "worktree", "list", "--porcelain", "-z")),
            "ignored": paths(git(repo, "ls-files", "--others", "--ignored", "--exclude-standard", "-z"))}


def file_record(repo, name):
    p = source_path(repo, name)
    if not p.exists() and not p.is_symlink():
        return {"path": name, "type": "missing"}, None
    mode = p.lstat().st_mode
    if stat.S_ISLNK(mode):
        data = os.fsencode(os.readlink(p))
        return {"path": name, "type": "symlink", "sha256": digest(data), "target": os.fsdecode(data)}, data
    if not stat.S_ISREG(mode):
        raise DeliveryError("Unsupported file type (including submodule or embedded repository)")
    data = p.read_bytes()
    return {"path": name, "type": "file", "sha256": digest(data), "mode": stat.S_IMODE(mode)}, data


def selected(repo, ignored):
    names = set(paths(git(repo, "ls-files", "-z")))
    names.update(paths(git(repo, "ls-files", "--others", "--exclude-standard", "-z")))
    ignored_names = set(paths(git(repo, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")))
    for name in ignored:
        safe_path(name)
        if name not in ignored_names:
            raise DeliveryError("Explicit ignored selection must name an existing ignored file")
        names.add(name)
    return sorted(names)


def source_snapshot(repo, ignored):
    info = inspect(repo)
    index_path = Path(os.fsdecode(git(repo, "rev-parse", "--git-path", "index")).removesuffix("\n"))
    if not index_path.is_absolute():
        index_path = repo / index_path
    index = index_path.read_bytes() if index_path.exists() else b""
    entries = git(repo, "ls-files", "--stage", "-z")
    if git(repo, "rev-parse", "--shared-index-path").strip():
        raise DeliveryError("Split index is unsupported")
    if git(repo, "config", "--bool", "core.sparseCheckout", optional=True) == b"true\n":
        raise DeliveryError("Sparse checkout is unsupported")
    files = [file_record(repo, name) for name in selected(repo, ignored)]
    return info, index, entries, files


def create_checkpoint(repo, output, include_ignored=()):
    repo = root(repo)
    output = Path(output).absolute()
    parent = output.parent.resolve()
    output = parent / output.name
    common = Path(os.fsdecode(git(repo, "rev-parse", "--git-common-dir")).removesuffix("\n"))
    common = (repo / common).resolve()
    if output == repo or repo in output.parents or output == common or common in output.parents:
        raise DeliveryError("Checkpoint must be outside source and Git storage")
    if output.exists() or output.is_symlink():
        raise DeliveryError("Checkpoint destination exists")
    before = source_snapshot(repo, include_ignored)
    info, index, entries, records = before
    stage = Path(tempfile.mkdtemp(prefix=".repo-delivery-", dir=parent))
    os.chmod(stage, 0o700)
    try:
        def write(name, data):
            p = stage / name
            p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            p.write_bytes(data)
            p.chmod(0o600)
        write("index", index)
        write("index-entries.z", entries)
        write("staged.patch", git(repo, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv"))
        write("unstaged.patch", git(repo, "diff", "--binary", "--no-ext-diff", "--no-textconv"))
        for entry in entries.split(b"\0"):
            if not entry:
                continue
            metadata, _ = entry.split(b"\t", 1)
            mode, oid, _ = metadata.split()
            if mode == b"160000":
                raise DeliveryError("Submodules are unsupported")
            write("objects/" + oid.decode(), git(repo, "cat-file", "blob", oid.decode()))
        if info["head"] or info["refs"]:
            args = ["bundle", "create", str(stage / "repository.bundle"), "--all"]
            if info["head"]:
                args.append("HEAD")
            git(repo, *args)
            (stage / "repository.bundle").chmod(0o600)
        for record, data in records:
            name = "files/" + record["path"]
            if record["type"] == "file":
                write(name, data)
            elif record["type"] == "symlink":
                p = stage / name
                p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                p.symlink_to(record["target"])
        if before != source_snapshot(repo, include_ignored):
            raise DeliveryError("Source changed during checkpoint; retry with quiescent source")
        manifest = {"schema_version": 1, "kind": "checkpoint", "inventory": info,
                    "files": [r for r, _ in records], "included_ignored": list(include_ignored),
                    "excluded_ignored": sorted(set(info["ignored"]) - set(include_ignored)),
                    "artifacts": archive_hashes(stage)}
        write("manifest.json", canonical(manifest))
        receipt = {"schema_version": 1, "kind": "preservation-receipt", "manifest_sha256": digest(canonical(manifest))}
        write("receipt.json", canonical(receipt))
        # mkdir reserves destination without replacing an existing archive.
        output.mkdir(mode=0o700)
        try:
            for child in stage.iterdir():
                shutil.move(str(child), output / child.name)
        except BaseException:
            shutil.rmtree(output)
            raise
        return receipt
    finally:
        shutil.rmtree(stage)


def canonical(value):
    return (json.dumps(value, ensure_ascii=True, sort_keys=True, indent=2) + "\n").encode()


def archive_hashes(directory):
    result = {}
    for base, dirs, names in os.walk(directory, followlinks=False):
        for name in dirs[:]:
            p = Path(base) / name
            if p.is_symlink():
                dirs.remove(name)
                names.append(name)
        for name in names:
            p = Path(base) / name
            rel = p.relative_to(directory).as_posix()
            safe_path(rel)
            result[rel] = digest(os.fsencode(os.readlink(p)) if p.is_symlink() else p.read_bytes())
    return result


def verify_checkpoint(directory):
    directory = Path(directory).resolve()
    try:
        for name in ("manifest.json", "receipt.json"):
            if (directory / name).is_symlink():
                raise DeliveryError("Metadata cannot be a symlink")
        manifest = json.loads((directory / "manifest.json").read_bytes())
        receipt = json.loads((directory / "receipt.json").read_bytes())
        if manifest["kind"] != "checkpoint" or manifest["schema_version"] != 1:
            raise DeliveryError("Unsupported checkpoint schema")
        if receipt != {"schema_version": 1, "kind": "preservation-receipt", "manifest_sha256": digest(canonical(manifest))}:
            raise DeliveryError("Manifest checksum mismatch")
        validate_manifest(directory, manifest)
        actual = archive_hashes(directory)
        del actual["manifest.json"]
        del actual["receipt.json"]
        if actual != manifest["artifacts"]:
            raise DeliveryError("Archive checksum or file set mismatch")
        for record in manifest["files"]:
            p = source_path(directory / "files", record["path"])
            if record["type"] == "symlink" and (not p.is_symlink() or os.readlink(p) != record["target"] or digest(os.fsencode(os.readlink(p))) != record["sha256"]):
                raise DeliveryError("Symlink type mismatch")
            if record["type"] == "file" and (p.is_symlink() or digest(p.read_bytes()) != record["sha256"]):
                raise DeliveryError("File type or checksum mismatch")
        return receipt
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise DeliveryError("Invalid checkpoint archive") from exc


def compare(repo, baseline):
    repo = root(repo)
    revision = git(repo, "rev-parse", "--verify", "--end-of-options", baseline + "^{commit}").decode().strip()
    baseline_files = {}
    for item in git(repo, "ls-tree", "-rz", revision).split(b"\0"):
        if item:
            metadata, name = item.split(b"\t", 1)
            mode, kind, oid = metadata.split()
            if kind != b"blob":
                raise DeliveryError("Submodule comparison is unsupported")
            baseline_files[os.fsdecode(name)] = (mode, oid)
    result = []
    snapshot = []
    for name in sorted(set(baseline_files) | set(selected(repo, ()))):
        record, data = file_record(repo, name)
        snapshot.append(record)
        base = baseline_files.get(name)
        if record["type"] == "missing":
            state = "deleted" if base else "local-missing"
        elif base is None:
            state = "local-only"
        else:
            mode, oid = base
            expected = git(repo, "cat-file", "blob", oid.decode())
            same_type = (mode == b"120000") == (record["type"] == "symlink")
            same_exec = record["type"] == "symlink" or bool(record["mode"] & 0o111) == (mode == b"100755")
            state = "identical" if data == expected and same_type and same_exec else "modified"
        result.append({"path": name, "state": state})
    return {"schema_version": 1, "kind": "comparison", "repository": str(repo), "baseline_revision": revision, "snapshot_sha256": digest(canonical(snapshot)), "files": result}


def validate_manifest(directory, manifest):
    from .validation import validate
    validate(directory, manifest, DeliveryError, safe_path, digest)
