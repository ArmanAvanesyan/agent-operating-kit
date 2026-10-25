# WorkKit P2 extraction decisions — 2026-10-25

Baseline: merged WorkKit P1 at `ff99ba2e43c5a650c25cccd6da1f779e12c6fac9`.
Historical P0/P1 captures and failed attempts are not rewritten.

1. `kits/workkit/readsource` owns the shared provider read planning/decoding,
   scoped request validation, reconstruction, pagination, provenance and merge
   logic extracted from `internal/tracker`. Legacy AOK public functions delegate
   to this implementation. `Source`, `Request`, `Response`, `Auth`, `Scope`,
   `Limits`, `Deployment` and `Checkpoint` are source-compatible aliases;
   private scope proof stays in the shared request type and is reconstructed
   from original pages, never accepted from JSON. Contract/projection types in
   AOK retain their original owners and wire formats. This is not a cross-build
   binary ABI guarantee.
2. Compatibility encoding types in readsource retain `aok-tracker/v1|v2` source
   identities and the old JSON-marshal digest algorithm. They are not a second
   canonical WorkKit domain or an authority. Explicit adapters preserve legacy
   contract types. The new operational `workkit-read/v1` records add the complete
   native identity, connection/principal context, exact inputs and lineage.
3. `kits/workkit/sdk` owns read application services and public ports. No ordinary
   kit package imports foreign AOK internals, credentials, databases or writers.
   Trusted standalone/umbrella bootstrap installs
   `integrations/legacy-aok/workkit.ReadBackend`. CLI and SDK use one service.
4. The operator read policy and signed, short-lived qualification-only enrollment
   are explicit external inputs. They are not selected by captured task content.
   Constructor success, fixtures, raw captures and signatures grant no live
   qualification, acceptance, workflow execution or production authority.
5. Existing tracker writers, outboxes, provisioning, runtime, capacity, protected
   identity/authority/anchor/verifier and delivery owners remain unchanged.
   Operational migration still requires preservation, quiescence and explicit
   namespace transfer. P2 implements no alternative sender or integration ledger.
6. Item/catalog data uses bounded assets instead of one configuration resource
   per item. Every reference is confined and hashed. Original provider numbers,
   IDs, revision types, observation dates and unsupported data remain inspectable.
   A normalization/output bound can produce unsealed failure evidence, never a
   partial active binding or silent success.
7. Portable queries are bounded local operations with explicit field mappings,
   exact scalar comparisons and three-valued unknown semantics. No inferred
   lifecycle mapping, native query pushdown, cross-account pooling or remote
   view mutation is introduced. Unsupported surfaces are reported as limits.
8. A signed resume binds the exact prior bundle and implementation. Inherited
   pages keep bounded originating-proof lineage; new enrollment does not renew
   their dates. Old artifacts remain historical evidence. Format/control changes
   require explicit evolution; no source observation or qualification is upgraded.
