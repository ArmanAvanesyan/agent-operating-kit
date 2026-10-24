# Enrolled verifier artifact custody

`Store` captures a validated protected policy and verifier ID at construction.
`Store.Ingest` independently validates an externally signed `ControlProof`
against those captured trust roots and requires it to bind the candidate digest,
snapshot digest, policy, pinned verifier and trusted controls issuer. The HTTP
handler also authenticates the opaque service credential before calling the
store. A bearer credential alone or a caller-supplied trust root cannot populate
the candidate store. `Store.Get` rehashes the full manifest inventory before
execution and after execution; the Docker backend also mounts that service-owned
bundle read-only.

The store uses a bounded archive, strict relative paths, regular files, exact
manifest inventory, content hashes, create-only digest paths and staging rename.
Those checks protect the service boundary from malformed or mutable worker
uploads. They do not make a directory immutable against a process with the same
UID or filesystem-administration authority. Deployment must run the enrolled
verifier/store under a separate protected UID, deny the worker write access to
the store and Docker config/socket, and mount candidate data read-only in the
qualified container engine. A same-UID filesystem race between hashing and
container launch remains an explicit deployment/OS-engine qualification gate;
this package does not claim to solve it in portable Go.

The Docker adapter's calculated controls identity includes the measured client
binary, daemon socket, empty private client-config directory, image digest,
resource limits and fixed isolation flags. The trusted controls issuer must
pin that calculated identity. Actual daemon, kernel, mount and cancellation
behavior still requires independent platform qualification before live use.
