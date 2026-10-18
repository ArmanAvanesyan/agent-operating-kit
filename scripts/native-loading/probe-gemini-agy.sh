#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$script_dir/../.." && pwd)"
cd "$repo"

for command_name in go npm node jq perl rg python3 sandbox-exec shasum; do
  command -v "$command_name" >/dev/null || { echo "missing required command: $command_name" >&2; exit 2; }
done
agy_source="$(command -v agy || true)"
if [[ -z "$agy_source" ]]; then
  echo "agy CLI is not installed; install is intentionally outside this probe" >&2
  exit 2
fi
probe_path="/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin:$(dirname "$(command -v go)"):$(dirname "$(command -v npm)"):$(dirname "$(command -v node)")"

probe_root="$(mktemp -d "$repo/.aok-google-loading.XXXXXX")"
cleanup_probe_root() {
  /usr/bin/python3 -B - "$repo" "$probe_root" <<'PY'
import os
import shutil
import stat
import sys
from pathlib import Path

repo = Path(sys.argv[1]).resolve(strict=True)
root = Path(sys.argv[2]).resolve(strict=True)
if root.parent != repo or not root.name.startswith(".aok-google-loading."):
    raise SystemExit("refusing to remove an unexpected probe directory")
for base, dirs, files in os.walk(root):
    os.chmod(base, stat.S_IRUSR | stat.S_IWUSR | stat.S_IXUSR)
    # Removing read-only files requires writable parent directories only.
    # Do not chmod file symlinks, whose targets can be outside the probe tree.
shutil.rmtree(root)
PY
}
trap cleanup_probe_root EXIT
resolved_probe_root="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$probe_root")"
sandbox_profile="(version 1)(allow default)(deny file-write*)(allow file-write* (subpath \"$resolved_probe_root\") (literal \"/dev/null\") (literal \"/dev/tty\"))"
mkdir -p "$probe_root/home-gemini/.gemini" "$probe_root/npm-cache" "$probe_root/npm-prefix" \
  "$probe_root/npm-global-prefix" \
  "$probe_root/gemini-project/.gemini/agents" "$probe_root/gemini-malformed/.gemini/agents" \
  "$probe_root/agy-home" "$probe_root/agy-project" "$probe_root/agy-empty-project" \
  "$probe_root/build-home" "$probe_root/go-cache" "$probe_root/go-modcache" "$probe_root/go-path" \
  "$probe_root/aok-home" "$probe_root/build-source"
printf '%s\n' '{"general":{"enableAutoUpdate":false,"enableAutoUpdateNotification":false},"experimental":{"enableAgents":true},"privacy":{"usageStatisticsEnabled":false}}' \
  > "$probe_root/home-gemini/.gemini/settings.json"
printf '%s\n' 'registry=https://registry.npmjs.org/' > "$probe_root/npm-userconfig"
: > "$probe_root/npm-globalconfig"

run_sandboxed() {
  /usr/bin/sandbox-exec -p "$sandbox_profile" "$@"
}

probe_env_json="$(jq -cn \
  --arg path "$probe_path" \
  --arg home "$probe_root/build-home" \
  --arg gocache "$probe_root/go-cache" \
  --arg gomodcache "$probe_root/go-modcache" \
  --arg gopath "$probe_root/go-path" \
  --arg tmp "$probe_root" \
  --arg npm_prefix "$probe_root/npm-global-prefix" \
  --arg npm_cache "$probe_root/npm-cache" \
  --arg npm_userconfig "$probe_root/npm-userconfig" \
  --arg npm_globalconfig "$probe_root/npm-globalconfig" \
  '{PATH:$path,HOME:$home,GOCACHE:$gocache,GOMODCACHE:$gomodcache,GOPATH:$gopath,GOENV:"off",GOTOOLCHAIN:"local",GOPROXY:"https://proxy.golang.org,direct",TMPDIR:$tmp,TMP:$tmp,TEMP:$tmp,NPM_CONFIG_PREFIX:$npm_prefix,npm_config_prefix:$npm_prefix,NPM_CONFIG_CACHE:$npm_cache,npm_config_cache:$npm_cache,NPM_CONFIG_USERCONFIG:$npm_userconfig,NPM_CONFIG_GLOBALCONFIG:$npm_globalconfig,NPM_CONFIG_REGISTRY:"https://registry.npmjs.org/",npm_config_registry:"https://registry.npmjs.org/",npm_config_fetch_timeout:"30000",npm_config_fetch_retries:"0"}')"

run_confined_command() {
  local write_root="$1" cwd="$2" timeout_seconds="$3"
  shift 3
  PYTHONPATH="$script_dir" AOK_PROBE_ENV_JSON="$probe_env_json" PYTHONDONTWRITEBYTECODE=1 /usr/bin/python3 -B \
    - "$write_root" "$cwd" "$timeout_seconds" "$@" <<'PY'
import json
import os
import subprocess
import sys
from native_confinement import run_confined

root, cwd, timeout = sys.argv[1], sys.argv[2], int(sys.argv[3])
command = sys.argv[4:]
try:
    result = run_confined(command, root=root, cwd=cwd,
                          env=json.loads(os.environ["AOK_PROBE_ENV_JSON"]), timeout=timeout)
except subprocess.TimeoutExpired:
    print("confined command timed out", file=sys.stderr)
    raise SystemExit(124)
sys.stdout.write(result.stdout)
sys.stderr.write(result.stderr)
raise SystemExit(result.returncode)
PY
}

# Copy only the Go module inputs into the private build root. The build and all
# caches then stay under probe_root while native_confinement guards the build.
cp "$repo/go.mod" "$repo/go.sum" "$probe_root/build-source/"
cp -R "$repo/cmd" "$repo/internal" "$probe_root/build-source/"
run_confined_command "$probe_root" "$probe_root/build-source" 120 go build -o "$probe_root/aok" ./cmd/aok
make_target_corpus() {
  local target="$1" model="$2" destination="$3"
  cp -R internal/agentresolve/testdata/corpus "$destination"
  jq --arg target "$target" --arg model "$model" \
    '.client.adapter = $target | .model.selector = $model' \
    "$destination/execution-profile.json" > "$destination/execution-profile.tmp"
  mv "$destination/execution-profile.tmp" "$destination/execution-profile.json"
  jq 'del(.instructions.parameters)' "$destination/agent.json" > "$destination/agent.tmp"
  mv "$destination/agent.tmp" "$destination/agent.json"
}

expect_blocked_export() {
  local target="$1" corpus="$2" output="$3" report="$4"
  set +e
  run_confined_command "$probe_root" "$probe_root" 30 "$probe_root/aok" agent native export --target "$target" --scope project \
    --input "$corpus/assignment.json" --root "$corpus" --out "$output" --json > "$report"
  local status=$?
  set -e
  [[ "$status" -eq 1 ]] || { echo "$target export returned $status; expected blocked status 1" >&2; exit 1; }
  [[ -s "$output/aok-native.json" ]] || { echo "$target export did not write bundle" >&2; exit 1; }
}

# Gemini CLI is installed from the official npm registry into a private prefix
# and cache. Runtime calls disable automatic updates in documented settings,
# direct all npm configuration to the temp root, and deny file writes outside
# the resolved temp root (including updater child processes).
make_target_corpus gemini gemini-2.5-flash "$probe_root/gemini-corpus"
run_confined_command "$probe_root" "$probe_root" 120 "$(command -v npm)" install \
  --prefix "$probe_root/npm-prefix" --ignore-scripts --no-audit --no-fund \
  @google/gemini-cli@0.61.0 >/dev/null
gemini_version="$(run_sandboxed /usr/bin/env -i PATH="$probe_path" \
  HOME="$probe_root/home-gemini" NPM_CONFIG_PREFIX="$probe_root/npm-global-prefix" \
  npm_config_prefix="$probe_root/npm-global-prefix" NPM_CONFIG_CACHE="$probe_root/npm-cache" \
  npm_config_cache="$probe_root/npm-cache" NPM_CONFIG_USERCONFIG="$probe_root/npm-userconfig" \
  NPM_CONFIG_GLOBALCONFIG="$probe_root/npm-globalconfig" \
  /usr/bin/perl -e 'alarm 20; exec @ARGV' "$probe_root/npm-prefix/node_modules/.bin/gemini" --version)"
[[ "$gemini_version" == "0.61.0" ]] || { echo "unexpected Gemini version: $gemini_version" >&2; exit 1; }
run_sandboxed /usr/bin/env -i PATH="$probe_path" \
  HOME="$probe_root/home-gemini" NPM_CONFIG_PREFIX="$probe_root/npm-global-prefix" \
  npm_config_prefix="$probe_root/npm-global-prefix" NPM_CONFIG_CACHE="$probe_root/npm-cache" \
  npm_config_cache="$probe_root/npm-cache" NPM_CONFIG_USERCONFIG="$probe_root/npm-userconfig" \
  NPM_CONFIG_GLOBALCONFIG="$probe_root/npm-globalconfig" \
  /usr/bin/perl -e 'alarm 20; exec @ARGV' "$probe_root/npm-prefix/node_modules/.bin/gemini" --help > "$probe_root/gemini-help.txt"
sed -n '/^Commands:/,/^Positionals:/p' "$probe_root/gemini-help.txt" > "$probe_root/gemini-commands.txt"
if rg -qi '^[[:space:]]+gemini[[:space:]]+(agents?|subagents?)([[:space:]]|$)' "$probe_root/gemini-commands.txt"; then
  gemini_help_mentions_agents=true
else
  gemini_help_mentions_agents=false
fi
expect_blocked_export gemini "$probe_root/gemini-corpus" "$probe_root/gemini-export" "$probe_root/gemini-export-report.json"
cp "$probe_root/gemini-export/.gemini/agents/repository-explorer.md" \
  "$probe_root/gemini-project/.gemini/agents/"
printf '%s\n' '---' 'name: duplicate' 'name: duplicate-again' 'description: malformed control' '---' 'Body' \
  > "$probe_root/gemini-malformed/.gemini/agents/bad.md"
bundle_dir="$probe_root/npm-prefix/node_modules/@google/gemini-cli/bundle"
run_sandboxed /usr/bin/env -i PATH="$probe_path" HOME="$probe_root/home-gemini" \
  node scripts/native-loading/gemini-source-loader.mjs "$bundle_dir" \
  "$probe_root/gemini-project/.gemini/agents" "$probe_root/gemini-malformed/.gemini/agents" \
  "$probe_root/gemini-missing/.gemini/agents" > "$probe_root/gemini-loader-result.json"
jq -e '.positive.agentCount == 1 and .positive.agents[0].name == "repository-explorer" and .positive.errorCount == 0 and .malformed.agentCount == 0 and .malformed.errorCount > 0 and .missing.agentCount == 0 and .missing.errorCount == 0' \
  "$probe_root/gemini-loader-result.json" >/dev/null || { echo "Gemini loader probe failed" >&2; exit 1; }
gemini_sha="$(shasum -a 256 "$probe_root/gemini-project/.gemini/agents/repository-explorer.md" | awk '{print $1}')"
gemini_bundle_sha="$(jq -r '.sourceHashes[] | select(.path == ".gemini/agents/repository-explorer.md") | .sha256' "$probe_root/gemini-export-report.json")"
[[ "$gemini_sha" == "$gemini_bundle_sha" ]] || { echo "Gemini fixture hash differs from export report" >&2; exit 1; }
jq -n --arg checkedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --arg version "gemini-cli@$gemini_version" \
  --arg sha256 "$gemini_sha" --slurpfile loader "$probe_root/gemini-loader-result.json" \
  --argjson helpMentionsAgents "$gemini_help_mentions_agents" \
  '{target:"gemini",checkedAt:$checkedAt,hostVersion:$version,probeKind:"bundled-source-loader",scope:"project",fixture:{path:".gemini/agents/repository-explorer.md",sha256:$sha256},loader:$loader[0],cliSurface:{command:"gemini --help",exitCode:0,agentListingCommandMentioned:$helpMentionsAgents,discoveryReason:"help exposes no documented agent listing command; the CLI defaults to interactive mode, which may require authentication and model traffic, so no session was started"},cliDiscovery:"not-run",modelInvocation:"not-run",runtimeEnforcement:"unverified",sources:["https://geminicli.com/docs/core/subagents/","https://github.com/google-gemini/gemini-cli/blob/v0.61.0/packages/core/src/agents/agentLoader.ts"]}' \
  > docs/evidence/native-adapters/gemini-cli-0.61.0-source-loader.json

# AGY is copied before execution: this host self-updated after an earlier help
# probe. All further writes by the copied executable and its app data stay in
# this private tree. Run only the documented roster/list command, never an agent.
make_target_corpus agy flash "$probe_root/agy-corpus"
expect_blocked_export agy "$probe_root/agy-corpus" "$probe_root/agy-export" "$probe_root/agy-export-report.json"
cp -R "$probe_root/agy-export/.agents" "$probe_root/agy-project/"
agy_source_sha="$(shasum -a 256 "$agy_source" | awk '{print $1}')"
cp "$agy_source" "$probe_root/agy-copy"
chmod u+x "$probe_root/agy-copy"
agy_version="$(run_sandboxed /usr/bin/env -i PATH="$probe_path" HOME="$probe_root/agy-home" \
  XDG_CONFIG_HOME="$probe_root/agy-home/.config" XDG_DATA_HOME="$probe_root/agy-home/.local/share" \
  TMPDIR="$probe_root" TMP="$probe_root" TEMP="$probe_root" \
  NPM_CONFIG_PREFIX="$probe_root/npm-global-prefix" npm_config_prefix="$probe_root/npm-global-prefix" \
  NPM_CONFIG_CACHE="$probe_root/npm-cache" npm_config_cache="$probe_root/npm-cache" \
  NPM_CONFIG_USERCONFIG="$probe_root/npm-userconfig" NPM_CONFIG_GLOBALCONFIG="$probe_root/npm-globalconfig" \
  perl -e 'alarm 15; exec @ARGV' "$probe_root/agy-copy" --version)"
run_agy_roster() {
  local cwd="$1" name="$2"
  mkdir -p "$probe_root/$name"
  set +e
  (cd "$cwd" && run_sandboxed /usr/bin/env -i PATH="$probe_path" \
    HOME="$probe_root/$name-home" XDG_CONFIG_HOME="$probe_root/$name-home/.config" \
    XDG_DATA_HOME="$probe_root/$name-home/.local/share" TMPDIR="$probe_root" TMP="$probe_root" TEMP="$probe_root" \
    NPM_CONFIG_PREFIX="$probe_root/npm-global-prefix" npm_config_prefix="$probe_root/npm-global-prefix" \
    NPM_CONFIG_CACHE="$probe_root/npm-cache" npm_config_cache="$probe_root/npm-cache" \
    NPM_CONFIG_USERCONFIG="$probe_root/npm-userconfig" NPM_CONFIG_GLOBALCONFIG="$probe_root/npm-globalconfig" \
    perl -e 'alarm 15; exec @ARGV' "$probe_root/agy-copy" agent) > "$probe_root/$name.stdout" 2> "$probe_root/$name.stderr"
  local status=$?
  set -e
  printf '%s' "$status" > "$probe_root/$name.exit"
}
run_agy_roster "$probe_root/agy-project" agy-positive
run_agy_roster "$probe_root/agy-empty-project" agy-empty
agy_sha="$(shasum -a 256 "$probe_root/agy-project/.agents/agents/repository-explorer/agent.md" | awk '{print $1}')"
positive_exit="$(cat "$probe_root/agy-positive.exit")"
empty_exit="$(cat "$probe_root/agy-empty.exit")"
positive_bytes="$(wc -c < "$probe_root/agy-positive.stdout" | tr -d ' ')"
empty_bytes="$(wc -c < "$probe_root/agy-empty.stdout" | tr -d ' ')"
positive_stderr="$(wc -c < "$probe_root/agy-positive.stderr" | tr -d ' ')"
jq -n --arg checkedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --arg version "agy@$agy_version" \
  --arg sha256 "$agy_sha" --argjson positiveExit "$positive_exit" --argjson emptyExit "$empty_exit" \
  --argjson positiveBytes "$positive_bytes" --argjson emptyBytes "$empty_bytes" --argjson stderrBytes "$positive_stderr" \
  --arg sourceSha256 "$agy_source_sha" \
  '{target:"agy",checkedAt:$checkedAt,hostVersion:$version,observedVersionDiscrepancy:{before:"1.3.6",after:"agy@1.4.2",cause:"The earlier change followed non-isolated help/list invocations; attribution to an updater is probable but not proven."},priorProbeRestoration:{restoredVersion:"agy@1.3.6",restoredSHA256:"2a6bd844ae3431d35203af44f31bdd5e26f5ffe287d47c62a46862bf44099fc2",updatedExecutableSHA256BeforeRestore:"fa85a90abded17bcd1b185bbd1d6b1b82065f093ad4700d4dbacd3be2433294c",copyVersionProbe:{exitCode:0,writeGuarded:true,backupHashUnchanged:true},originalExecutableInvokedDuringAudit:false},probeKind:"CLI roster command on private executable copy",scope:"project",fixture:{path:".agents/agents/repository-explorer/agent.md",sha256:$sha256},sourceExecutableSha256:$sourceSha,positive:{command:"agy agent",exitCode:$positiveExit,stdoutBytes:$positiveBytes,stderrBytes:$stderrBytes},emptyControl:{command:"agy agent",exitCode:$emptyExit,stdoutBytes:$emptyBytes},loadingStatus:"not-verified",reason:"the documented roster command exited successfully but returned no output with either the generated fixture or the empty control project",modelInvocation:"not-run",runtimeEnforcement:"unverified",sources:["https://www.antigravity.google/docs/cli/commands/agents/","https://antigravity.google/docs/cli/subagents"]}' \
  > "docs/evidence/native-adapters/agy-${agy_version}.json"

printf 'Gemini source-loader evidence: %s\n' docs/evidence/native-adapters/gemini-cli-0.61.0-source-loader.json
printf 'AGY roster blocker evidence: %s\n' "docs/evidence/native-adapters/agy-${agy_version}.json"
