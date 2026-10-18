#!/usr/bin/env python3
"""Verify OpenClaw in disposable containers, including its hardcoded /tmp locks.

Requires an already-running Docker engine and the explicit pinned Node image.
Only synthetic AOK-native fixture bytes enter the containers, via a tar stream.
No host paths, credentials, Docker socket, or user configuration are mounted.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path
import subprocess
import shlex
import tarfile
import tempfile
import uuid

IMAGE = "node:26.7.0-bookworm-slim"
PACKAGE = "2026.9.4"
INTEGRITY = "sha512-lTQpEEe1Xm3u2PCHaPEr+vP8paGk1vLdHuzdItsNToaLI6hAqRVvgJYg+GxukJhETJp4tPy/S1Gftl4KuB8n7A=="


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", type=Path, required=True)
    args = parser.parse_args()
    aok = args.aok.resolve(strict=True)
    repo = Path(__file__).resolve().parents[2]
    nonce = uuid.uuid4().hex
    volume = "aok-openclaw-probe-" + nonce
    owned_containers = []
    volume_created = False

    def docker(argv, *, data=None, timeout=30, check=True):
        r = subprocess.run(["docker", *argv], input=data, capture_output=True, timeout=timeout)
        if check and r.returncode != 0:
            raise RuntimeError("Docker probe command failed: " + r.stderr.decode(errors="replace")[-1200:])
        return r

    inspect = docker(["image", "inspect", IMAGE, "--format", "{{json .RepoDigests}}"])
    digests = json.loads(inspect.stdout)
    if not digests:
        raise RuntimeError("Pinned image must have a registry digest; pull it explicitly before probing")
    image = "node@sha256:4db36457f406501e6f608802e5da617e5fbd0e80b75901b6a09de1ae5a667d32"
    if image not in digests:
        raise RuntimeError("Explicit pinned Node image digest is required")

    def is_owned(kind, name):
        result = docker([kind, "inspect", name, "--format", '{{json .Labels}}' if kind == "volume" else '{{json .Config.Labels}}'], check=False)
        return result.returncode == 0 and (json.loads(result.stdout) or {}).get("aok.native-loading") == nonce
    with tempfile.TemporaryDirectory(prefix="aok-openclaw-container-") as tmp:
        root = Path(tmp).resolve()
        source = repo / "internal/nativeadapters/openclaw/testdata/home"
        for op in [
            ["import", "--input", str(source), "--out", str(root / "draft")],
            ["export", "--input", str(root / "draft/aok-native.json"), "--source", str(source), "--out", str(root / "export")],
            ["compare", "--input", str(root / "export"), "--against", str(root / "export/aok-native.json")],
        ]:
            r = subprocess.run([str(aok), "agent", "native", op[0], "--target", "openclaw",
                                "--scope", "isolated-home", *op[1:]], capture_output=True, timeout=30)
            if r.returncode != (0 if op[0] == "compare" else 1):
                raise RuntimeError("AOK native fixture preparation failed")
        files = {p: (root / "export" / p).read_bytes() for p in
                 [".openclaw/openclaw.json", ".openclaw/workspace/AGENTS.md"]}
        expected_config = json.loads(files[".openclaw/openclaw.json"])
        expected_id, entry = next(iter(expected_config["agents"]["entries"].items()))

        def archive(case):
            chosen = dict(files)
            if case == "absent":
                chosen[".openclaw/openclaw.json"] = b'{"agents":{"entries":{}},"update":{"checkOnStart":false}}'
            elif case == "malformed":
                chosen[".openclaw/openclaw.json"] = b'{"agents":'
            buf = io.BytesIO()
            with tarfile.open(fileobj=buf, mode="w") as tar:
                for path, data in chosen.items():
                    info = tarfile.TarInfo(path)
                    info.size, info.mode = len(data), 0o600
                    tar.addfile(info, io.BytesIO(data))
            return buf.getvalue()

        try:
            if docker(["volume", "inspect", volume], check=False).returncode == 0:
                raise RuntimeError("Refusing to reuse an existing probe volume")
            docker(["volume", "create", "--label", "aok.native-loading=" + nonce, volume])
            if not is_owned("volume", volume):
                raise RuntimeError("Probe volume ownership label mismatch")
            volume_created = True
            install_name = "aok-openclaw-install-" + nonce
            owned_containers.append(install_name)
            installed = docker([
                "run", "--rm", "--name", install_name, "--label", "aok.native-loading=" + nonce, "--read-only", "--cap-drop=ALL",
                "--security-opt=no-new-privileges", "--pids-limit=256", "--memory=2g", "--tmpfs", "/tmp:rw,nosuid,size=512m",
                "--mount", "type=volume,source=" + volume + ",target=/opt/probe",
                "--env", "HOME=/tmp/npm-home", "--env", "npm_config_cache=/tmp/npm-cache",
                "--env", "npm_config_registry=https://registry.npmjs.org/", image, "sh", "-c",
                "npm install --prefix /opt/probe --no-audit --no-fund openclaw@" + PACKAGE +
                " >/dev/null && node -e 'const fs=require(\"fs\");const p=JSON.parse(fs.readFileSync(\"/opt/probe/node_modules/openclaw/package.json\"));const l=JSON.parse(fs.readFileSync(\"/opt/probe/package-lock.json\"));console.log(JSON.stringify({version:p.version,integrity:l.packages[\"node_modules/openclaw\"].integrity,node:process.version}))'",
            ], timeout=180)
            package = json.loads(installed.stdout)
            if package["version"] != PACKAGE or package["integrity"] != INTEGRITY or package["node"] != "v26.7.0":
                raise RuntimeError("Unexpected native package version")
            observations = {}
            prelude = "mkdir -p /probe/home && tar xf - -C /probe/home && "
            workspace_code = """
import fs from 'node:fs';import crypto from 'node:crypto';import {pathToFileURL} from 'node:url';
const dir='/opt/probe/node_modules/openclaw/dist/';
const file=fs.readdirSync(dir).find(n=>n.startsWith('workspace-')&&n.endsWith('.mjs')&&fs.readFileSync(dir+n,'utf8').includes('loadWorkspaceBootstrapFiles as _'));
if(!file)throw Error('Native workspace module changed');
const m=await import(pathToFileURL(dir+file).href);
const found=(await m._('/probe/home/.openclaw/workspace',['AGENTS.md'])).find(f=>f.name==='AGENTS.md');
const missing=(await m._('/probe/missing',['AGENTS.md'])).find(f=>f.name==='AGENTS.md');
console.log(JSON.stringify({loaded:!!found&&!found.missing,contentSHA256:crypto.createHash('sha256').update(found?.content||'').digest('hex'),missingReported:missing?.missing===true,moduleSHA256:crypto.createHash('sha256').update(fs.readFileSync(dir+file)).digest('hex')}));
"""
            for case in ["positive", "absent", "malformed", "workspace"]:
                name = "aok-openclaw-" + nonce + "-" + case
                owned_containers.append(name)
                r = docker([
                    "run", "--rm", "-i", "--name", name, "--label", "aok.native-loading=" + nonce, "--network=none", "--read-only",
                    "--user=1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g",
                    "--tmpfs", "/probe:rw,nosuid,size=128m,mode=1777", "--tmpfs", "/tmp:rw,nosuid,size=128m,mode=1777",
                    "--mount", "type=volume,source=" + volume + ",target=/opt/probe,readonly",
                    "--env", "HOME=/probe/home", "--env", "OPENCLAW_STATE_DIR=/probe/home/.openclaw",
                    image, "sh", "-c", prelude + ("node --input-type=module -e " + shlex.quote(workspace_code) if case == "workspace" else "node /opt/probe/node_modules/openclaw/openclaw.mjs agents list --json"),
                ], data=archive(case), timeout=45, check=False)
                if case != "malformed" and r.returncode != 0:
                    raise RuntimeError("Native probe failed: " + case + ": " + r.stderr.decode(errors="replace")[:1500])
                stdout = r.stdout.decode(errors="replace")
                try:
                    parsed = json.loads(stdout)
                except ValueError:
                    if case != "malformed":
                        raise RuntimeError("Native CLI result " + case + ": exit=" + str(r.returncode) + "; stdout=" + stdout[:500] + "; stderr=" + r.stderr.decode(errors="replace")[:1500])
                    parsed = {"stderr": r.stderr.decode(errors="replace").strip()}
                observations[case] = {"exitCode": r.returncode, "result": parsed}
            positive = observations["positive"]["result"]
            if not isinstance(positive, list):
                raise RuntimeError("Native roster returned an error/envelope instead of agents: " + json.dumps(positive))
            matches = [a for a in positive if a.get("id") == expected_id and a.get("model") == entry["model"]]
            if len(matches) != 1 or matches[0].get("identityName") != entry["identity"]["name"]:
                raise RuntimeError("Generated native agent id/callsign/model was not discovered")
            absent = observations["absent"]["result"]
            if not isinstance(absent, list) or any(a.get("id") == expected_id for a in absent):
                raise RuntimeError("Absent-agent control did not remove the generated identity")
            malformed = observations["malformed"]["result"]
            if (observations["malformed"]["exitCode"] == 0 or not isinstance(malformed, dict)
                    or "OpenClaw config is invalid:" not in malformed.get("stderr", "")
                    or "JSON5 parse failed:" not in malformed.get("stderr", "")):
                raise RuntimeError("Malformed-config control did not report a native error")
            workspace = observations["workspace"]["result"]
            if workspace.get("contentSHA256") != sha(files[".openclaw/workspace/AGENTS.md"]) or not workspace.get("loaded") or not workspace.get("missingReported"):
                raise RuntimeError("Native workspace loader did not load the exact generated instructions/missing control")
            print(json.dumps({
                "target": "openclaw", "scope": "isolated-home", "observedAt": datetime.now(timezone.utc).isoformat(),
                "platform": "Linux container", "image": image, "package": package,
                "aokBinarySHA256": sha(aok.read_bytes()),
                "installation": "Pinned official npm package with its lifecycle scripts enabled inside the disposable install container; only its owned volume and /tmp writable; no host mounts", "nativeLoading": "verified", "cliRoster": "verified",
                "runtimeEnforcement": "unverified", "modelExecution": "not-run",
                "isolation": "Unprivileged runtime containers; network none; read-only root/package; tmpfs /probe and /tmp; no host mounts or credentials",
                "renderedFiles": {p: sha(data) for p, data in files.items()}, "observations": observations,
                "limitations": ["Version/scope/fixture-bound CLI discovery only; no inference or permission enforcement tested.",
                                "OpenClaw hardcodes /tmp coordination locks; they remain inside each container's private tmpfs namespace."],
            }, indent=2))
        finally:
            for name in owned_containers:
                if is_owned("container", name):
                    docker(["rm", "-f", name])
            if volume_created and is_owned("volume", volume):
                docker(["volume", "rm", volume])


if __name__ == "__main__":
    main()
