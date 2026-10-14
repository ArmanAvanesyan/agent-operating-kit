# swarmd Integration

`swarmd` remains a separate MCP runtime and state backend. AOK integrates with
it by generating config and mapping neutral workflow objects to MCP calls.

## Compatibility

| Field | Value |
|---|---|
| swarmd version | **Untracked** |
| Integration surface | MCP server command `swarmd mcp` (examples below) |
| Ownership | AOK generates config and call shapes; swarmd owns runtime state |

AOK does not pin or vendor a swarmd release. Example configs below match the
documented MCP mapping; they are not a compatibility guarantee for any specific
swarmd build.

### Recording a tested swarmd version

After verifying against a real `swarmd` binary or release:

1. Note **version or commit**, **date (UTC)**, and which MCP methods were
   exercised (`task.create`, `memory.put`, `event.append`, …).
2. Replace the “Untracked” cell above (or add a “Last tested” bullet) with that
   version.
3. Optionally mirror the note in [host-compatibility.md](host-compatibility.md).

## Boundary

- AOK owns workflow definitions, schemas, packs, adapters, and generated config.
- `swarmd` owns task queue state, event log state, semantic memory, routing, and
  MCP serving.
- AOK does not vendor or absorb `swarmd` code.

## Mapping

| AOK concept | swarmd MCP call | Notes |
|---|---|---|
| Task packet | `task.create` | `title` from packet title, `spec` from Markdown body |
| Role | `assignee` | Use role callsign or adapter-specific assignee |
| Decision | `memory.put` | Store under `namespace: "decision"` |
| Lifecycle update | `event.append` | Use stable event types such as `aok.task.started` |

## Example MCP Config

```json
{
  "mcpServers": {
    "swarmd": {
      "command": "swarmd",
      "args": ["mcp"],
      "env": {
        "SWARMD_DB": ".swarmd/state.db"
      }
    }
  }
}
```

## Example `task.create`

```json
{
  "title": "Add Stripe webhook verification",
  "spec": "## Problem\nWebhook requests need signature verification.\n\n## Acceptance Criteria\n- [ ] Invalid signatures are rejected.",
  "assignee": "implementer",
  "gate_cmd": "make quality"
}
```

## Example `memory.put`

```json
{
  "namespace": "decision",
  "key": "stripe-webhook-signature",
  "content": "Use provider SDK verification at the HTTP boundary before parsing event payloads."
}
```

## Example `event.append`

```json
{
  "type": "aok.task.qa_passed",
  "actor": "qa-reality-checker",
  "payload": {
    "taskPacket": "agents/tasks/stripe-webhook.md",
    "evidence": "make quality passed"
  }
}
```
