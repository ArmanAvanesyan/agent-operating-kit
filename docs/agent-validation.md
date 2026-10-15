# Local agent validation

The Go CLI provides `aok agent validate <directory> [--json]`. Build it with
`go build -o build/aok ./cmd/aok`. The contributor Python entrypoint does not
provide this command; its existing commands continue to work.

A corpus contains canonical JSON resources. Put supporting instruction files,
task packets and JSON schemas under an `assets/` directory. File references are
relative to the owning resource and must remain inside the corpus. No imported
commands are executed, packages installed or remote schemas fetched.

Validation reports stable diagnostic codes, source paths, affected fields,
severity and explanations. Exit 0 means the supplied corpus is valid, 1 means
invalid, and 2 indicates command misuse. `--json` emits a machine-readable
report. Validation does not establish host availability, runtime enforcement,
readiness, authorization or successful execution.

The initial operational kinds are Agent, AgentAssignment, ExecutionProfile,
CapabilityRegistry, InvocationContract, ContextPolicy, OutputContract,
EnvironmentProfile, LifecyclePolicy, EvaluationContract and MigrationContract.
Schemas are embedded in the Go binary. Version-pinned document references
identify kind, ID and version. Capability references resolve through registry
entries with matching type and version. Named verification/template bindings
must be unambiguous. Policy declarations remain inert.

The historical [agent proposal](proposals/agent-spec/README.md) is preserved;
its illustrative examples are not operational corpora. See the
[delivery tracker](implementation/agent-foundations/README.md) for remaining
resolution, planning and native-adapter work.

To bound schema-engine exact arithmetic, numeric lexemes are limited to 1024
bytes and exponents to -10000 through 10000 before schema validation.
