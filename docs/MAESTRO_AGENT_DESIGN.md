# maestro-agent: Container-Side Agent Lifecycle Manager

## Overview

maestro-agent is a Go binary that runs inside every Maestro container. It replaces the original bash hook scripts with a structured lifecycle manager that handles Claude's start/stop cycle, message delivery, user connection detection, heartbeat scheduling, and automatic restarts.

The binary serves two roles:
1. **Hook handler** — Claude Code calls `maestro-agent hook <type>` for each hook event
2. **Background service** — `maestro-agent service` runs persistently, managing idle wake-up, heartbeat, and clear timer

## State Machine

```
                    ┌──────────────────────────────────────────┐
                    │                                          │
                    ▼                                          │
              ┌──────────┐    SessionStart     ┌────────┐     │
              │ starting  │ ──────────────────→ │ active │     │
              └──────────┘                      └────────┘     │
                    ▲                               │          │
                    │                     Claude finishes work  │
                    │                               │          │
                    │                               ▼          │
                    │                          ┌─────────┐     │
              kill-restart                     │ waiting  │     │
              (clear timer)                    └─────────┘     │
                    │                            │     │       │
                    │               ┌────────────┘     └───────┤
                    │               │                          │
                    │    user connects              message/trigger
                    │               │                          │
                    │               ▼                          │
                    │          ┌──────┐                        │
                    └───────── │ idle  │                        │
                               └──────┘
                                  │
                          clear_after expires
                                  │
                                  ▼
                            ┌──────────┐
                            │ clearing  │ ──→ (kill-restart) ──→ starting
                            └──────────┘
```

State is written atomically to `/home/node/.maestro/state/agent-state`. A backward-compatible `claude-idle` flag file is maintained for older code that checks idle status. The `question` state was added for the Ask hook — it means Claude is blocked waiting for an answer to AskUserQuestion. Both `idle` and `question` states touch the `claude-idle` flag (both represent "Claude is blocked").

## Hook Lifecycle

Claude Code hooks call `maestro-agent hook <type>`. Each hook command sets `suppressStderr = true` to prevent log output from contaminating hook communication channels (Stop hook uses stderr for message delivery).

### Stop Hook (`maestro-agent hook stop`)

The most complex hook. When Claude finishes working:

1. **Re-entrancy guard** — checks if the calling process is the main Claude (not a child like haiku). Child processes exit silently.
2. **Immediate queue check** — if messages are already waiting in `pending-messages/`, delivers them instantly via stderr + exit 2 (Claude continues processing).
3. **Blocking wait** — if no messages, enters blocking wait with concurrent watchers. State is set to `waiting`.
4. **First watcher wins** — watchers race on a channel. The first to fire determines the outcome:
   - **Queue watcher** (polls every 1s): drains messages, delivers via stderr, exit 2
   - **Connected watcher** (polls every 2s): detects tmux client, exit 0 (allow idle)
   - **Script watcher** (custom): runs a bash script that blocks until its condition fires, delivers output as a trigger, exit 2
5. **Cleanup** — remaining watchers are cancelled via context. 3-second cleanup timeout for goroutines.

```
Claude finishes work
  → Stop hook fires (maestro-agent hook stop)
  → Checks queue immediately (fast path)
  → If messages: deliver via stderr + exit 2 → Claude processes → loop
  → If no messages: enter blocking wait
    → Spawn goroutines:
      1. watchQueue: polls pending-messages/ every 1s
      2. watchConnected: polls tmux list-clients every 2s
      3. watchScript: runs custom bash script (blocks until condition)
    → First to fire wins (channel race)
    → If queue/script: deliver content via stderr + exit 2
    → If connected: exit 0 (interactive mode)
```

**Critical: Hook timeout.** Claude Code hooks have a default timeout of 600 seconds (10 minutes). The Stop hook must have `"timeout": 86400` (24 hours) in settings.json, or Claude will kill the blocking wait after 10 minutes.

### Prompt Hook (`maestro-agent hook prompt`)

Fires when Claude receives user input (including "continue" from idle wake-up):

1. Re-entrancy guard
2. Clears idle flag (Claude is now active)
3. Drains queue and prints messages to stdout (Claude sees them as part of the prompt context)

This is the primary delivery mechanism for messages that arrive while Claude is idle — the service sends "continue" to tmux, which triggers the prompt hook, which delivers queued messages.

### SessionStart Hook (`maestro-agent hook session-start`)

Fires when Claude's session begins (startup, resume, or after compaction):

1. Reads JSON from stdin (SessionID, Source, etc.)
2. Touches `session-ready` flag (signals to service that Claude is operational)
3. Sets state to `active`
4. If source is "compact" and manifest has `compact_context: true`, injects lightweight context refresh via stdout

### PreToolUse Hook (`maestro-agent hook pre-tool-use [--idle]`)

State management hook with two modes:

1. **Default (no flag)** — `WriteState(StateActive)`. Claude is working, clears idle flag.
2. **`--idle` flag** — `WriteState(StateIdle)`. Used for user-blocking tools (AskUserQuestion, EnterPlanMode, ExitPlanMode) where Claude is waiting for user input.

### Ask Hook (`maestro-agent hook ask`)

Handles AskUserQuestion's PreToolUse hook — blocking wait for response or user connection. Follows the same watcher pattern as the Stop hook:

1. **Re-entrancy guard** — child processes exit silently.
2. **Parse stdin JSON** — extracts `tool_input` from the hook's JSON payload and writes it to `current-question.json`. This gives external tools (daemon, TUI) the structured question data.
3. **User already connected?** — if `isUserConnected()` returns true, exit 0 immediately (let Claude show its interactive UI).
4. **Clear stale response** — removes any leftover `question-response.txt` from a previous question.
5. **Set state** — `WriteState(StateQuestion)` (touches `claude-idle` for backward compat).
6. **Blocking wait** — spawns two watcher goroutines racing on a channel:
   - **watchResponseFile** (polls every 1s): detects `question-response.txt` written by daemon
   - **watchUserConnected** (polls every 2s): detects tmux client attachment
7. **First watcher wins**:
   - Response found → read content, output to stderr, cleanup files, **exit 2** (Claude receives answer)
   - User connects → **exit 0** (Claude shows interactive question UI)
   - 6-hour timeout → exit 0 (safety valve)

**Critical: Hook timeout.** Like the Stop hook, the Ask hook needs `"timeout": 86400` in settings.json.

**Important sequencing detail:** The hook removes the stale response file (step 4) BEFORE starting watchers (step 6). This means the daemon must write the response file AFTER the hook enters blocking wait (i.e., after seeing "question" state), not before.

### PostToolUse Hook (`maestro-agent hook post-tool-use`)

State transition hook for PostToolUse and PostToolUseFailure events:

1. **Re-entrancy guard** — child processes exit silently.
2. **Parse stdin JSON** — reads `tool_name` from the hook payload.
3. **`WriteState(StateActive)`** — Claude got its answer, it's working again. Removes idle flag.
4. **Cleanup** — if the tool was `AskUserQuestion`, removes `current-question.json`.

## Background Service (`maestro-agent service`)

A persistent process that runs alongside Claude. Started during container creation and runs for the container's lifetime. Polls every 2 seconds.

### Responsibilities

1. **Idle wake-up** — when state is `idle` and messages exist in the queue, sends "continue" + Enter to Claude's tmux pane via `tmux load-buffer` + `tmux paste-buffer` + `tmux send-keys C-m`.

2. **Clear timer** — when state transitions to `idle`, starts a one-shot timer (`clear_after` seconds). If the timer fires and Claude is still idle with no user connected, transitions to `clearing` and performs a kill-restart:
   - Runs pre-clear script (e.g., `save-state.sh`) to checkpoint state
   - Kills Claude process (via PID file or `pgrep -f "^claude"`)
   - Reassembles bootstrap prompt from manifest
   - Respawns Claude in tmux (`tmux respawn-pane -k` or `tmux new-session` if server died)
   - Waits up to 60s for `session-ready` flag from SessionStart hook

3. **Heartbeat** — periodic message generation at configurable intervals. Runs a heartbeat script (or uses generic fallback) and writes the result to the message queue. Heartbeats are delivered via the normal queue watcher in the Stop hook. Can be suppressed while Claude is active.

### Clear Timer Behavior

- Starts once per idle transition (not every poll cycle)
- Cancelled when state leaves `idle` (active, waiting)
- Checks user connection before killing (aborts if user attached)
- Kills Claude via PID file first, falls back to `pgrep`
- Falls back to creating new tmux session if `respawn-pane` fails (tmux server may have died)

## Agent Manifest (`agent.yml`)

Each container can have an agent manifest at `/home/node/.maestro/agent.yml` that configures its behavior. Without a manifest, the system falls back to minimal behavior (simple queue drain in Stop hook, no heartbeat, no clear timer).

```yaml
# Agent type — informational, used by TUI/daemon
type: manager            # manager | worker | interactive

bootstrap:
  strategy: pipe          # pipe (stdin) | cli_arg | skill
  skill: manager          # Skill name to invoke (prepended as /skill\n)
  context:
    files:                # Files to include in bootstrap context
      - memory/state.md
      - "memory/log.md:tail:50"    # :tail:N suffix = last N lines
    assembly: hooks/build-context.sh  # OR: script that assembles context

on_stop:
  watchers:               # Concurrent watchers for blocking Stop hook
    - queue               # Built-in: polls pending-messages/ every 1s
    - connected           # Built-in: polls tmux list-clients every 2s
    - script: hooks/watch-repo.sh  # Custom: blocks until condition fires

on_idle:
  clear_after: 10800      # Seconds before kill-restart (0 = disabled)
  pre_clear: hooks/save-state.sh   # Script to run before killing Claude

on_message:
  delivery: batch         # batch (deliver all at once) | immediate

heartbeat:
  interval: 900           # Seconds between heartbeats (0 = disabled)
  script: hooks/heartbeat.sh  # Script that generates heartbeat content
  suppress_while_active: true  # Don't heartbeat while Claude is working

on_session_start:
  compact_context: true   # Re-inject context after transcript compaction
```

### Watcher Configuration

Watchers use custom YAML unmarshaling to support two formats:

```yaml
watchers:
  - queue                        # String → built-in watcher
  - connected                    # String → built-in watcher
  - script: hooks/watch-repo.sh  # Map → custom script watcher
```

Custom watcher scripts must:
- Block until their condition fires
- Write a human-readable description to stdout
- Exit 0 when the condition is met
- Accept SIGTERM gracefully (they'll be killed when another watcher wins)

### Context Assembly

Bootstrap context can be assembled two ways:

1. **File list** — reads each file, concatenates with headers. Supports `:tail:N` suffix to include only the last N lines. Relative paths are resolved under `/workspace/` and its subdirectories.

2. **Assembly script** — runs a bash script in `/workspace/` and uses its stdout as the context block. Takes priority over file list if both are specified.

The assembled context is wrapped in `=== MANAGER CONTEXT ===` / `=== END CONTEXT ===` markers and inserted into the bootstrap prompt after the skill invocation line.

## Message Queue

External systems (daemon, other containers, scripts) deliver messages by writing `.txt` files to `/home/node/.maestro/pending-messages/`. Filenames are nanosecond Unix timestamps (e.g., `1708262400000000000.txt`).

`DrainQueue()` reads all `.txt` files, sorts by timestamp, removes the files, and returns the messages. This is an atomic operation — messages are consumed on read.

Messages are formatted with structured headers:

```
=== MESSAGE SOURCE: queue (2 messages) ===
--- Message 1 [2026-02-18T12:10:19Z] ---
<content>
--- Message 2 [2026-02-18T12:10:22Z] ---
<content>
=== END MESSAGES ===
```

Trigger messages (from custom watchers) use a different format:

```
=== TRIGGER: watch-repo.sh ===
<watcher script stdout>
=== END TRIGGER ===
```

## Piped Bootstrap

Container startup uses piped bootstrap instead of the old auto-input script:

```bash
cat /tmp/maestro-bootstrap.txt | claude --dangerously-skip-permissions
```

The bootstrap prompt is assembled by `BuildBootstrapPrompt()`:

1. Skill invocation line (e.g., `/manager\n`) — if configured
2. Context block — assembled from manifest files or assembly script
3. Pending messages — any messages already in the queue at startup

This approach is more reliable than the old tmux send-keys method, which had timing issues with Claude's startup sequence.

## File Layout

```
/usr/local/bin/maestro-agent            # The binary

/home/node/.maestro/
├── agent.yml                           # Agent manifest (per-container config)
├── claude-idle                         # Backward-compat idle flag file
├── state/
│   ├── agent-state                     # Current state (starting|active|waiting|idle|clearing)
│   ├── agent.pid                       # Service process PID
│   ├── claude.pid                      # Main Claude process PID (if written)
│   └── session-ready                   # Touched by SessionStart hook
├── logs/
│   └── maestro-agent.log              # Structured JSON log (append-only)
└── pending-messages/
    ├── 1708262400000000000.txt         # Queued message (timestamp filename)
    └── ...

/home/node/.claude/settings.json        # Claude Code settings with hook config
/tmp/maestro-bootstrap.txt              # Staged bootstrap prompt for piped startup
```

## Settings.json Hook Configuration

The container's `settings.json` wires all hooks to maestro-agent:

```json
{
  "hooks": {
    "Stop": [{
      "hooks": [{"type": "command", "command": "maestro-agent hook stop", "timeout": 86400}]
    }],
    "SessionStart": [{
      "hooks": [{"type": "command", "command": "maestro-agent hook session-start"}]
    }],
    "UserPromptSubmit": [{
      "hooks": [{"type": "command", "command": "maestro-agent hook prompt"}]
    }],
    "PreToolUse": [
      {"hooks": [{"type": "command", "command": "maestro-agent hook pre-tool-use"}]},
      {"matcher": "AskUserQuestion|EnterPlanMode|ExitPlanMode",
       "hooks": [{"type": "command", "command": "maestro-agent hook pre-tool-use --idle"}]},
      {"matcher": "AskUserQuestion",
       "hooks": [{"type": "command", "command": "maestro-agent hook ask", "timeout": 86400}]}
    ],
    "PostToolUse": [
      {"matcher": "AskUserQuestion|EnterPlanMode|ExitPlanMode",
       "hooks": [{"type": "command", "command": "maestro-agent hook post-tool-use"}]}
    ],
    "PostToolUseFailure": [
      {"matcher": "AskUserQuestion",
       "hooks": [{"type": "command", "command": "maestro-agent hook post-tool-use"}]}
    ]
  }
}
```

The 86400-second timeout on Stop and Ask hooks is critical — without it, Claude's default 600-second hook timeout will kill the blocking wait.

## Re-entrancy Guard

Claude can spawn child processes (e.g., haiku for compression) that also trigger hooks. The `isMainClaude()` guard prevents these from executing hook logic:

1. Reads the main Claude PID from `claude.pid`
2. Walks the `/proc/<pid>/stat` parent chain (up to 10 levels)
3. Returns true only if the calling process's ancestry includes the main PID

Child process hook invocations exit silently (exit 0) without affecting state.

## Source Files

| File | Purpose |
|---|---|
| `main.go` | CLI entrypoint, Cobra command tree |
| `paths.go` | Central path variable definitions |
| `state.go` | State machine read/write with atomic file ops |
| `queue.go` | Message queue drain, format, timestamp parsing |
| `manifest.go` | agent.yml loading with custom YAML unmarshaling |
| `log.go` | Structured JSON logging with stderr suppression |
| `context.go` | Bootstrap prompt assembly and context injection |
| `hook_stop.go` | Blocking Stop hook with concurrent watchers + `isMainClaude()`/`isUserConnected()` |
| `hook_prompt.go` | Prompt hook — idle wake-up message delivery |
| `hook_session_start.go` | SessionStart — readiness signal, compaction context |
| `hook_pre_tool_use.go` | PreToolUse — state management with `--idle` flag |
| `hook_ask.go` | Ask hook — blocking wait for AskUserQuestion response or user connection |
| `hook_post_tool_use.go` | PostToolUse — state transition + question file cleanup |
| `service.go` | Background service — idle wake-up, clear timer, heartbeat |
| `status.go` | `maestro-agent status` diagnostic command |

## Unit Tests

Tests across 8 test files, all self-contained using temp directories:

| Test File | Coverage |
|---|---|
| `state_test.go` | Read/write state, idle flag lifecycle, question state, EnsureStateDirs |
| `queue_test.go` | Drain (empty/missing/single/ordered/non-txt), HasQueued, FormatMessages, FormatTrigger |
| `manifest_test.go` | Load/defaults/full config/invalid YAML, HasManifest, WatcherConfig unmarshaling |
| `log_test.go` | File output, stderr suppression, log levels |
| `context_test.go` | Path resolution, file reading, tail suffix, bootstrap assembly |
| `hook_ask_test.go` | Question file extraction (valid/invalid/null JSON), response file detection, cancellation, state transitions |
| `hook_post_tool_use_test.go` | State transition, question file cleanup, non-Ask preservation, empty stdin |
| `hook_pre_tool_use_test.go` | Default→active and `--idle`→idle state transitions |

Tests use a `setupTestDirs()` helper that redirects all path variables to `t.TempDir()`, enabling fully isolated test execution without Docker.
