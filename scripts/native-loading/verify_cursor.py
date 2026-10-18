"""Probe the shipped Cursor loader component; never invoke an agent or model.

Usage: python3 scripts/native-loading/verify_cursor.py --aok /abs/aok \
  --host-dir /abs/cursor-agent/versions/VERSION
Prints a sanitized evidence object. Uses a disposable HOME and project.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

ENTRY = 'var __webpack_exports__=__webpack_require__("./src/index.tsx")'
EXPOSE = 'module.exports=__webpack_require__("../local-exec/dist/index.js")'
MODULE = "../local-exec/dist/index.js"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", type=Path, required=True)
    parser.add_argument("--host-dir", type=Path, required=True)
    args = parser.parse_args()
    aok, host = args.aok.resolve(strict=True), args.host_dir.resolve(strict=True)
    repo = Path(__file__).resolve().parents[2]
    bundle = host / "index.js"
    original = bundle.read_bytes()
    assert original.decode().count(ENTRY) == 1, "Host startup changed; re-review instrumentation"
    assert "Vz:()=>di" in original.decode(), "Host loader export changed; re-review"
    fixture = repo / "internal/nativeadapters/cursor/testdata/reviewer.md"
    with tempfile.TemporaryDirectory(prefix="aok-cursor-component-") as temp:
        root = Path(temp).resolve()
        home = root / "home"
        home.mkdir()
        env = {"PATH": "/usr/bin:/bin", "HOME": str(home), "TMPDIR": str(root)}
        native_input = root / "input"
        source = native_input / ".cursor/agents/reviewer.md"
        source.parent.mkdir(parents=True)
        source.write_bytes(fixture.read_bytes())
        (native_input / "aok-native-input.json").write_text(json.dumps({
            "version": "aok-native-input/v1", "files": [{
                "path": ".cursor/agents/reviewer.md", "format": "markdown"}]}))
        statuses = {}
        operations = [
            ["import", "--input", str(native_input), "--out", str(root / "draft")],
            ["export", "--input", str(root / "draft/aok-native.json"),
             "--source", str(native_input), "--out", str(root / "export")],
            ["compare", "--input", str(root / "export"),
             "--against", str(root / "export/aok-native.json")],
        ]
        for operation in operations:
            result = subprocess.run([str(aok), "agent", "native", operation[0],
                                     "--target", "cursor", "--scope", "project", *operation[1:]],
                                    env=env, capture_output=True, text=True, timeout=30)
            statuses[operation[0]] = json.loads(result.stdout)["status"]
            assert result.returncode == (0 if operation[0] == "compare" else 1)
        # Execute the shipped webpack module factories unchanged. Only the CLI
        # startup entry is replaced in memory, so login/UI/model startup never runs.
        # Network transports are disabled before loading modules as a further bound.
        harness = root / "loader.cjs"
        harness.write_text('''
const fs=require('node:fs'),Module=require('node:module'),path=require('node:path');
const deny=()=>{throw new Error('network disabled for component probe')};
require('node:net').Socket.prototype.connect=deny;
require('node:http').request=deny;require('node:https').request=deny;
global.fetch=deny;
const file=process.argv[2],source=fs.readFileSync(file,'utf8');
const entry=''' + json.dumps(ENTRY) + ''', expose=''' + json.dumps(EXPOSE) + ''';
if(source.split(entry).length!==2)throw new Error('startup entry changed');
const patched=source.replace(entry,expose);
if(patched.replace(expose,entry)!==source)throw new Error('unexpected module mutation');
const m=new Module(file,module);m.filename=file;
m.paths=Module._nodeModulePaths(path.dirname(file));m._compile(patched,file);
(async()=>{const rows=await new m.exports.Vz(process.argv[3],()=>false).getAllSubagents();
 console.log(JSON.stringify(rows.map(x=>({name:x.name,description:x.description,
 model:x.model,prompt:x.prompt,permissionMode:x.permissionMode,tools:x.tools,isBackground:x.isBackground,fullPath:x.fullPath}))))
})().catch(e=>{console.error(e.message);process.exitCode=1});
''')
        exported = root / "export/.cursor/agents/reviewer.md"
        generated = exported.read_bytes()
        observations = {}
        for case in ("positive", "absent", "malformed", "quoted-scalar", "quoted-readonly", "blank-prompt"):
            if case == "absent":
                exported.unlink()
            elif case == "malformed":
                exported.write_text("Not a native frontmatter document.\n")
            elif case == "quoted-scalar":
                exported.write_text('---\nname: reviewer\ndescription: "Review: exact scalar"\nmodel: inherit\n---\nPrompt\n')
            elif case == "quoted-readonly":
                exported.write_text('---\nname: reviewer\nreadonly: "true"\n---\nPrompt\n')
            elif case == "blank-prompt":
                exported.write_text('---\nname: reviewer\n---\n  \n')
            else:
                exported.write_bytes(generated)
            result = subprocess.run([str(host / "node"), str(harness), str(bundle),
                                     str(root / "export")], env=env, cwd=root,
                                    capture_output=True, text=True, timeout=30)
            assert result.returncode == 0, "Native loader component failed"
            rows = json.loads(result.stdout)
            for row in rows:
                row["fullPath"] = row["fullPath"].replace(str(root), "$PROBE_ROOT")
            observations[case] = rows
        assert len(observations["positive"]) == 1
        row = observations["positive"][0]
        assert row["name"] == "reviewer" and row["model"] == "inherit"
        assert row["description"] == "Reviews changes for correctness and security."
        assert not observations["absent"] and not observations["malformed"] and not observations["blank-prompt"]
        assert observations["quoted-scalar"][0]["description"] == '"Review: exact scalar"'
        version = subprocess.run([str(host / "cursor-agent"), "--version"], env=env,
                                 capture_output=True, text=True, timeout=15)
        assert version.returncode == 0
        print(json.dumps({
            "observedAt": datetime.now(timezone.utc).isoformat(),
            "aokBinarySHA256": sha(aok.read_bytes()),
            "target": "cursor", "scope": "project", "hostVersion": version.stdout.strip(),
            "bundleSHA256": sha(original), "runtime": "bundled Node",
            "module": MODULE, "export": "Vz (LocalSubagentsService)",
            "instrumentation": "Only webpack startup entry replaced in memory; module factories unchanged; Node TCP connect, HTTP(S) request and fetch guarded",
            "fixture": str(fixture.relative_to(repo)), "sourceSHA256": sha(fixture.read_bytes()),
            "renderedSHA256": sha(generated), "cliResults": statuses,
            "observations": observations, "componentDiscovery": "verified",
            "nativeLoading": "not-run", "runtimeEnforcement": "unverified", "modelExecution": "not-run",
            "blocker": "No supported full CLI discovery interface was verified; component discovery is narrower than CLI startup/selection.",
            "adapterFix": "Reject mapped scalar values whose YAML semantics differ from this installed line-oriented host parser.",
        }, indent=2))


if __name__ == "__main__":
    main()
