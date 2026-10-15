# Agent Task Packet

## Metadata

| Field | Value |
|---|---|
| Project Item | |
| Issue | https://github.com/acme/demo/issues/1 |
| Pull Request | |
| Target Repo | acme/demo |
| Branch | |
| Status | |
| Phase | |
| Priority | |
| Attempt | 0 |
| Primary Agent | |
| QA Agent | |

## Problem

Fix the flaky install path on Linux.

## Scope

In scope:
- PATH shim

Out of scope:
- Windows

## Acceptance Criteria

- [ ] Linux install places binary on PATH
- [ ] doctor reports ok

## Context

Relevant files:
- scripts/release

## Pre-Hook Results

- `pre_project_sync`:

## Implementation Notes

## Verification Plan

Commands or evidence required:
- go test ./...

## QA Verdict

Verdict: `PENDING`

Evidence:
-

Failures:
-

## GitHub Update Log

-

## Final Summary
