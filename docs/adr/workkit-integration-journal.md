# WorkKit integration journal and authority ownership

Status: P5 implementation decision, October 26, 2026.

WorkKit owns the future tracker/setup/sync integration coordinator for explicitly
enrolled new namespaces. Existing AOK outbox and provisioning namespaces remain
with their current owners until an explicit cutover. The P3 access journal owns
only local authentication/install operations. Runtime claims, capacity,
verification, identity/anchor services and acceptance remain external authorities.
Deliverkit may own disjoint Git/PR namespaces; it receives no tracker namespace
implicitly.

A fresh WorkKit SQLite identity reuses the existing AOK outbox schema and
consumption code, adding only plan/evidence, route-state, field-ownership and
local-control records. No legacy database is auto-upgraded or repaired. Canonical
path and random store identity are protected-enrollment inputs. Current ownership
and anti-rollback checks must survive local restore; SQLite alone cannot establish
external retention. New namespaces do not migrate outstanding operations.

Every send consumes exact one-use authority durably before I/O. Sync admission
records pending operations before effects; pause and send admission serialize
within the same journal. Fences, grants/nonces, revocations, referenced assets,
uncertain sends, correlation identities and pending plan lineage are retained.
Readback may settle an obligation but cannot manufacture a resend or rollback.
Existing provisioning receives delegated plans through its original boundary,
without a parallel WorkKit outbox for those effects.

Activation uses the existing v1 `provider-send` boundary with domain-specific
logical IDs/digests. Unsupported boundary strings remain rejected. Neither new
record schemas nor fixtures broaden activation policy. Protected bootstrap must
establish disjoint ownership across stores and revalidate route qualification,
current policy and revocation at admission and immediately before transport.

Migration activation is blocked until protected state inspection, preservation,
quiescence, unresolved-obligation reconciliation, external retention and explicit
namespace transfer are independently verified. Production activation is outside
this decision and unauthorized. Live qualifications remain route-specific.
