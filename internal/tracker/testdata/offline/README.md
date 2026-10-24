# Offline tracker examples

These four synthetic, non-private captures are fixture evidence. They establish
no real account permission, provider conformance, or runtime readiness. All
observation times are fixed at `2026-10-24T12:00:00Z`; import never renews them.

Each provider directory contains original response assets and their manifest,
an explicit complete packet mapping, a requested human-owned title edit, and
source/read-policy templates. Replace identities and external credential
references before live use. Fixture policies are examples, not credentials.

From the repository root, using a built Go `aok`:

```sh
aok tracker import --manifest internal/tracker/testdata/offline/github/manifest.json --out /new/path/github
aok tracker validate /new/path/github --json
aok tracker packet --snapshot /new/path/github --mapping internal/tracker/testdata/offline/github/mapping.json --out /new/path/packets
aok tracker plan-update --snapshot /new/path/github --mapping internal/tracker/testdata/offline/github/mapping.json --desired internal/tracker/testdata/offline/github/desired.json --as-of 2026-10-24T12:01:00Z --out /new/path/draft
```

Substitute `azure-devops`, `linear`, or `jira` for the other fixtures. Every
output directory must be new. Packets/bindings and update plans remain local,
and their reports always deny execution authorization.
