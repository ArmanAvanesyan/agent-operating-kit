# Enrolled authority anchor service

`anchorserver` serves `POST /v1/anchor` for the enrolled authority's external
linearizable witness. Configure it only with an already existing private
directory, a pre-enrolled canonical `STREAM.00000000000000000000.json` genesis
record, its externally protected initial tip, a response signing key, and an
opaque token reference resolved by the service's secret broker. `New` never
creates a directory, stream, key, or genesis record. There is no reset,
truncate, or delete API. Every accepted advance is a create-only sequence file
written with `O_EXCL`, fsynced, and followed by a directory fsync. Corrupt,
partial, missing, or divergent history blocks service; it is never healed.

Production must expose the handler only through authenticated HTTPS, retain the
response public key out of band, and resolve bearer credentials from a
protected broker. `TestOnlyAllowHTTP` exists only for in-process tests and must
not appear in operator configuration. The data directory, signing key,
process identity, and backups belong to a trust domain separate from the worker
and authority database. A deployment still needs independent evidence for
retention/WORM behavior, administrator separation, anti-rollback restore,
filesystem and storage TOCTOU resistance, availability, and incident recovery.
The append-only application code cannot prevent a storage administrator from
deleting or replacing the whole directory; these are external qualification
gates, not properties inferred from a successful response.

Genesis is canonical JSON with `version`, `streamId`, `sequence: 0`, and `tip`.
Provisioning and signing of that genesis is deliberately out of band.
