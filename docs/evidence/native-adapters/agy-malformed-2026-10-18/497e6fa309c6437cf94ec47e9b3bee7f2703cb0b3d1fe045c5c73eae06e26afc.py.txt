#!/usr/bin/env python3
"""Verify AGY's documented interactive /agents panel in isolated containers.

The vendor package is downloaded and checksum-verified as inert host data. The
native process runs only in a disposable, networkless Docker container. The
helper stores sanitized markers, never terminal text, and sends only the
documented `/agents` command after a local prompt is detected without an auth
barrier.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time
import uuid

REPO = Path(__file__).resolve().parents[2]
BASE = REPO / "scripts/native-loading/probe-agy-linux-container.py"
spec = importlib.util.spec_from_file_location("agy_linux_base", BASE)
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)


ANSI = re.compile(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")
AUTH = re.compile(r"sign[ -]?in|log[ -]?in|authenticate|oauth|authentication required|api key", re.I)
LOCAL_PROMPT = re.compile(r"(?:^|\n)[^\n]{0,60}(?:❯|›|>)[ \t]*$|(?:what would you like to do|type a message)[?: ]*$", re.I)
MANAGER_HEADER = re.compile(r"available agents", re.I)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def inspect_local_pinned_image():
    context = base.docker(["context", "inspect", "desktop-linux", "--format",
                           "{{json .Endpoints.docker.Host}}"])
    host = json.loads(context.stdout)
    expected = str(Path.home() / ".docker/run/docker.sock")
    if host != "unix://" + expected:
        raise RuntimeError("Docker context is not the expected local desktop-linux socket")
    info = base.docker(["info", "--format", "{{.Name}} {{.ServerVersion}} {{.OperatingSystem}}"])
    if not info.stdout.decode().startswith("docker-desktop "):
        raise RuntimeError("Docker daemon did not identify as local Docker Desktop")
    digest = base.IMAGE_DIGEST.split("@", 1)[1]
    image_id = "sha256:" + digest.removeprefix("sha256:")
    detail = json.loads(base.docker(["image", "inspect", image_id]).stdout)[0]
    if (detail.get("Id") != image_id or detail.get("Os") != "linux" or detail.get("Architecture") != "amd64" or
            base.IMAGE_DIGEST not in (detail.get("RepoDigests") or []) or
            base.IMAGE not in (detail.get("RepoTags") or [])):
        raise RuntimeError("the exact local pinned amd64 base image is unavailable")
    return {"context": "desktop-linux", "endpoint": "local Docker Desktop Unix socket",
            "daemon": info.stdout.decode().strip(), "image": base.IMAGE_DIGEST,
            "imageId": image_id, "cacheStatus": "already-present; no pull performed"}


def sanitized_state(data):
    text = ANSI.sub(b"", data).decode("utf-8", "replace").replace("\r", "\n")
    return {
        "authBarrierObserved": bool(AUTH.search(text)),
        "localPromptObserved": bool(LOCAL_PROMPT.search(text)),
        "managerSectionObserved": bool(MANAGER_HEADER.search(text)),
        "nameObserved": bool(re.search(r"(?<![\w-])aok-probe(?![\w-])", text)),
        "descriptionObserved": "Synthetic AGY adapter discovery fixture" in text,
        "parseErrorObserved": bool(re.search(r"parse error|invalid yaml|failed to parse|malformed", text, re.I)),
        "errorObserved": bool(re.search(r"\berror\b|failed|invalid", text, re.I)),
        "bytesObserved": len(data),
    }


def may_send_manager_command(state):
    return state["localPromptObserved"] and not state["authBarrierObserved"] and not state["errorObserved"]


def qualifies_manager_listing(case, state):
    return (case["cleanupVerified"] is True and state.get("commandSent") is True and
            state["managerSectionObserved"] and not state["authBarrierObserved"] and
            not state["errorObserved"] and not state["parseErrorObserved"])


def qualifies_fixture(case, state, *, positive):
    if not qualifies_manager_listing(case, state):
        return False
    identity = state["nameObserved"] and state["descriptionObserved"]
    return identity if positive else not identity


def has_candidate_manager_evidence(cases):
    required = ("positive", "absent", "malformed")
    if not all(case in cases and isinstance(cases[case], dict) and "state" in cases[case] for case in required):
        return False
    return (qualifies_fixture(cases["positive"], cases["positive"]["state"], positive=True) and
            all(qualifies_fixture(cases[name], cases[name]["state"], positive=False)
                for name in ("absent", "malformed")))


def write_json_line(path, record):
    with Path(path).open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())


def validate_interactive_config(config):
    host = config["HostConfig"]
    valid = (config["Config"].get("User") == "1000:1000" and
             config["Config"].get("Tty") is True and config["Config"].get("OpenStdin") is True and
             host.get("NetworkMode") == "none" and host.get("ReadonlyRootfs") is True and
             not host.get("Binds") and not config.get("Mounts") and
             set(host.get("Tmpfs", {})) == {"/probe", "/tmp"} and
             host.get("CapDrop") == ["ALL"] and
             "no-new-privileges" in " ".join(host.get("SecurityOpt", [])))
    if not valid:
        raise RuntimeError("interactive container configuration failed isolation checks")


def remove_owned(name, nonce):
    if base.owned_container_labels(name, nonce):
        result = base.docker(["container", "rm", "--force", name], timeout=20, check=False)
        if result.returncode:
            raise RuntimeError("owned interactive container removal is unconfirmed")
    return not base.owned_container_labels(name, nonce)


def _joined(chunks):
    return b"\n".join(chunks.values())


def append_capture(selector, chunks, deadline, stop_when, max_bytes=1024 * 1024):
    """Read TTY bytes until a safe state marker, process exit, or deadline."""
    while time.monotonic() < deadline and sum(map(len, chunks.values())) < max_bytes:
        if not selector.get_map():
            break
        events = selector.select(timeout=min(0.15, max(0.0, deadline - time.monotonic())))
        if not events:
            continue
        for key, _ in events:
            block = os.read(key.fd, 65536)
            if not block:
                try:
                    selector.unregister(key.fileobj)
                except Exception:
                    pass
                continue
            chunks[key.fileobj] += block
            if stop_when(_joined(chunks)):
                return _joined(chunks)
    return _joined(chunks)


def bounded_interactive(container, nonce, audit, case_name, timeout=25):
    attach = None
    selector = selectors.DefaultSelector()
    chunks = {}
    final_state = None
    try:
        attach = subprocess.Popen(["docker", "--context", "desktop-linux", "container", "start", "--attach", "--interactive", container],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  start_new_session=True)
        write_json_line(audit, {"event": "docker-attach-started", "containerId": container,
                                "hostPid": attach.pid, "hostPgid": attach.pid,
                                "case": case_name, "at": datetime.now(timezone.utc).isoformat()})
        for stream in (attach.stdout, attach.stderr):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ)
            chunks[stream] = b""

        init_pid = int(json.loads(base.docker(["container", "inspect", container,
                                               "--format", "{{json .State.Pid}}" ]).stdout))
        write_json_line(audit, {"event": "native-gate-ready", "containerId": container,
                                "containerPid": init_pid, "case": case_name,
                                "at": datetime.now(timezone.utc).isoformat()})
        # The entrypoint cannot exec AGY until the persisted identity/PID record exists.
        started = base.docker(["exec", container, "sh", "-c", "touch /probe/start"], timeout=15)
        if started.returncode:
            raise RuntimeError("could not release the isolated local startup gate")

        deadline = time.monotonic() + timeout
        output = append_capture(selector, chunks, deadline,
                                lambda b: (lambda s: s["authBarrierObserved"] or s["localPromptObserved"] or s["errorObserved"])(sanitized_state(b)))
        state = sanitized_state(output)
        if may_send_manager_command(state):
            attach.stdin.write(b"/agents\n")
            attach.stdin.flush()
            write_json_line(audit, {"event": "documented-command-sent", "containerId": container,
                                    "case": case_name, "command": "/agents", "at": datetime.now(timezone.utc).isoformat()})
            deadline = time.monotonic() + 15
            output = append_capture(selector, chunks, deadline,
                                    lambda b: (lambda s: (s["managerSectionObserved"] and
                                                        (case_name != "positive" or (s["nameObserved"] and s["descriptionObserved"])))
                                                        or s["authBarrierObserved"] or s["errorObserved"])(sanitized_state(b)))
            if sanitized_state(output)["managerSectionObserved"]:
                settle_deadline = min(deadline, time.monotonic() + 1.5)
                output = append_capture(selector, chunks, settle_deadline, lambda _b: False)
            state = sanitized_state(output)
            state["commandSent"] = True
        else:
            state["commandSent"] = False
        final_state = state
        return state
    finally:
        # Closing stdin adds no command; container removal is the authoritative stop.
        if attach is not None and attach.stdin:
            try:
                attach.stdin.close()
            except OSError:
                pass
        try:
            remove_owned(container, nonce)
        finally:
            if attach is not None:
                try:
                    attach.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    try:
                        os.killpg(attach.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                    try:
                        attach.communicate(timeout=3)
                    except subprocess.TimeoutExpired:
                        try:
                            os.killpg(attach.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                        try:
                            attach.communicate(timeout=3)
                        except subprocess.TimeoutExpired:
                            attach.stdout.close()
                            attach.stderr.close()
                            attach.wait(timeout=2)
            if final_state is not None:
                final_state["hostAttachExitCode"] = attach.returncode
            selector.close()


def create_project_inputs(root, rendered):
    git_home = root / "git-home"
    git_home.mkdir()
    git_path = shutil.which("git")
    if not git_path:
        raise RuntimeError("Git is required to create owned synthetic project roots")
    env = {"PATH": "/usr/bin:/bin", "HOME": str(git_home), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"}
    inputs = root / "input"
    for label in ("positive", "absent", "malformed"):
        project = inputs / label
        project.mkdir(parents=True)
        proc = subprocess.run([git_path, "init", "--quiet", "--initial-branch=main"], cwd=project,
                              env=env, capture_output=True, timeout=15)
        if proc.returncode:
            raise RuntimeError("could not create synthetic Git project")
    positive = inputs / "positive/.agents/agents/aok-probe/agent.md"
    positive.parent.mkdir(parents=True)
    positive.write_bytes(rendered)
    malformed = inputs / "malformed/.agents/agents/aok-probe/agent.md"
    malformed.parent.mkdir(parents=True)
    malformed.write_bytes(b"---\nname: aok-probe\nname: duplicate\ndescription: broken\n---\n")
    return inputs


def run_case(case, nonce, image, audit):
    name = "aok-agy-interactive-" + nonce + "-" + case
    wrapper = "mkdir -p /probe/home /probe/project; cp -R /opt/probe/input/" + case + "/. /probe/project/; " \
              "cd /probe/project; while [ ! -e /probe/start ]; do sleep 0.05; done; " \
              "exec /opt/agy/antigravity"
    env = ["--env", "HOME=/probe/home", "--env", "XDG_CONFIG_HOME=/probe/home/.config",
           "--env", "XDG_CACHE_HOME=/probe/home/.cache", "--env", "TMPDIR=/tmp",
           "--env", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
           "--env", "TERM=xterm-256color"]
    try:
        created = base.docker(["container", "create", "--name", name, "--label", "aok.native-loading=" + nonce,
                           "--network", "none", "--read-only", "--user=1000:1000", "--cap-drop=ALL",
                           "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=2g", "--cpus=2",
                           "--tmpfs", "/probe:rw,nosuid,nodev,size=128m,mode=1777",
                           "--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,mode=1777", "--interactive", "--tty", *env,
                           image, "sh", "-c", wrapper], timeout=30, check=False)
    except BaseException:
        remove_owned(name, nonce)
        raise
    if created.returncode:
        remove_owned(name, nonce)
        raise RuntimeError("could not create interactive probe container")
    container_id = created.stdout.decode().strip()
    write_json_line(audit, {"event": "container-created", "containerId": container_id,
                            "containerName": name, "nonce": nonce, "case": case,
                            "at": datetime.now(timezone.utc).isoformat()})
    container_removed = False
    try:
        config = json.loads(base.docker(["container", "inspect", container_id]).stdout)[0]
        validate_interactive_config(config)
        state = bounded_interactive(container_id, nonce, audit, case)
    finally:
        container_removed = remove_owned(container_id, nonce)
        write_json_line(audit, {"event": "container-cleanup", "containerId": container_id,
                                "cleanupVerified": container_removed, "case": case,
                                "at": datetime.now(timezone.utc).isoformat()})
    return {"containerId": container_id, "cleanupVerified": container_removed, "state": state}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", type=Path, required=True)
    parser.add_argument("--evidence-out", type=Path, required=True)
    parser.add_argument("--audit-out", type=Path, required=True)
    args = parser.parse_args()
    if args.evidence_out.exists() or args.audit_out.exists():
        raise SystemExit("evidence and audit paths must be new files")
    evidence = args.evidence_out.resolve()
    audit = args.audit_out.resolve()
    if REPO not in evidence.parents or REPO not in audit.parents:
        raise SystemExit("evidence paths must remain inside this checkout")
    evidence.parent.mkdir(parents=True, exist_ok=True)
    audit.parent.mkdir(parents=True, exist_ok=True)

    code = Path(__file__).read_bytes()
    source_snapshot = REPO / "docs/evidence/native-adapters" / ("agy-interactive-manager-probe-source-" + sha(code) + ".py")
    source_snapshot.parent.mkdir(parents=True, exist_ok=True)
    source_snapshot.write_bytes(code)
    source_hash = sha(code)
    if sha(source_snapshot.read_bytes()) != source_hash:
        raise SystemExit("retained probe source hash mismatch")
    write_json_line(audit, {"event": "probe-source-pinned", "path": str(source_snapshot.relative_to(REPO)),
                            "sha256": source_hash, "at": datetime.now(timezone.utc).isoformat()})

    environment = inspect_local_pinned_image()
    raw_manifest, payload, binary_hash = base.download_payload()
    with tarfile.open(fileobj=__import__("io").BytesIO(payload), mode="r:gz") as archive:
        vendor_binary = archive.extractfile("antigravity").read()
    nonce = uuid.uuid4().hex
    tag = "aok-agy-interactive:" + nonce
    result = {"target": "antigravity", "scope": "project", "nativeLoading": "not-verified",
              "runtimeEnforcement": "unverified", "modelInvocation": "not-run", "authentication": "not-run",
              "observedAt": datetime.now(timezone.utc).isoformat(), "environment": environment,
              "package": {"version": base.VERSION, "manifestUrl": base.MANIFEST_URL,
                          "payloadUrl": base.PAYLOAD_URL, "manifestSha256": sha(raw_manifest),
                          "archiveSha512": hashlib.sha512(payload).hexdigest(), "binarySha256": binary_hash},
              "probeSource": str(source_snapshot.relative_to(REPO)), "probeSourceSha256": source_hash,
              "auditPath": str(audit.relative_to(REPO))}

    with tempfile.TemporaryDirectory(prefix="agy-interactive-manager-") as tmp:
        root = Path(tmp).resolve()
        aok, rendered, rendered_hash, description = base.prepare_aok(args.aok, REPO, root)
        inputs = create_project_inputs(root, rendered)
        context = root / "docker-context"
        context.mkdir()
        (context / "antigravity").write_bytes(vendor_binary)
        (context / "antigravity").chmod(0o555)
        shutil.copytree(inputs, context / "input")
        (context / "Dockerfile").write_text("FROM " + base.IMAGE_DIGEST + "\n"
            "COPY --chown=1000:1000 antigravity /opt/agy/antigravity\n"
            "COPY --chown=1000:1000 input/ /opt/probe/input/\n"
            "USER 1000:1000\n", encoding="utf-8")
        try:
            base.docker(["build", "--no-cache", "--pull=false", "--network=none", "--label",
                         "aok.native-loading=" + nonce, "--tag", tag, str(context)], timeout=240)
            details = json.loads(base.docker(["image", "inspect", tag]).stdout)[0]
            labels = details.get("Config", {}).get("Labels") or {}
            if details.get("Os") != "linux" or details.get("Architecture") != "amd64" or labels.get("aok.native-loading") != nonce:
                raise RuntimeError("derived interactive image failed ownership/platform check")
            cases = {case: run_case(case, nonce, tag, audit) for case in ("positive", "absent", "malformed")}
        finally:
            image_removed = base.remove_owned_image(tag, nonce)
        positive = cases["positive"]["state"]
        candidate = has_candidate_manager_evidence(cases)
        result.update({"aok": {"binarySha256": sha(aok.read_bytes()), "importStatus": "blocked",
                               "exportStatus": "blocked", "bundleCompleteness": "draft-incomplete"},
                       "fixture": {"path": ".agents/agents/aok-probe/agent.md", "sha256": rendered_hash,
                                   "expectedName": "aok-probe", "expectedDescription": description},
                       "cases": cases, "candidateManagerEvidence": candidate,
                       "derivedImage": {"base": base.IMAGE_DIGEST, "removed": image_removed,
                                        "platform": "linux/amd64", "networkDuringBuild": "none"},
                       "limitations": ["The report never promotes nativeLoading automatically.",
                                       "Only the documented /agents command is sent, and only after a local input prompt without auth/error markers.",
                                       "No authentication, agent selection, ordinary prompt, model invocation, imported command, or enforcement is tested."]})
    evidence.parent.mkdir(parents=True, exist_ok=True)
    evidence.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
