# Claude Code Hooks Guide for Maestro Containers

This guide explains how to set up custom Claude Code hooks inside a Maestro container. Use hooks to react to events like context compaction, tool invocations, or session lifecycle changes.

## Background

Maestro pre-configures system hooks in `/home/node/.claude/settings.json` to manage the agent lifecycle (stop blocking, idle detection, message delivery, etc.). These are handled by `maestro-agent` and should not be removed.

You can add **your own** hooks alongside the system hooks using the **project-level** settings file at `.claude/settings.json` inside your workspace. Project-level hooks are merged with the system hooks — they do not replace them.

## Hook Events

Claude Code supports these hook events:

| Event | When it fires | Stdin JSON fields |
|---|---|---|
| `PreToolUse` | Before a tool is invoked | `tool_name`, `tool_input` |
| `PostToolUse` | After a tool succeeds | `tool_name`, `tool_input`, `tool_result` |
| `PostToolUseFailure` | After a tool fails | `tool_name`, `tool_input`, `tool_result` |
| `Stop` | When Claude finishes working | _(none)_ |
| `UserPromptSubmit` | When a user message is submitted | `prompt` |
| `SessionStart` | When a session begins or resumes | `session_id`, `transcript_path`, `source`, `model` |

The `SessionStart` `source` field indicates why the session started: `startup`, `resume`, `clear`, or `compact` (after context window compaction).

## Settings File Locations

Claude Code loads hooks from multiple locations (all are merged):

| File | Scope |
|---|---|
| `/home/node/.claude/settings.json` | User-level (Maestro system hooks live here — do not edit) |
| `/workspace/.claude/settings.json` | Project-level (add your hooks here) |
| `/workspace/.claude/settings.local.json` | Local project-level (gitignored, also works) |

## Hook Format

Each hook entry has:
- **`type`**: Always `"command"`
- **`command`**: Shell command to run
- **`timeout`** (optional): Seconds before the hook is killed (default: 600)
- **`matcher`** (optional): Regex to filter by tool name (for `PreToolUse`/`PostToolUse` events)

A hook command receives event data as JSON on **stdin**. It communicates back to Claude via:
- **stdout** → injected into Claude's context (Claude sees it)
- **stderr** → injected as a user-turn message (when the hook exits with code 2)
- **exit code 0** → hook succeeded, Claude proceeds normally
- **exit code 2** → hook wants to override behavior (for example, `Stop` hook exit 2 = keep Claude running)

## Examples

### React to context compaction

Create a hook that runs a script whenever Claude's context window is compacted. This is useful for re-injecting important state that may have been lost.

**`/workspace/.claude/settings.json`:**
```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/workspace/hooks/on-compaction.sh"
          }
        ]
      }
    ]
  }
}
```

**`/workspace/hooks/on-compaction.sh`:**
```bash
#!/bin/bash
# Read the event JSON from stdin
INPUT=$(cat)
SOURCE=$(echo "$INPUT" | jq -r '.source // empty')

# Only act on compaction events
if [ "$SOURCE" != "compact" ]; then
  exit 0
fi

# Output goes to stdout → Claude sees it as context
echo "=== CONTEXT REFRESH ==="
echo "Important state that should survive compaction:"
cat /workspace/memory/current-state.md 2>/dev/null
echo "=== END REFRESH ==="
```

Make the script executable: `chmod +x /workspace/hooks/on-compaction.sh`

### Log all tool invocations

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "jq -r '{tool: .tool_name, time: now | todate}' >> /tmp/tool-log.jsonl"
          }
        ]
      }
    ]
  }
}
```

### Gate specific tools

Use `matcher` to target specific tools. The matcher is a regex tested against the tool name.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/workspace/hooks/check-bash.sh"
          }
        ]
      }
    ]
  }
}
```

## Important Notes

- **Do not modify** `/home/node/.claude/settings.json` — it contains Maestro system hooks with carefully tuned timeouts. Removing them will break message delivery, idle detection, and the stop blocking mechanism.
- **Project-level hooks merge** with user-level hooks. Your hooks run in addition to Maestro's, not instead of.
- **Long-running hooks** need an explicit `timeout` value. The default is 600 seconds (10 minutes). If your hook needs to block longer (like Maestro's Stop hook at 86400s), set the timeout accordingly.
- **Hook scripts receive JSON on stdin.** Use `jq` or similar to parse. If your script doesn't need stdin, it will simply be ignored.
- **stdout output is visible to Claude.** Only print to stdout if you want Claude to see and act on the content.
