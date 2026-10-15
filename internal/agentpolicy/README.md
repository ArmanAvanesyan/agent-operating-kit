# Declared permission intersection

`Intersect` computes an inert declaration from agent permissions and optional
restrictions. It does not enforce capabilities or prove that a runner can
enforce them. Runtime enforcement remains the responsibility of the executor
or native host.

Filesystem values are exact symbolic scope names: lowercase slug tokens such
as `workspace`, `reports`, or `build-cache`. Path fragments, separators,
wildcards, and `.`/`..` are rejected. Network allowlist values are canonical
lowercase DNS hostnames or canonical IP literals; schemes, ports, wildcards,
and trailing-dot hostnames are rejected. Both categories intersect by exact
set membership and results are sorted.

Missing (`nil`) restrictions inherit. A supplied restriction is a full policy;
empty filesystem sets and absent action/resource rules therefore remove access.
Action defaults remain `deny`; decisions combine by `allow` <
`require-authorization` < `deny`, and any missing key yields `deny`. A mismatch
between enforcement owners for the same action/resource is an error, because
the policy layer cannot safely select the enforcement boundary. Network denial,
secret references-only mode, and denied external mutations remain the strongest
representable outcomes.
