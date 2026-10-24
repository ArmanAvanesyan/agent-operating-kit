# Windows Preservation Probe Protocol

The native probe script is intended for **Windows 11 amd64 on a fixed local
NTFS volume**. From the repository root, run it in PowerShell:

```powershell
powershell -NoProfile -File scripts/testing/windows-preservation-probe.ps1 `
  -Output windows-preservation-report.json
aok delivery windows-preservation-evaluate `
  --report windows-preservation-report.json --json
```

The script exercises native ACL, junction/reparse-point, alternate data stream,
case-insensitive lookup, long-path, ZIP traversal, and file-replacement-lock
scenarios. It reports delivery preservation of dirty/untracked files,
interruption recovery, and restore recovery as `pending`: those observations
require the actual AOK delivery workflow, and the probe does not simulate them
with copies or unchanged-file hash checks.

`fixture-passed` means only that each filesystem probe passed in the stated
scope. It is not a repository-delivery certification. The report is a local
self-report; before any qualified claim, a protected external witness must
bind a native run identity to the AOK delivery workflow evidence and verify its
signature. A report produced on another operating system, architecture, or
filesystem is out of scope. No native Windows run is claimed by this checkout.
