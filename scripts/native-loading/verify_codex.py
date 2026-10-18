"""Capture Codex native-role visibility using a loopback-only provider sink.

The sink returns HTTP 400 and never produces model output. No model runs and
no remote API key is inherited. This tests request construction, not inference.
Usage: python3 scripts/native-loading/verify_codex.py --aok /abs/aok --codex /abs/codex
"""
import argparse
from datetime import datetime, timezone
import hashlib
import http.server
import json
from pathlib import Path
import subprocess
import tempfile
import threading


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", type=Path, required=True)
    parser.add_argument("--codex", type=Path, required=True)
    args = parser.parse_args()
    aok, codex = args.aok.resolve(strict=True), args.codex.resolve(strict=True)
    repo = Path(__file__).resolve().parents[2]
    fixture = repo / "internal/nativeadapters/codex/testdata/probe.toml"
    with tempfile.TemporaryDirectory(prefix="aok-codex-loading-") as temp:
        root = Path(temp).resolve()
        source = root / "input/.codex/agents/aok-probe.toml"
        source.parent.mkdir(parents=True)
        source.write_bytes(fixture.read_bytes())
        (root / "input/aok-native-input.json").write_text(json.dumps({
            "version": "aok-native-input/v1", "files": [{
                "path": ".codex/agents/aok-probe.toml", "format": "toml"}]}))
        home = root / "home"
        (home / ".codex").mkdir(parents=True)
        env = {"PATH": "/usr/bin:/bin", "HOME": str(home), "CODEX_HOME": str(home / ".codex"),
               "TMPDIR": str(root)}
        statuses = {}
        for op in [
            ["import", "--input", str(root / "input"), "--out", str(root / "draft")],
            ["export", "--input", str(root / "draft/aok-native.json"),
             "--source", str(root / "input"), "--out", str(root / "export")],
            ["compare", "--input", str(root / "export"),
             "--against", str(root / "export/aok-native.json")],
        ]:
            result = subprocess.run([str(aok), "agent", "native", op[0], "--target", "codex",
                                     "--scope", "project", *op[1:]], env=env, capture_output=True,
                                    text=True, timeout=30)
            statuses[op[0]] = json.loads(result.stdout)["status"]
            assert result.returncode == (0 if op[0] == "compare" else 1)
        exported = root / "export/.codex/agents/aok-probe.toml"
        generated = exported.read_bytes()
        captured = []

        class Sink(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                if length > 4 * 1024 * 1024:
                    self.send_error(413)
                    return
                captured.append(json.loads(self.rfile.read(length)))
                self.send_response(400)
                self.end_headers()
                self.wfile.write(b'{"error":{"message":"AOK capture only; no inference"}}')

            def log_message(self, *_):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Sink)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        observations = {}
        try:
            for feature in ("multi_agent", "multi_agent_v2"):
                for case in ("positive", "absent", "malformed"):
                    if case == "absent":
                        exported.unlink()
                    elif case == "malformed":
                        exported.write_text("name = [\n")
                    else:
                        exported.write_bytes(generated)
                    before = len(captured)
                    cmd = [str(codex), "exec", "--ephemeral", "--skip-git-repo-check",
                           "--sandbox", "read-only", "--enable", feature, "--json"]
                    for setting in [
                        'model_provider="aok_capture"', 'model="aok-no-inference"',
                        'analytics.enabled=false', 'feedback.enabled=false',
                        'model_providers.aok_capture.name="AOK loopback capture"',
                        f'model_providers.aok_capture.base_url="http://127.0.0.1:{server.server_port}/v1"',
                        'model_providers.aok_capture.wire_api="responses"',
                        'model_providers.aok_capture.requires_openai_auth=false',
                        'model_providers.aok_capture.request_max_retries=0',
                        'model_providers.aok_capture.stream_max_retries=0',
                    ]:
                        cmd.extend(["-c", setting])
                    cmd.append("Synthetic local request-construction probe. No work is requested.")
                    result = subprocess.run(cmd, cwd=root / "export", env=env,
                                            stdin=subprocess.DEVNULL, capture_output=True,
                                            text=True, timeout=30)
                    requests = captured[before:]
                    assert requests, "No loopback request captured; investigation required"
                    assert result.returncode != 0, "Capture sink must fail without inference"
                    names = []
                    for tool in requests[0].get("tools", []):
                        names.append(tool.get("name", tool["type"]))
                    text = json.dumps(requests, sort_keys=True)
                    observations[feature + ":" + case] = {
                        "exitCode": result.returncode, "requestCount": len(requests),
                        "tools": names, "generatedAgentIdentityVisible": "aok-probe" in text,
                        "generatedAgentDescriptionVisible": "Synthetic read-only discovery fixture" in text,
                        "malformedWarning": "malformed agent" in (result.stderr + result.stdout).lower(),
                        "requestSHA256": sha(text.encode()),
                    }
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
        version = subprocess.run([str(codex), "--version"], env=env, capture_output=True,
                                 text=True, timeout=15)
        assert version.returncode == 0
        print(json.dumps({
            "observedAt": datetime.now(timezone.utc).isoformat(),
            "aokBinarySHA256": sha(aok.read_bytes()),
            "target": "codex", "scope": "project", "hostVersion": version.stdout.strip(),
            "binarySHA256": sha(codex.read_bytes()), "fixture": str(fixture.relative_to(repo)),
            "sourceSHA256": sha(fixture.read_bytes()), "renderedSHA256": sha(generated),
            "cliResults": statuses, "observations": observations,
            "isolation": "Temporary HOME/CODEX_HOME/cwd; allow-listed environment without credentials; analytics/feedback disabled",
            "provider": "Ephemeral loopback HTTP sink; always returns HTTP 400; no model implementation or output",
            "nativeLoading": "not-run", "nativeParsing": "see prior app-server parser evidence", "modelExecution": "not-run",
            "priorParserEvidence": "docs/evidence/native-adapters/codex-0.162.1.json",
            "runtimeEnforcement": "unverified",
            "blocker": "Neither captured tool protocol exposed the generated role identity/description. Parser acceptance alone does not prove registration or selection.",
            "nextGate": "Use a supported no-inference custom-role discovery/selection interface that returns the generated identity; preserve version/scope evidence.",
            "source": "https://learn.chatgpt.com/docs/agent-configuration/subagents",
        }, indent=2))


if __name__ == "__main__":
    main()
