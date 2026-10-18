#!/usr/bin/env bash
set -euo pipefail

# Safe, isolated loader probes for the Hermes profile and OpenClaw home adapters.
# This does not run Hermes' bootstrap launcher, execute config-provided commands,
# configure providers, or make model requests.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
hermes_source="${HERMES_SOURCE:?Set HERMES_SOURCE to the installed Hermes source checkout}"
hermes_python="${HERMES_PYTHON:?Set HERMES_PYTHON to an existing Hermes environment Python}"
hermes_runtime_site="${HERMES_RUNTIME_SITE_PACKAGES:?Set HERMES_RUNTIME_SITE_PACKAGES to the existing Hermes PM runtime site-packages}"
openclaw_node="${OPENCLAW_NODE:-$(command -v node)}"
npm_bin="$(command -v npm)"
sandbox_exec="$(command -v sandbox-exec || true)"
python_bin="$(command -v python3)"
if [[ -z "$sandbox_exec" ]]; then
	printf 'sandbox-exec is required for this Darwin probe\n' >&2
	exit 2
fi
node_path="$(dirname "$openclaw_node"):/usr/bin:/bin"
npm_path="$(dirname "$openclaw_node"):$(dirname "$npm_bin"):/usr/bin:/bin"

for required in "$hermes_source/hermes_cli/config.py" "$hermes_python" "$hermes_runtime_site" "$openclaw_node"; do
	if [[ ! -e "$required" ]]; then
		printf 'required probe input is missing: %s\n' "$(basename "$required")" >&2
		exit 2
	fi
done

tmp_base="$(mktemp -d "${TMPDIR:-/tmp}/aok-profile-load.XXXXXX")"
tmp_root="$(cd "$tmp_base" && pwd -P)"
cleanup_probe_root() {
	"$python_bin" -B - "$tmp_base" "$tmp_root" <<'CLEANUP_PY'
import os
from pathlib import Path
import shutil
import stat
import sys
candidate = Path(sys.argv[1])
root = Path(sys.argv[2])
if candidate.is_symlink() or candidate.resolve(strict=True) != root or not root.name.startswith("aok-profile-load."):
    raise SystemExit("refusing to remove an unexpected probe root")
# Go module-cache directories may be read-only. Chmod only walked directories;
# os.walk does not follow directory links, and file-link targets are untouched.
for base, _, _ in os.walk(root, followlinks=False):
    os.chmod(base, stat.S_IRUSR | stat.S_IWUSR | stat.S_IXUSR)
shutil.rmtree(root)
CLEANUP_PY
}
trap cleanup_probe_root EXIT
mkdir -p "$tmp_root/build" "$tmp_root/npm-home" "$tmp_root/npm-cache" "$tmp_root/npm-config"
sandbox_profile="(version 1)(allow default)(deny file-write*)(allow file-write* (subpath \"$tmp_root\") (literal \"/dev/null\") (literal \"/dev/tty\"))"
bounded_runner="$tmp_root/bounded-run.py"
cat >"$bounded_runner" <<'PY'
import os
import signal
import subprocess
import sys

limit = float(sys.argv[1])
command = sys.argv[2:]
if not command:
    raise SystemExit("bounded runner requires a command")
proc = subprocess.Popen(command, stdin=None, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
try:
    stdout, stderr = proc.communicate(timeout=limit)
except subprocess.TimeoutExpired:
    try:
        os.killpg(proc.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        proc.communicate(timeout=2)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        try:
            proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            if proc.stdout is not None:
                proc.stdout.close()
            if proc.stderr is not None:
                proc.stderr.close()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=2)
    print(f"command timed out after {limit:g}s", file=sys.stderr)
    raise SystemExit(124)
sys.stdout.buffer.write(stdout)
sys.stderr.buffer.write(stderr)
raise SystemExit(proc.returncode)
PY
bounded() {
	local limit="$1"
	shift
	"$python_bin" "$bounded_runner" "$limit" "$@"
}
if bounded 1 "$python_bin" -c 'import subprocess,time; subprocess.Popen(["/usr/bin/sleep","30"]); time.sleep(30)' >/dev/null 2>&1; then
	printf 'bounded-runner timeout self-check unexpectedly completed\n' >&2
	exit 1
else
	code=$?
	[[ "$code" -eq 124 ]]
fi
printf 'Bounded process-group timeout self-check: passed\n'

cd "$repo_root"
bounded 120 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$(dirname "$(command -v go)"):/usr/bin:/bin" \
	HOME="$tmp_root/build-home" TMPDIR="$tmp_root" TMP="$tmp_root" TEMP="$tmp_root" \
	GOCACHE="$tmp_root/build-cache" GOMODCACHE="$tmp_root/mod-cache" GOPATH="$tmp_root/go" \
	GOTOOLCHAIN=local go build -o "$tmp_root/aok" ./cmd/aok

# Generate OpenClaw files with the actual AOK adapter from an explicit, inert
# native inventory. The only unmapped field is a documented, non-executable
# update setting; the fixture contains no command, credential, provider call,
# plugin, channel, or tool configuration.
mkdir -p "$tmp_root/openclaw-input/.openclaw/workspace"
cat >"$tmp_root/openclaw-input/aok-native-input.json" <<'JSON'
{"version":"aok-native-input/v1","files":[{"path":".openclaw/openclaw.json","format":"json"},{"path":".openclaw/workspace/AGENTS.md","format":"markdown"}]}
JSON
cat >"$tmp_root/openclaw-input/.openclaw/openclaw.json" <<'JSON'
{"agents":{"entries":{"aokprobe":{"identity":{"name":"AOK Probe"},"model":"anthropic/claude-sonnet-4","workspace":"~/.openclaw/workspace"}}},"update":{"checkOnStart":false}}
JSON
cat >"$tmp_root/openclaw-input/.openclaw/workspace/AGENTS.md" <<'MD'
# AOK loading probe

This inert fixture is used only to verify native workspace file loading.
MD
printf 'OpenClaw source-file hashes:\n'
for f in "$tmp_root/openclaw-input/.openclaw/openclaw.json" "$tmp_root/openclaw-input/.openclaw/workspace/AGENTS.md"; do printf '%s ' "$(basename "$f")"; shasum -a 256 "$f" | cut -d ' ' -f 1; done

if bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/aok-home" TMPDIR="$tmp_root" \
	"$tmp_root/aok" agent native import --target openclaw --scope isolated-home --input "$tmp_root/openclaw-input" --out "$tmp_root/openclaw-import" >"$tmp_root/openclaw-import-report.json"; then
	:
else
	code=$?
	[[ "$code" -eq 1 && -s "$tmp_root/openclaw-import/aok-native.json" ]]
fi

# OpenClaw is a pinned official package in a disposable local prefix. Install
# lifecycle scripts are disabled; no global prefix or user npm config is used.
bounded 120 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$npm_path" HOME="$tmp_root/npm-home" TMPDIR="$tmp_root" \
	npm_config_cache="$tmp_root/npm-cache" \
	npm_config_userconfig="$tmp_root/npm-config/npmrc" \
	npm_config_globalconfig="$tmp_root/npm-config/global-npmrc" \
	npm_config_registry="https://registry.npmjs.org" \
	"$npm_bin" install --prefix "$tmp_root/openclaw-install" --ignore-scripts --no-audit --no-fund openclaw@2026.9.4 >/dev/null
cd "$tmp_root"

if bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/aok-home" TMPDIR="$tmp_root" \
	"$tmp_root/aok" agent native export --target openclaw --scope isolated-home \
	--input "$tmp_root/openclaw-import/aok-native.json" --source "$tmp_root/openclaw-input" \
	--out "$tmp_root/openclaw-export" >"$tmp_root/openclaw-export-report.json"; then
	:
else
	code=$?
	[[ "$code" -eq 1 && -s "$tmp_root/openclaw-export/.openclaw/openclaw.json" ]]
fi

mkdir -p "$tmp_root/openclaw-home/.openclaw/workspace"
cp "$tmp_root/openclaw-export/.openclaw/openclaw.json" "$tmp_root/openclaw-home/.openclaw/openclaw.json"
cp "$tmp_root/openclaw-export/.openclaw/workspace/AGENTS.md" "$tmp_root/openclaw-home/.openclaw/workspace/AGENTS.md"
openclaw_cli="$tmp_root/openclaw-install/node_modules/openclaw/openclaw.mjs"
openclaw_workspace_module="$(grep -l 'loadWorkspaceBootstrapFiles as _' "$tmp_root/openclaw-install/node_modules/openclaw/dist"/workspace-*.mjs | sed -n '1p')"
[[ -n "$openclaw_workspace_module" ]]
state_lock_module="$tmp_root/openclaw-install/node_modules/openclaw/dist/state-database-coordinator-DBce2evc.mjs"
state_lock_hash="$(shasum -a 256 "$state_lock_module" | cut -d ' ' -f 1)"
printf 'OpenClaw CLI roster result: '
if grep -Fq 'return process.platform === "win32" ? path.join(os.homedir(), "AppData", "Local", "OpenClaw", "locks") : "/tmp";' "$state_lock_module"; then
	printf '{"status":"blocked","reason":"pinned CLI writes state locks under hardcoded /tmp, outside strict temp-root sandbox","stateLockModuleSha256":"%s"}\n' "$state_lock_hash"
else
	printf '{"status":"not-run","reason":"pinned state-lock path is unrecognized; refusing native CLI execution"}\n'
fi
bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$node_path" HOME="$tmp_root/openclaw-home" TMPDIR="$tmp_root" \
	OPENCLAW_WORKSPACE_MODULE="$openclaw_workspace_module" \
	XDG_CONFIG_HOME="$tmp_root/openclaw-home/xdg-config" XDG_DATA_HOME="$tmp_root/openclaw-home/xdg-data" \
	XDG_CACHE_HOME="$tmp_root/openclaw-home/xdg-cache" XDG_STATE_HOME="$tmp_root/openclaw-home/xdg-state" \
	OPENCLAW_WORKSPACE="$tmp_root/openclaw-home/.openclaw/workspace" \
	OPENCLAW_MISSING_WORKSPACE="$tmp_root/no-such-workspace" \
	OPENCLAW_EXPECTED_FILE="$tmp_root/openclaw-export/.openclaw/workspace/AGENTS.md" \
	"$openclaw_node" --input-type=module - <<'NODE'
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
const module = await import(pathToFileURL(process.env.OPENCLAW_WORKSPACE_MODULE).href);
const files = await module._(process.env.OPENCLAW_WORKSPACE, ["AGENTS.md"]);
const found = files.find((file) => file.name === "AGENTS.md");
const missing = await module._(process.env.OPENCLAW_MISSING_WORKSPACE, ["AGENTS.md"]);
const loadedSha256 = found?.content ? createHash("sha256").update(found.content).digest("hex") : null;
const expectedSha256 = createHash("sha256").update(await readFile(process.env.OPENCLAW_EXPECTED_FILE)).digest("hex");
if (!found || found.missing || loadedSha256 !== expectedSha256 || missing.find((file) => file.name === "AGENTS.md")?.missing !== true) {
  throw new Error("OpenClaw workspace loader control failed");
}
console.log(JSON.stringify({
  loader: "loadWorkspaceBootstrapFiles",
  loaded: true,
  loadedSha256,
  missingReported: missing.find((file) => file.name === "AGENTS.md")?.missing === true
}));
NODE

mkdir -p "$tmp_root/openclaw-bad-home/.openclaw"
printf '{"agents":' >"$tmp_root/openclaw-bad-home/.openclaw/openclaw.json"
if bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$node_path" HOME="$tmp_root/openclaw-bad-home" TMPDIR="$tmp_root" \
	XDG_CONFIG_HOME="$tmp_root/openclaw-bad-home/xdg-config" XDG_DATA_HOME="$tmp_root/openclaw-bad-home/xdg-data" \
	XDG_CACHE_HOME="$tmp_root/openclaw-bad-home/xdg-cache" XDG_STATE_HOME="$tmp_root/openclaw-bad-home/xdg-state" \
	OPENCLAW_STATE_DIR="$tmp_root/openclaw-bad-home/.openclaw" \
	"$openclaw_node" "$openclaw_cli" agents list --json >"$tmp_root/openclaw-bad-stdout.txt" 2>"$tmp_root/openclaw-bad-stderr.txt"; then
	printf '{"malformedConfigRejected":false}\n'
	exit 1
else
	grep -Eiq 'JSON5.*invalid end of input|invalid end of input' "$tmp_root/openclaw-bad-stderr.txt" || {
		printf 'OpenClaw malformed-config control failed to produce a parse diagnostic\n' >&2
		exit 1
	}
	printf '{"malformedConfigRejected":true}\n'
fi

printf 'OpenClaw CLI: '
bounded 15 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$node_path" HOME="$tmp_root/openclaw-home" TMPDIR="$tmp_root" \
	XDG_CONFIG_HOME="$tmp_root/openclaw-home/xdg-config" XDG_DATA_HOME="$tmp_root/openclaw-home/xdg-data" \
	XDG_CACHE_HOME="$tmp_root/openclaw-home/xdg-cache" XDG_STATE_HOME="$tmp_root/openclaw-home/xdg-state" \
	OPENCLAW_STATE_DIR="$tmp_root/openclaw-home/.openclaw" \
	"$openclaw_node" "$openclaw_cli" --version
printf 'OpenClaw package integrity: '
bounded 15 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="$npm_path" HOME="$tmp_root/npm-home" TMPDIR="$tmp_root" \
	npm_config_registry="https://registry.npmjs.org" npm_config_cache="$tmp_root/npm-cache" \
	npm_config_userconfig="$tmp_root/npm-config/npmrc" \
	npm_config_globalconfig="$tmp_root/npm-config/global-npmrc" \
	"$npm_bin" view openclaw@2026.9.4 dist.integrity
printf 'OpenClaw rendered-file hashes:\n'
for f in "$tmp_root/openclaw-export/.openclaw/openclaw.json" "$tmp_root/openclaw-export/.openclaw/workspace/AGENTS.md"; do printf '%s ' "$(basename "$f")"; shasum -a 256 "$f" | cut -d ' ' -f 1; done
printf 'OpenClaw loader-module hash:\n'
shasum -a 256 "$openclaw_workspace_module" | cut -d ' ' -f 1
printf 'OpenClaw CLI hash:\n'
shasum -a 256 "$openclaw_cli" | cut -d ' ' -f 1
printf 'OpenClaw state-lock module hash:\n%s\n' "$state_lock_hash"

# Hermes probes call only the installed source loaders with an isolated home.
# They do not invoke the launcher or import a profile-provided executable.
if bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/aok-home" TMPDIR="$tmp_root" \
	"$tmp_root/aok" agent native import --target hermes-agent --scope profile \
	--input "$repo_root/internal/nativeadapters/hermes/testdata" --out "$tmp_root/hermes-import" \
	>"$tmp_root/hermes-import-report.json"; then
	:
else
	code=$?
	[[ "$code" -eq 1 && -s "$tmp_root/hermes-import/aok-native.json" ]]
fi
if bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/aok-home" TMPDIR="$tmp_root" \
	"$tmp_root/aok" agent native export --target hermes-agent --scope profile \
	--input "$tmp_root/hermes-import/aok-native.json" \
	--source "$repo_root/internal/nativeadapters/hermes/testdata" --out "$tmp_root/hermes-export" \
	>"$tmp_root/hermes-export-report.json"; then
	:
else
	code=$?
	[[ "$code" -eq 1 && -s "$tmp_root/hermes-export/SOUL.md" ]]
fi

mkdir -p "$tmp_root/hermes-home"
cp "$tmp_root/hermes-export/config.yaml" "$tmp_root/hermes-home/config.yaml"
cp "$tmp_root/hermes-export/profile.yaml" "$tmp_root/hermes-home/profile.yaml"
cp "$tmp_root/hermes-export/SOUL.md" "$tmp_root/hermes-home/SOUL.md"
bounded 30 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/hermes-home" TMPDIR="$tmp_root" \
	XDG_CONFIG_HOME="$tmp_root/hermes-home/xdg-config" XDG_DATA_HOME="$tmp_root/hermes-home/xdg-data" \
	XDG_CACHE_HOME="$tmp_root/hermes-home/xdg-cache" \
	HERMES_SOURCE="$hermes_source" HERMES_HOME="$tmp_root/hermes-home" \
	HERMES_FIXTURE_HOME="$tmp_root/hermes-home" PYTHONPATH="$hermes_source:$hermes_runtime_site" \
	PYTHONDONTWRITEBYTECODE=1 "$hermes_python" -B - <<'PY'
import contextlib
import hashlib
import io
import json
import logging
import os
from pathlib import Path

from agent.prompt_builder import load_soul_md
from hermes_cli.config import load_config_readonly
from hermes_cli.config_read_errors import get_active_config_parse_failure
from hermes_cli.profiles import read_profile_meta

home = Path(os.environ["HERMES_FIXTURE_HOME"])
config = load_config_readonly()
soul = load_soul_md(home_override=home)
meta = read_profile_meta(home)
expected_model = "anthropic/claude-sonnet-4"
expected_description = "Focused software engineering assistant."
expected_soul_hash = "caf511604e929596df1f13e2e89a976900b99ca8b998b955546925bf3419f4b6"
if config.get("model", {}).get("default") != expected_model or meta.get("description") != expected_description or hashlib.sha256((soul or "").encode()).hexdigest() != expected_soul_hash:
    raise SystemExit("Hermes positive loader controls failed")
print(json.dumps({
    "loaders": ["load_config_readonly", "read_profile_meta", "load_soul_md"],
    "loadedModelDefault": config.get("model", {}).get("default"),
    "loadedDescription": meta.get("description"),
    "soulLoaded": bool(soul),
    "soulLoadedSha256": hashlib.sha256((soul or "").encode()).hexdigest(),
    "activeParseFailure": bool(get_active_config_parse_failure()),
}))

missing = home.parent / "missing-profile"
missing_result = (load_soul_md(home_override=missing) is None and read_profile_meta(missing).get("description") == "")
if not missing_result:
    raise SystemExit("Hermes missing-profile control failed")
print(json.dumps({
    "missingSoul": True,
    "missingDescriptionEmpty": True,
}))

bad_home = home.parent / "malformed-home"
bad_home.mkdir()
(bad_home / "config.yaml").write_text("model:\n  default: [broken\n", encoding="utf-8")
os.environ["HERMES_HOME"] = str(bad_home)
logging.disable(logging.CRITICAL)
with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
    fallback_config = load_config_readonly()
malformed_recorded = bool(get_active_config_parse_failure())
fallback_is_default = isinstance(fallback_config.get("model"), dict) and fallback_config["model"].get("default") == ""
if not malformed_recorded or not fallback_is_default:
    raise SystemExit("Hermes malformed-config control was not recorded")
print(json.dumps({"malformedConfigRecorded": True, "fallbackModelDefaultEmpty": True}))
PY

printf 'Hermes source revision: '
bounded 15 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/hermes-home" TMPDIR="$tmp_root" git -C "$hermes_source" describe --tags --always
printf 'Hermes source commit: '
bounded 15 "$sandbox_exec" -p "$sandbox_profile" env -i PATH="/usr/bin:/bin" HOME="$tmp_root/hermes-home" TMPDIR="$tmp_root" git -C "$hermes_source" rev-parse HEAD
printf 'Hermes loader source hashes:\n'
for f in hermes_cli/config.py hermes_cli/profiles.py agent/prompt_builder.py; do printf '%s ' "$f"; shasum -a 256 "$hermes_source/$f" | cut -d ' ' -f 1; done
printf 'Hermes rendered-file hashes:\n'
for f in config.yaml profile.yaml SOUL.md; do printf '%s ' "$f"; shasum -a 256 "$tmp_root/hermes-export/$f" | cut -d ' ' -f 1; done
