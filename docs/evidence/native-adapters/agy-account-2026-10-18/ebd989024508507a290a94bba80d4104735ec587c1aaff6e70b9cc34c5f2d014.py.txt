#!/usr/bin/env python3
"""Probe the official AGY Linux CLI only inside a disposable, networkless Docker container.

The official archive is fetched and SHA-512 verified on the host as inert data.
The native executable runs only in a private container with no host mounts,
credentials, or network. Raw terminal output is never persisted.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request
import uuid

IMAGE = "node:26.7.0-bookworm-slim"
IMAGE_DIGEST = "node@sha256:4db36457f406501e6f608802e5da617e5fbd0e80b75901b6a09de1ae5a667d32"
VERSION = "1.4.2"
MANIFEST_URL = "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_amd64.json"
PAYLOAD_URL = "https://storage.googleapis.com/antigravity-public/antigravity-cli/1.4.2-4835981233356800/linux-x64/cli_linux_x64.tar.gz"
PAYLOAD_SHA512 = "4a393afc1369cee94e817c1b430c9128d02b12ebd4d5703ace5cd47b2333e216d673fb0097cbf7195f757b110163ba9850e9eea3fdbbaddd4b25668b82c03f05"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def docker(argv, *, data=None, timeout=20, check=True):
    proc = subprocess.run(["docker", "--context", "desktop-linux", *argv], input=data,
                          capture_output=True, timeout=timeout)
    if check and proc.returncode:
        raise RuntimeError("Docker command failed: " + proc.stderr.decode("utf-8", "replace")[:500])
    return proc


def make_tar(files):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w") as tar:
        for name, (data, mode) in sorted(files.items()):
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), mode
            tar.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def classify(text, expected_name, expected_description):
    low = text.lower()
    return {
        "expectedNameObserved": bool(re.search(r"(?<![\w-])" + re.escape(expected_name) + r"(?![\w-])", text)),
        "expectedDescriptionObserved": expected_description in text,
        "authPromptMarkerObserved": any(x in low for x in ("sign in", "log in", "login", "authenticate", "oauth")),
        "parseErrorMarkerObserved": any(x in low for x in ("parse error", "invalid yaml", "failed to parse", "malformed")),
        "errorMarkerObserved": any(x in low for x in ("error", "failed", "invalid")),
    }


def inspect_context_and_image():
    context = docker(["context", "inspect", "desktop-linux", "--format", "{{json .Endpoints.docker.Host}}"])
    host = json.loads(context.stdout)
    expected_socket = str(Path.home() / ".docker/run/docker.sock")
    if host != "unix://" + expected_socket:
        raise RuntimeError("Docker context is not the expected local desktop-linux socket")
    info = docker(["info", "--format", "{{.Name}} {{.ServerVersion}} {{.OperatingSystem}}"])
    if not info.stdout.decode().startswith("docker-desktop "):
        raise RuntimeError("Docker daemon did not identify as local Docker Desktop")
    image_inspect = docker(["image", "inspect", IMAGE, "--format", "{{json .RepoDigests}}"])
    digests = json.loads(image_inspect.stdout)
    if IMAGE_DIGEST not in digests:
        raise RuntimeError("Required pinned image digest is not already present locally")
    detail = json.loads(docker(["image", "inspect", IMAGE_DIGEST]).stdout)[0]
    if detail.get("Os") != "linux" or detail.get("Architecture") != "amd64":
        raise RuntimeError("Pinned runtime image is not linux/amd64")
    return {"context": "desktop-linux", "endpoint": "local Docker Desktop Unix socket",
            "daemon": info.stdout.decode().strip(), "image": IMAGE_DIGEST}


def download_payload():
    with urllib.request.urlopen(MANIFEST_URL, timeout=20) as response:
        raw_manifest = response.read(65536)
    manifest = json.loads(raw_manifest)
    if manifest != {"version": VERSION, "url": PAYLOAD_URL, "sha512": PAYLOAD_SHA512}:
        raise RuntimeError("Official manifest did not match the pinned Linux x64 release")
    with urllib.request.urlopen(PAYLOAD_URL, timeout=60) as response:
        payload = response.read(100 * 1024 * 1024)
    if hashlib.sha512(payload).hexdigest() != PAYLOAD_SHA512:
        raise RuntimeError("Official AGY Linux payload checksum mismatch")
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as archive:
        members = archive.getmembers()
        if len(members) != 1 or members[0].name != "antigravity" or not members[0].isfile():
            raise RuntimeError("Unexpected vendor archive contents")
        binary_sha = hashlib.sha256(archive.extractfile(members[0]).read()).hexdigest()
    return raw_manifest, payload, binary_sha


def prepare_aok(aok_path, repo, root):
    aok_path = aok_path.resolve(strict=True)
    aok_copy = root / "aok"
    aok_copy.write_bytes(aok_path.read_bytes())
    aok_copy.chmod(0o700)
    if sha(aok_copy.read_bytes()) != sha(aok_path.read_bytes()):
        raise RuntimeError("Copied AOK binary hash mismatch")
    source = repo / "internal/nativeadapters/agy/testdata/probe.md"
    input_root = root / "native-input"
    native = input_root / ".agents/agents/aok-probe/agent.md"
    native.parent.mkdir(parents=True)
    native.write_bytes(source.read_bytes())
    (input_root / "aok-native-input.json").write_text(json.dumps({
        "version": "aok-native-input/v1", "files": [
            {"path": ".agents/agents/aok-probe/agent.md", "format": "markdown"}]}), encoding="utf-8")
    imported = root / "imported"
    clean_env = {"PATH": "/usr/bin:/bin", "HOME": str(root / "aok-home"),
                 "XDG_CONFIG_HOME": str(root / "aok-home/config"), "XDG_CACHE_HOME": str(root / "aok-home/cache"),
                 "TMPDIR": str(root)}
    Path(clean_env["HOME"]).mkdir()
    r = subprocess.run([str(aok_copy), "agent", "native", "import", "--target", "agy", "--scope", "project",
                        "--input", str(input_root), "--out", str(imported), "--json"],
                       capture_output=True, timeout=30, cwd=root, env=clean_env)
    report = json.loads(r.stdout)
    bundle = imported / "aok-native.json"
    bundle_json = json.loads(bundle.read_text())
    if r.returncode != 1 or report.get("status") != "blocked" or bundle_json.get("completeness") != "draft-incomplete":
        raise RuntimeError("AOK import did not produce the expected blocked draft")
    exported = root / "exported"
    r = subprocess.run([str(aok_copy), "agent", "native", "export", "--target", "agy", "--scope", "project",
                        "--input", str(bundle), "--source", str(input_root), "--out", str(exported), "--json"],
                       capture_output=True, timeout=30, cwd=root, env=clean_env)
    report = json.loads(r.stdout)
    rendered = exported / ".agents/agents/aok-probe/agent.md"
    if r.returncode != 1 or report.get("status") != "blocked" or not rendered.is_file():
        raise RuntimeError("AOK export did not produce its blocked candidate")
    content = rendered.read_bytes()
    match = re.search(rb"(?m)^description:\s*['\"]?([^\r\n'\"]+)", content)
    if not match:
        raise RuntimeError("Could not read fixture description from rendered Markdown")
    description = match.group(1).decode("utf-8")
    return aok_copy, content, sha(content), description


def owned_container_labels(container, nonce):
    r = docker(["container", "inspect", container, "--format", "{{json .Config.Labels}}"], check=False)
    if r.returncode != 0:
        message = r.stderr.decode("utf-8", "replace").lower()
        if "no such container" in message or "no such object" in message:
            return False
        raise RuntimeError("Could not verify container ownership/cleanup state")
    labels = json.loads(r.stdout)
    if labels.get("aok.native-loading") != nonce:
        raise RuntimeError("Container name resolved to a resource without this probe's ownership label")
    return True


def remove_owned_container(container, nonce):
    if owned_container_labels(container, nonce):
        removed = docker(["container", "rm", "--force", container], timeout=20, check=False)
        if removed.returncode != 0:
            raise RuntimeError("Owned container removal failed; cleanup is unconfirmed")
    return not owned_container_labels(container, nonce)


def validate_container_config(config):
    host = config["HostConfig"]
    isolated = (config["Config"].get("User") == "1000:1000" and config["Config"].get("Tty") and
                not config["Config"].get("OpenStdin") and host.get("NetworkMode") == "none" and
                host.get("ReadonlyRootfs") and not host.get("Binds") and not config.get("Mounts") and
                set(host.get("Tmpfs", {})) == {"/probe", "/tmp"} and host.get("CapDrop") == ["ALL"] and
                "no-new-privileges" in " ".join(host.get("SecurityOpt", [])))
    if not isolated:
        raise RuntimeError("Container configuration did not satisfy isolation assertions")


def _clean_surface(observation, *, tty=False):
    if not isinstance(observation, dict):
        return False
    if observation.get("returnCode" if tty else "exitCode") != 0:
        return False
    return not any(observation.get(key, False) for key in (
        "authPromptMarkerObserved", "parseErrorMarkerObserved", "errorMarkerObserved"))


def has_candidate_roster_evidence(results):
    """Return a conservative review candidate; never closes the loading gate."""
    def complete_case(case):
        value = results.get(case)
        if not isinstance(value, dict) or value.get("containerReturnCode") != 0 or value.get("cleanupVerified") is not True:
            return False
        summary = value.get("summary")
        if not isinstance(summary, dict) or summary.get("version", {}).get("exitCode") != 0:
            return False
        non_tty = summary.get("nonTTY")
        return (_clean_surface(non_tty) and value.get("tty", {}).get("returnCode") == 0 and
                _clean_surface(value.get("tty"), tty=True))

    def observes_identity(case):
        value = results[case]
        tty = value["tty"]
        non_tty = value["summary"]["nonTTY"]
        return any(surface.get("expectedNameObserved") and surface.get("expectedDescriptionObserved")
                   for surface in (tty, non_tty))

    if not all(complete_case(case) for case in ("positive", "absent", "malformed")):
        return False
    return (observes_identity("positive") and
            not observes_identity("absent") and not observes_identity("malformed"))


def native_loading_status(results):
    # Output parsing is not sufficient to close the host-loading gate.
    has_candidate_roster_evidence(results)
    return "not-verified"


def run_container_case(name, nonce, image, expected_name, description, timeout=45):
    container = "aok-agy-linux-" + nonce + "-" + name
    marker_begin, marker_end = "__AGY_TTY_BEGIN_" + nonce + "__", "__AGY_TTY_END_" + nonce + "__"
    script = r'''set +e
mkdir -p /probe/home /probe/project
cp -R /opt/probe/input/$CASE/. /probe/project/
export HOME=/probe/home XDG_CONFIG_HOME=/probe/home/.config XDG_CACHE_HOME=/probe/home/.cache TMPDIR=/tmp
cd /probe/project
/opt/agy/antigravity --version </dev/null >/probe/version.out 2>/probe/version.err; version_rc=$?
/opt/agy/antigravity --help </dev/null >/probe/help.out 2>/probe/help.err; help_rc=$?
/opt/agy/antigravity agents --help </dev/null >/probe/agents-help.out 2>/probe/agents-help.err; agents_help_rc=$?
/opt/agy/antigravity agents </dev/null >/probe/non-tty.out 2>/probe/non-tty.err; non_tty_rc=$?
printf '\n%s\n' "$TTY_BEGIN"
timeout --signal=TERM --kill-after=2s 12s /opt/agy/antigravity agents
tty_rc=$?
printf '\n%s:%s\n' "$TTY_END" "$tty_rc"
export VERSION_RC=$version_rc HELP_RC=$help_rc AGENTS_HELP_RC=$agents_help_rc NON_TTY_RC=$non_tty_rc
node -e 'const fs=require("fs");const get=n=>fs.readFileSync("/probe/"+n);const all=n=>Buffer.concat([get(n+".out"),get(n+".err")]).toString("utf8");const a=all("non-tty"),v=all("version"),h=all("help"),ah=all("agents-help"),name=process.env.EXPECTED_NAME,desc=process.env.EXPECTED_DESC;const esc=s=>s.replace(/[.*+?^${}()|[\]\\]/g,"\\$&");const rx=new RegExp("(^|[^\\w-])"+esc(name)+"([^\\w-]|$)");const auth=x=>/sign in|log in|login|authenticate|oauth/i.test(x),bad=x=>/parse error|invalid yaml|failed to parse|malformed/i.test(x),err=x=>/error|failed|invalid/i.test(x);const summary={version:{exitCode:+process.env.VERSION_RC,version14:v.includes("1.4.2")},help:{exitCode:+process.env.HELP_RC,agentsHelpExitCode:+process.env.AGENTS_HELP_RC,hasAgentCommand:/agent/i.test(h),hasListAvailable:/list available agents/i.test(ah)},nonTTY:{exitCode:+process.env.NON_TTY_RC,stdoutBytes:get("non-tty.out").length,stderrBytes:get("non-tty.err").length,expectedNameObserved:rx.test(a),expectedDescriptionObserved:a.includes(desc),authPromptMarkerObserved:auth(a),parseErrorMarkerObserved:bad(a),errorMarkerObserved:err(a)}};console.log("__SUMMARY__"+JSON.stringify(summary));'
'''
    env = ["--env", "CASE=" + name, "--env", "EXPECTED_NAME=" + expected_name,
           "--env", "EXPECTED_DESC=" + description, "--env", "TTY_BEGIN=" + marker_begin,
           "--env", "TTY_END=" + marker_end, "--env", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
           "--env", "HOME=/probe/home", "--env", "XDG_CONFIG_HOME=/probe/home/.config",
           "--env", "XDG_CACHE_HOME=/probe/home/.cache", "--env", "TMPDIR=/tmp"]
    try:
        created = docker(["container", "create", "--name", container, "--label", "aok.native-loading=" + nonce,
                      "--network", "none", "--read-only", "--user=1000:1000", "--cap-drop=ALL",
                      "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=2g", "--cpus=2",
        "--tmpfs", "/probe:rw,nosuid,nodev,size=128m,mode=1777",
                      "--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,mode=1777", "-t", *env,
                      image, "sh", "-c", script], timeout=30)
    except BaseException:
        # Docker may have created the named object before a client timeout/error.
        remove_owned_container(container, nonce)
        raise
    if created.returncode:
        remove_owned_container(container, nonce)
        raise RuntimeError("Could not create isolated AGY container")
    container_id = created.stdout.decode().strip()
    proc = None
    timed_out = False
    cleanup_verified = False
    try:
        config = json.loads(docker(["container", "inspect", container_id]).stdout)[0]
        validate_container_config(config)
        proc = subprocess.Popen(["docker", "--context", "desktop-linux", "container", "start", "--attach", container_id],
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                start_new_session=True)
        try:
            out, err = proc.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            remove_owned_container(container_id, nonce)
            try:
                out, err = proc.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, 9)
                try:
                    out, err = proc.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.stdout.close()
                    proc.stderr.close()
                    proc.wait(timeout=2)
                    out, err = b"", b""
    finally:
        cleanup_verified = remove_owned_container(container_id, nonce)
    if timed_out:
        return {"timedOut": True, "containerReturnCode": proc.returncode,
                "stdoutBytes": len(out), "stderrBytes": len(err), "cleanupVerified": cleanup_verified}
    text = out.decode("utf-8", "replace")
    summary_match = re.search(r"__SUMMARY__(\{[^\r\n]*\})", text)
    tty_text, tty_rc = "", None
    if marker_begin in text:
        segment = text.split(marker_begin, 1)[1]
        tty_text = segment.split(marker_end, 1)[0]
        ending = re.search(re.escape(marker_end) + r":(\d+)", segment)
        tty_rc = int(ending.group(1)) if ending else None
    result = {"containerReturnCode": proc.returncode, "cleanupVerified": cleanup_verified,
              "tty": {**classify(tty_text, expected_name, description), "returnCode": tty_rc,
                      "stdoutBytes": len(tty_text.encode("utf-8"))}}
    if summary_match:
        result["summary"] = json.loads(summary_match.group(1))
    else:
        result["summaryObserved"] = False
        result["dockerOutputBytes"] = len(out)
        result["dockerErrorBytes"] = len(err)
    return result


def image_has_nonce(tag, nonce):
    inspected = docker(["image", "inspect", tag], check=False)
    if inspected.returncode != 0:
        message = inspected.stderr.decode("utf-8", "replace").lower()
        if "no such image" in message or "no such object" in message:
            return False
        raise RuntimeError("Could not verify derived image ownership/cleanup state")
    details = json.loads(inspected.stdout)[0]
    labels = details.get("Config", {}).get("Labels") or {}
    if labels.get("aok.native-loading") != nonce:
        raise RuntimeError("Image tag resolved to a resource without this probe's ownership label")
    return True


def remove_owned_image(tag, nonce):
    if image_has_nonce(tag, nonce):
        removed = docker(["image", "rm", tag], timeout=45, check=False)
        if removed.returncode != 0:
            raise RuntimeError("Owned derived image removal failed; cleanup is unconfirmed")
    return not image_has_nonce(tag, nonce)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", type=Path, required=True)
    parser.add_argument("--retain-render", type=Path, required=True)
    parser.add_argument("--evidence-out", type=Path, required=True)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    evidence_path = args.evidence_out.resolve()
    if evidence_path.exists() or repo not in evidence_path.parents:
        raise SystemExit("evidence output must be a new file inside this repository")
    docker_environment = inspect_context_and_image()
    raw_manifest, payload, binary_hash = download_payload()
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as payload_archive:
        vendor_binary = payload_archive.extractfile("antigravity").read()
    nonce = uuid.uuid4().hex
    image_tag = "aok-agy-probe:" + nonce
    result = None
    with tempfile.TemporaryDirectory(prefix="aok-agy-linux-container-") as tmp:
        root = Path(tmp).resolve()
        aok_copy, rendered, rendered_hash, description = prepare_aok(args.aok, repo, root)
        retained = args.retain_render.resolve()
        if repo not in retained.parents:
            raise SystemExit("retained render must be inside this repository")
        if retained.exists() and retained.read_bytes() != rendered:
            raise SystemExit("refusing to overwrite a different retained render")
        if not retained.exists():
            retained.parent.mkdir(parents=True, exist_ok=True)
            retained.write_bytes(rendered)

        context = root / "docker-context"
        context.mkdir()
        binary = context / "antigravity"
        binary.write_bytes(vendor_binary)
        binary.chmod(0o555)
        (context / "Dockerfile").write_text(
            "FROM " + IMAGE_DIGEST + "\n"
            "COPY --chown=1000:1000 antigravity /opt/agy/antigravity\n"
            "COPY --chown=1000:1000 input/ /opt/probe/input/\n"
            "USER 1000:1000\n", encoding="utf-8")
        git_home = root / "git-home"
        git_home.mkdir()
        git_path = shutil.which("git")
        if not git_path:
            raise RuntimeError("Git executable is required only to create synthetic test repositories")
        git_env = {"PATH": "/usr/bin:/bin", "HOME": str(git_home),
                   "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"}
        input_root = context / "input"
        for case in ("positive", "absent", "malformed"):
            project = input_root / case
            project.mkdir(parents=True)
            init = subprocess.run([git_path, "init", "--quiet", "--initial-branch=main"],
                                  cwd=project, env=git_env, capture_output=True, timeout=15)
            if init.returncode:
                raise RuntimeError("Could not initialize synthetic Git-root fixture")
        positive = input_root / "positive/.agents/agents/aok-probe/agent.md"
        positive.parent.mkdir(parents=True)
        positive.write_bytes(rendered)
        malformed = input_root / "malformed/.agents/agents/aok-probe/agent.md"
        malformed.parent.mkdir(parents=True)
        malformed.write_bytes(b"not valid frontmatter\n")

        results = {}
        image_removed = False
        try:
            docker(["build", "--no-cache", "--pull=false", "--network=none",
                    "--label", "aok.native-loading=" + nonce, "--tag", image_tag,
                    str(context)], timeout=240)
            image_details = json.loads(docker(["image", "inspect", image_tag]).stdout)[0]
            if (image_details.get("Architecture") != "amd64" or image_details.get("Os") != "linux" or
                    (image_details.get("Config", {}).get("Labels") or {}).get("aok.native-loading") != nonce):
                raise RuntimeError("Derived probe image failed platform or ownership checks")
            for case in ("positive", "absent", "malformed"):
                results[case] = run_container_case(case, nonce, image_tag, "aok-probe", description)
        finally:
            image_removed = remove_owned_image(image_tag, nonce)
        candidate_evidence = has_candidate_roster_evidence(results)
        result = {
            "target": "antigravity", "scope": "project", "observedAt": datetime.now(timezone.utc).isoformat(),
            "nativeLoading": native_loading_status(results),
            "cliRoster": "not-verified",
            "candidateRosterEvidence": "requires-independent-review" if candidate_evidence else "not-observed",
            "runtimeEnforcement": "unverified", "modelInvocation": "not-run", "authentication": "not-run",
            "environment": docker_environment,
            "package": {"manifestUrl": MANIFEST_URL, "payloadUrl": PAYLOAD_URL,
                        "manifestSha256": sha(raw_manifest), "version": VERSION,
                        "archiveSha512": hashlib.sha512(payload).hexdigest(),
                        "binarySha256": binary_hash, "archiveBytes": len(payload), "binaryBytes": len(vendor_binary)},
            "derivedImage": {"tag": "aok-agy-probe:<UUID>", "base": IMAGE_DIGEST,
                             "platform": "linux/amd64", "copyOnlyDockerfile": True,
                             "networkDuringBuild": "none", "removedAfterProbe": image_removed},
            "aok": {"binarySha256": sha(aok_copy.read_bytes()), "importStatus": "blocked",
                    "exportStatus": "blocked", "bundleCompleteness": "draft-incomplete"},
            "renderedFixture": {"path": str(retained.relative_to(repo)), "nativePath": ".agents/agents/aok-probe/agent.md",
                                "sha256": rendered_hash, "expectedName": "aok-probe",
                                "expectedDescription": description},
            "container": {"network": "none; private loopback namespace", "rootFilesystem": "read-only",
                          "user": "1000:1000", "capabilities": "all dropped", "hostMounts": "none",
                          "credentials": "none supplied", "stdin": "closed; TTY allocated for TTY case",
                          "tmpfs": ["/probe", "/tmp"], "timeouts": "12 seconds per native TTY listing, 45 seconds per container"},
            "controls": results,
            "limitations": ["The probe never promotes native loading; any candidate roster observation requires independent review.",
                            "No credentials, authentication, model inference, ordinary prompt input, agent selection, or enforcement were tested.",
                            "Linux-container evidence is distinct from prior Darwin observations."],
            "probe": "scripts/native-loading/probe-agy-linux-container.py",
            "probeSha256": sha(Path(__file__).read_bytes()),
            "source": "Official manifest-pinned Linux x64 archive; downloaded as data, checksum-verified, never executed on the host.",
        }
    evidence_path.parent.mkdir(parents=True, exist_ok=True)
    evidence_path.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
