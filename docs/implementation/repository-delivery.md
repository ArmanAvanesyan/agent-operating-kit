# Repository delivery integration design and acceptance

## Ownership and decisions

The approved design embeds one authoritative Python engine and eight guides in
`packs/repository-delivery/`, with a relative launcher and 25 versioned evidence
schemas. Go performs dependency discovery and argument-vector routing only.
This avoids a preservation rewrite and a mandatory Python dependency for AOK.
No sibling-repository lookup or runtime download is permitted.

Pack conventions supply neutral manifest/content, sourceRefs, marketplace metadata
and optional skillRoot. Both renderers/validators share that additive contract.
Codex points at bundled canonical skills; Claude/Cursor wrappers point at their
bundled guides. Support paths must be relative and safe; development artifacts
are excluded. Existing packs retain behavior absent skillRoot.

The old ship-pr policy is replaced with a compatibility entrypoint; no blanket
staging, assumed gates or unconditional merge/delete remains. Root portable
schemas retain ownership. The imported schema fixture tests historical compatibility.

## Migration sequence and acceptance matrix

| Phase | Implementation | Acceptance |
|---|---|---|
| 1 Design/provenance | Baselines, imported hashes, ownership and overlap mapping | Original checkouts/retained worktrees observed before writes |
| 2 Embedded capability | Python payload, Go bridge, local dependency diagnostics, gateway | Routing, strict preservation, stale evidence, authority and optional dependency regressions |
| 3 Distribution | skillRoot, safe support copies, manifests/wrappers, release inclusion | Both renderers/all seven targets, relocated helpers, installed binary outside source trees |
| 4 Closeout | Docs, compatibility evidence, review, focused open PR | AOK suites, >=80% delivery coverage, pinned isolated runtime tests, green CI |

Actual executed gates and exceptions are recorded in the validation report;
this matrix is an acceptance contract, not a claim that unexecuted checks passed.
The planned integration is complete only to the extent demonstrated there.

## Compatibility and remaining proposals

AOK input commit: `40fe464c9f19626d4712a226771435c35076a04e`.
repo-delivery input: `cf7bae5f45dba2473bf6f8134128e6c5e1f819e7`.
swarmd inspected reference: `0a4298b1cbbc3cd848f20a97527752e270abc6c3`.
Historical compatibility pin: `f7a515320f4e939e608b13c15dca8b7f4aa25b88`.

Use clean immutable exports, fresh databases and test-only producers outside
original checkouts for compatibility. Public CI has no mandatory private secret.
Unavailable real runtime checks are unexecuted, never presumed passing.
Windows certification, producer/heartbeat/recovery, live AOK submission, automatic
routing and dependency installation are proposals/out of scope. Existing legacy
distribution and all retained private checkpoints remain in place.
