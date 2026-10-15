# Setup and recovery

Inspect setup commands before running them. Preserve existing changes and package-manager
intent; use lockfiles and repository-declared runtime versions. Prefer an isolated local
environment where appropriate. Record necessary services, ports, fixtures, configuration
variable names and supply mechanisms without copying credentials into evidence.

Choose tools conditionally. ripgrep/ast-grep/Tokei serve exploration; missing structural
analysis does not necessarily block a runtime task. mise or uv may help when adopted by
the project; do not introduce them just because they are available. Remote access and
delivery readiness do not grant publication/deployment authority.

AOK's setup helper tolerates diagnostic failures and does not install application
requirements. Verify its report and actual application checks. Distinguish infrastructure,
missing prerequisites, pre-existing product failures and failures introduced by setup.

On failure, record the command/scenario, observed result, affected capability, attempted
hypothesis and what would unblock it. Avoid repeated attempts without new information.
Record cleanup for started services and created fixtures; undo only changes owned by this
setup. Preserve successful exploration when readiness is blocked. Do not invent rollback
for irreversible changes; do not run production migrations as routine development setup.
