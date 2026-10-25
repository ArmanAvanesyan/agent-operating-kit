# WorkKit P3 toolchain and authentication

WorkKit P3 supplies tool discovery, scoped installation, credential lifecycle,
native authentication drivers and broker-backed reads in the existing AOK Go
module. `workkit` and `aok work` use the same SDK. Source/fixture conformance,
native certification, provider live qualification, migration and production
activation remain separate results. Standard builds contain no credentials,
trusted production enrollment or activated bindings.

## Commands and local configuration

```text
workkit tools doctor --tool PIN --as-of RFC3339
workkit tools doctor --discovery REQUEST --as-of RFC3339
workkit tools plan --artifact ARTIFACT --policy POLICY --destination NEW-PATH \
  --as-of RFC3339 --out NEW [--action update --previous-path OLD --previous-sha256 HASH]
workkit tools install|update --plan PLAN --policy POLICY --grant GRANT --out NEW
workkit tools verify --tool PIN --policy POLICY --grant GRANT --out NEW
workkit auth status
workkit auth status --configuration-file AUTH --receipt RECEIPT --as-of RFC3339
workkit auth login|refresh|logout ID --configuration-file AUTH \
  --policy POLICY --grant GRANT --out NEW
workkit connections add ID --configuration-file AUTH [--registry OLD --expected-sha256 HASH] \
  --as-of RFC3339 --out NEW
workkit connections remove ID --registry OLD --expected-sha256 HASH --as-of RFC3339 --out NEW
workkit connections list --registry REGISTRY
workkit connections inspect ID --registry REGISTRY
workkit connections list --workspace P1-WORKSPACE
workkit connections inspect ID --configuration-file AUTH
workkit connections test ID --connection-file P2-CONFIG --policy P2-POLICY \
  --enrollment P2-ENROLLMENT --out NEW
workkit connections test ID --configuration-file AUTH-WITH-READ \
  --policy ACCESS-POLICY --grant ACCESS-GRANT --out NEW
```

All accept `--json`, `--non-interactive` and a positive `--timeout` up to five
minutes. OAuth browser login blocks before a challenge when noninteractive.
Unknown/irrelevant flags reject before effects. No global force flag overrides
identity, scope, freshness or authority.

Connection add/remove produces a new immutable registry version; it preserves
original configuration bytes in base64 and binds their original SHA-256. The
expected prior hash prevents manual-edit conflicts. Registries enforce reader
size/count bounds and retain inactive migration/production flags. Operators
explicitly select the new standalone `.workkit` reference; commands never replace
suite configuration, activate a WorkBinding, revoke unrelated credentials or
transfer a sender namespace. Removal is not provider logout/revocation.

## Tool discovery, installation and verification

Discovery inspects only named gh/az/acli files beneath explicitly supplied roots.
It executes nothing, searches no credential/home stores automatically, and reports
unverified discoveries/unsafe files. Version strings are never inferred from
names. Byte-only doctor reports `verified:false`; a signed tools verify operation
runs a fixed version probe. Missing tools do not trigger installation.

Artifact policies pin source/version/platform, download SHA, extracted entry SHA,
format, layout, target and approved official redirect origins. Binary, ZIP and
tar.gz support include complete `tree-v1` runtimes; `azure-extension` maps the
official unprefixed Azure DevOps wheel into an `azure-devops` child beneath the
extension root. The official GitHub Azure extension release URL family and ZIP
wheel content are supported explicitly. Installed receipts expose executable or
extension pins; runtime/extension pins include all files, directory structure and
modes, and are rechecked at native admission/launch. Changing adjacent libraries
or adding files invalidates a pin.

Downloads use no credentials or ambient proxy. Source policy approves exact
artifacts, not arbitrary manifest scripts. ZIP central-directory allocation,
archive members/expansion, traversal, links/special files, duplicates, byte counts
and layout collisions are bounded/rejected. Unsupported ZIP64, MSI/DEB/RPM,
system-level installation and package-manager execution remain explicit v1
capability limits. There is no unpinned dependency fetching or pip/extension
installation during inspection or authentication. Operators/source policy must supply the required runtime/dependency closure.
Hash/tree and version checks do not prove every dynamically loaded dependency
is present; missing dependencies can fail loading or later conformance. An
extracted artifact is not a claim of usable vendor conformance.

Installs/updates use fresh private targets and adjacent exclusive locks. Updates
are side-by-side and preserve the prior hash-tracked target. No user-owned tool,
PATH/default, process or credential store is silently upgraded/replaced. Partial
staging retains locks/evidence for inspection; no automatic rollback is claimed.

## Access authority and durability

`kits/workkit/api/accessv1` defines closed `workkit-access/v1` records beside the
unchanged P1 domain and P2 read formats. Schemas are generated by
`go run ./scripts/workkit/access-schemas`; SDK validation adds identity, trust,
freshness and safe-path checks. A schema-valid declaration is not qualification.

Externally supplied access policies pin approved issuer keys, actions, connection
IDs, artifacts and a protected ledger root. Signed grants bind exact request,
policy and implementation digests, operation ID, action, absolute output and at
most five minutes of validity. `sdk.AccessSigningBytes` exposes the preimage.
The CLI does not manufacture real signing keys, independent approvals or root
custody. Policy files are external trust inputs; embedding one cannot self-qualify.

Fresh output and operation consumption precede credential access/IO. Connection
holds serialize auth transitions. Unix intent files/directories and complete
receipts are flushed before release. Windows uses a private SQLite pending/
terminal journal, with the existing pinned SQLite dependency, before IO;
committed receipts/state and hold release are transactional. Directory flush
failures are not ignored as a substitute for Windows durability. Windows private
roots use SID/DACL verification and new output roots receive protected ACLs.
Native NTFS/power-loss/ACL certification remains pending despite the implementation.

An interruption or uncertain provider/native-store response keeps consumption
and holds. There is no automatic resend, refresh retry or stale-PID lock removal.
Receipts/state must be durable before connection release. Local state references
exact receipt bytes; missing/tampered state blocks dependent reads. These are
local access journals, not AOK tracker/runtime/accounting/authority stores. The
operator still owns anti-rollback, revocation distribution, retention, recovery
and isolation from task identities.

Historical auth status is explicitly unverified metadata. It cannot grant
readiness/authorization, renew observation dates, contact a provider or refresh a
token. Principal observation does not establish tenant/container access; complete
P2 reads are recorded separately, and live qualification/active bindings remain
unestablished. Every access receipt retains migration blocked, live pending and
production not authorized.

## OS credential broker

Build explicitly; this never starts the broker or opens a credential store:

```text
python3 scripts/workkit/build_broker.py --go /absolute/pinned/go \
  --swiftc /absolute/pinned/swiftc --sdk /absolute/selected/MacOSX.sdk --out NEW
```

macOS requires Apple's Swift compiler for the small Security-framework helper;
Linux/Windows use Go backends. The build manifest records resolved compiler
hashes/version output, SDK metadata, pre/post source closure and binaries. Runtime ToolPins additionally bind
exact broker/helper/configuration hashes. Compiler/source hashes establish
traceability, not vendor authorship or native certification.

The broker's only private IPC operation is `exchange --config FILE
--config-sha256 HASH`; `version` is metadata-only. Configuration selects an exact
namespace, protected ledger, store and (on macOS) pinned helper. Linux requires
the explicitly configured current-user session bus and existing unlocked Secret
Service; requests do not auto-start or unlock it. macOS uses native Keychain APIs
with interaction disabled. Windows uses Credential Manager generic credentials
and its 2560-byte blob bound; oversized bundles fail closed, never fall back to
files. Go cross-builds and the Swift compile do not certify these native services.

Credentials are stored only in the native store. Files/SQLite contain locks and
non-secret operation/state metadata. CAS compares exact configuration-scoped
namespaces and opaque generations across processes. Creation cannot replace an
existing generation. Delete replaces payload with a nonce-bearing tombstone;
metadata-only status supports explicit re-login without resetting history.
Ambiguous native persistence/readback leaves the scope held. No private response,
token/code/verifier, signing key or credential fingerprint is an evidence asset.

The Go broker/Swift helper are bounded and use private pipes; nested helpers stay
inside the outer owned process group. Secure storage alone does not isolate
same-user workers. Protected identities/control proofs and native qualification
remain separate prerequisites. No broker daemon, login UI or protected AOK
service is automatically provisioned/started.

## Authentication routes

| Route | Implemented behavior | Independent gates/limits |
|---|---|---|
| GitHub token | Explicit credential reference and fixed identity request | Exact account/host/read scope live qualification pending |
| gh native | Explicit token-injected native API read in a pinned isolated profile | Fresh externally issued inherited-control enrollment; canonical hosts only, no explicit ports/base paths |
| Azure PAT/Entra | Fixed Services identity endpoint and exact organization lookup | Organization/project access remains a separate scoped-read observation |
| az native | PAT-injected native GET with exact CLI/runtime and extension-root pins | No dynamic extension install, global defaults or ambient Python user site; live route pending |
| Jira Cloud basic | Separate username/token references and immutable account ID | Scope/operation qualification pending |
| acli hybrid | Pinned native status plus explicitly declared Jira basic identity request | Signed opaque status hash/account mapping; no invented email-to-ID parsing, no implicit SSO fallback |
| Jira Cloud OAuth | Confidential app code/rotating-refresh flow, exact accessible site/scopes and API gateway | Registered app/client secret/broker and independent conformance required; no undocumented remote revocation API is invented |
| Jira Data Center | Instance/base path/API and profile-bound CA/server pins | Cloud evidence does not transfer; instance/TLS qualification pending |
| Linear API key/OAuth | Workspace/principal identity, PKCE/state callback, explicit rotating refresh/revocation | Registered app/broker and independently scoped route qualification pending |

Native drivers never mutate global CLI caches, switch accounts or infer secure
storage from a login exit. They require an externally issued short-lived native
control enrollment binding the current parent process/binary, operation,
configuration scope, tool/profile hashes, purpose and observed controls.
`sdk.NativeSigningBytes` defines the signature preimage. The runtime inherits
those external controls; the record itself does not install a sandbox or prove
reviewer independence. Missing/stale/mismatched controls block before credentials.
Native SSO/cache mutation and undocumented native interfaces are unsupported
capabilities, not emulated task files or advertised successful logins.

Token/native login observes the exact principal and optionally imports the
explicit credential into WorkKit's OS broker. Non-OAuth refresh means explicit
replacement of a broker-owned externally supplied token after identity checks;
without a broker it is unsupported and blocks before consumption. OAuth refresh
calls the provider once and CASes the rotating pair; ambiguous effects are never
retried. Logout detaches WorkKit state/removes its owned token; Linear can revoke
through its documented endpoint. External references and vendor-global sessions
are not silently revoked or deleted.

## Broker-backed P2 reads

An auth configuration may contain `read` pins for exact P2 configuration, policy,
enrollment and a distinct new capture output. `connections test` additionally
requires a signed access grant. Inputs are captured once before credentials;
P2 consumes the immutable byte snapshot rather than reopening mutable paths.
Provider, profile/deployment, tenant/container and native principal must agree.
Basic username references are explicitly bound and resolved privately; there is
no ambient fallback. Jira OAuth uses the declared cloud-ID gateway and preserves
per-request cancellation/deadlines. No read performs token refresh.

Partial P2 capture stays inspectable and returns partial/unknown conformance;
it cannot become a successful connection test. Authentication, API response
capture, independently live-qualified support and binding activation remain
separate states. Existing P2 entry points retain their own enrollment and effects.

See [delivery evidence](implementation/workkit-phase-3-delivery-evidence.md).

Protocol references: [GitHub CLI credential environment](https://cli.github.com/manual/gh_help_environment),
[Azure extension index](https://raw.githubusercontent.com/Azure/azure-cli-extensions/main/src/index.json),
[Atlassian OAuth](https://developer.atlassian.com/cloud/oauth/getting-started/making-calls-to-api/),
[Windows credential blob contract](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw).
These describe interfaces, not tested qualification for the local route.
