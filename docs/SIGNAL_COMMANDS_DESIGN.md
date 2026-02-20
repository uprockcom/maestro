# Signal Commands & Projects: Design Notes

## Overview

Enable mobile interaction with running containers via Signal, built on top of
a new **project** abstraction that solves the "which directory?" problem and
enables **manager containers** as persistent dispatchers.

### Concept Stack

```
Projects          — named sets of directories, copied into containers together
  Nicknames       — persistent aliases for long-running containers
    Manager       — a nicknamed container that dispatches work for a project
      Signal      — the mobile interface to all of the above
```

Each layer builds on the one below it.

---

## Projects

### Problem

Today, `maestro new` copies whatever directory you're standing in. Run it from
the wrong folder and you get the wrong code in the container. There's also a
global `sync.additional_folders` that applies the same sibling directories to
every container regardless of what you're working on.

### Solution

A **project** is a named set of directories that get copied into a container
together. Projects are **additive** — ad-hoc `maestro new` in any directory
always works, exactly as today. Defining a project doesn't prevent ad-hoc
usage; it just gives you a named shortcut and enables @-routing via Signal.

A single-path project and an ad-hoc run from that same directory produce
identical containers. The project just means you can also do `maestro new -p
maestro "fix bug"` from anywhere, or `@maestro fix bug` from Signal, instead
of having to `cd` first.

Two modes:

- **Single-path** (`path`): one repo → copies straight to `/workspace/`
  (identical to today's behavior and identical to ad-hoc)
- **Multi-path** (`paths`): multiple repos → each becomes a subdirectory of
  `/workspace/` (all peers, no primary/secondary distinction)

**Config:**

```yaml
projects:
  # Single-path: one repo, goes to /workspace/ (current behavior)
  maestro:
    path: ~/Documents/Code/multiclaude

  # Multi-path: all repos are peers under /workspace/
  insight:
    paths:
      - ~/Documents/Code/insight-app
      - ~/Documents/Code/insight-cli
      - ~/Documents/Code/shared-utils

  # Multi-path with explicit primary (for branch naming, Claude start dir)
  uprock:
    paths:
      - ~/Documents/Code/uprock-monorepo
      - ~/Documents/Code/mcp-servers
    primary: ~/Documents/Code/uprock-monorepo
```

**`path` vs `paths` are mutually exclusive.** Having both is a config error.

### Why Two Modes

A single-element `paths` list would produce `/workspace/multiclaude/` instead
of `/workspace/`, breaking backwards compatibility. The `path` singular form
is a semantic signal: "this IS the workspace, not a parent of subdirectories."
This also means adding a second path to a project is a conscious layout change,
not an accidental side effect.

### Container Layout

**Single-path:**
```
/workspace/          ← the repo itself (unchanged from today)
```

**Multi-path:**
```
/workspace/
  insight-app/       ← first repo
  insight-cli/       ← second repo
  shared-utils/      ← third repo
```

The old `sync.additional_folders` copied to `/workspace/../<name>/` (siblings).
Multi-path puts everything *under* `/workspace/` instead — shorter paths,
more natural for Claude to navigate, and all repos are visible from one `ls`.

The global `sync.additional_folders` continues to work for all containers
(ad-hoc and project-based). For ad-hoc containers, it works exactly as today.
For project containers, the project's paths take priority — if a project
defines its own set of directories, `sync.additional_folders` is not also
applied (the project definition IS the complete set).

### Ad-hoc vs Project Containers

| Scenario | What happens |
|---|---|
| `cd ~/Code/foo && maestro new "task"` (no project match) | Ad-hoc: copies cwd to `/workspace/`, uses `sync.additional_folders` — identical to today |
| `cd ~/Code/foo && maestro new "task"` (foo is in a project) | Project detected: uses project config instead of cwd + global folders |
| `cd ~/Code/foo && maestro new --no-project "task"` | Force ad-hoc: ignores project detection, copies cwd — escape hatch |
| `maestro new -p insight "task"` (from anywhere) | Explicit project: uses insight config regardless of cwd |

The `--no-project` flag is the escape hatch for when you're inside a project
directory but want ad-hoc behavior. This should be rare — if you find yourself
using it often, your project config probably needs adjusting.

### Project Detection from cwd

Detection checks **all** paths in a project — `path` or every entry in
`paths`. If cwd is anywhere inside any of them, the whole project is selected.

Example with the `insight` project:
- `cd ~/Code/insight-app/src && maestro new "fix bug"` → insight
- `cd ~/Code/insight-cli && maestro new "add retry"` → insight
- `cd ~/Code/shared-utils/lib && maestro new "update types"` → insight

**Algorithm:**
1. Expand and normalize cwd to absolute path
2. For each project, expand all paths (singular `path` or all `paths` entries)
3. Check if cwd starts with any of them (prefix match)
4. Multiple projects match → longest prefix wins
5. No match → no project, use cwd directly (existing behavior)

The project defines the canonical layout; cwd just determines which project
you meant.

### Git Branch Creation

**Single-path:** unchanged — `git checkout -b` in `/workspace/`.

**Multi-path:** create the branch in ALL repos that have a `.git` directory.
Since the container is isolated, this is safe and ensures all repos are on a
consistent branch. Repos without `.git` are skipped silently. Also needs
`git config --global --add safe.directory` for each subdirectory.

### Claude's Working Directory

**Single-path:** Claude starts in `/workspace/` (unchanged).

**Multi-path:** Claude starts in `/workspace/` (the parent). All repos are
visible as subdirectories. The MAESTRO.md template should document the layout:

```
- **Workspace**: `/workspace/` containing:
  - `insight-app/` — primary repo
  - `insight-cli/`
  - `shared-utils/`
```

### Container Naming

When a project is specified, prefix with project name:
- `maestro-insight-feat-add-auth-1` (project container)
- `maestro-feat-add-auth-1` (no project, backwards compatible)

Docker label `maestro.project=insight` should also be set for programmatic
queries and grouping.

### TUI Integration

When starting a new container from the TUI, show a project picker:

```
Start new container

  Project:
  > maestro         ~/Documents/Code/multiclaude
    insight         ~/Documents/Code/insight-app + 2 repos
    uprock          ~/Documents/Code/uprock-monorepo + 1 repo
    Custom folder...

  Task: _
```

- Arrow keys to select project, then type task description
- "Custom folder..." opens a text input for a path (free entry)
- If only one project is configured, pre-select it but still allow override
- No projects configured → skip picker, go straight to task input

---

## Nicknames

### Problem

Container names like `maestro-feat-add-auth-1` are auto-generated and
disposable. For long-running containers (especially managers), you need a
stable handle. Typing `@maestro-feat-add-auth-1` on a phone is a non-starter.

### Solution

A **nickname** is a short alias for a container that persists across the
container's lifetime. Set at creation time or after the fact.

```bash
maestro new "manage the insight project" --nick manager
maestro nick maestro-feat-auth-1 auth
```

**Storage:** Nickname → container name mapping lives in daemon state (or a
file in `~/.maestro/nicknames.yml`). Simple bidirectional lookup.

**Usage:**
- `maestro connect manager` — resolves nickname, connects to container
- `@manager deploy the latest changes` — Signal routes to the nicknamed container
- `list` output uses nicknames when available:

```
manager (insight): IDLE
  Waiting for instructions

auth: WORKING
  Adding JWT middleware to API routes

maestro-fix-tests-2: IDLE
  Debugging flaky integration test
```

**Matching priority for `@name`:**
1. Exact nickname match
2. Exact short-name match (container name minus prefix)
3. Substring/fuzzy match (only if unambiguous)
4. Ambiguous → reply asking which one

**Persistence across restarts:** Nicknames survive container stop/restart.
If a container is replaced (stopped + new container for same purpose), the
user can reassign the nickname: `maestro nick new-container-name manager`.

---

## Manager Containers

### Concept

A **manager** is just a regular container with:
1. A **nickname** so you can always reach it
2. A **long-running prompt** that tells Claude it's a project dispatcher
3. Access to **create child containers** (already supported via IPC `new` action)

The key insight: this doesn't need to be a special Maestro concept. It's a
container with the right prompt and a nickname. Maestro provides the
infrastructure (projects, nicknames, message injection), and the manager
behavior comes from Claude's instructions.

### Setup

```bash
maestro new -p insight --nick manager "You are the project manager for the
Insight application. When I send you a task or ticket, create a new child
container to work on it. Monitor your children and report back on progress.
When a child finishes, review its work and let me know."
```

Or from Signal:
```
new insight: You're the manager for Insight. I'll send you tickets.
```

The manager container then:
- Receives tasks via `@manager fix the login bug` (message injection)
- Creates child containers via IPC `new` action (already works)
- Monitors children via IPC `read_messages` / `wait_idle` (already works)
- Reports back via IPC `notify` or `container_notification` event

### Manager → User Communication

The manager needs a way to proactively notify the user.

**Recommended:** The `container_notification` event type already exists in the
notify system. The manager writes to a known file path, the daemon's check
loop picks it up and dispatches via the notification engine. Same pattern as
the idle flag and question detection. The existing IPC `notify` action also
works for explicit notifications.

---

## Signal Commands

### Routing

Currently, `pollOnce()` only matches replies to pending questions. Unmatched
messages from the recipient are silently discarded. Change:

1. After failing to match a pending question, check for command patterns
2. Route to command handler:
   - `list` / `ls` / `status` → list handler
   - `new [project:] <task>` → new container handler
   - `@<nick> <message>` → message injection to single container
   - `@all <message>` → broadcast to all active containers
   - `@<project> <message>` → broadcast to all containers in a project
   - Unrecognized → reply with short help text

**Implementation:** Command callback passed into Signal provider (same pattern
as the existing response callback). Signal parses, daemon executes.

### `list`

1. Get running containers where Claude is active
2. For each: read last ~10 messages via `readClaudeMessages()`
3. Pass to Haiku with: "Summarize what this Claude is doing in one sentence,
   max 80 chars."
4. Format with nickname (if any), project, branch, status, summary
5. Send as single Signal message

```
manager (insight): IDLE
  Waiting for instructions

auth (insight) feat/add-auth: WORKING
  Adding JWT middleware to API routes
```

**Zero-cost alternative:** Use `GetTaskSummary()` current task name + progress.
Less descriptive but instant. Could be the default, with `detail` or `status`
triggering the LLM version.

### `new [project:] <task>`

1. Parse optional project prefix (before colon)
2. Resolve project config (paths + layout)
3. No project specified → use `daemon.default_project` or reply asking
4. Create container via existing flow
5. Reply with container name when ready

### `@<nick> <message>`

1. Resolve nick to container (see matching priority above)
2. Write message to pending message queue in container
3. If idle → trigger via tmux send-keys
4. If busy → Stop hook will pick it up
5. Reply with confirmation: "Sent to auth"

### `@all <message>` — Broadcast

1. Get all running containers with active Claude
2. For each: write message to pending queue, trigger if idle
3. Reply: "Sent to 4 containers: manager, auth, api, tests"

Use cases: "stop what you're doing", "pull latest from main", "run tests
before you commit".

### `@<project> <message>` — Project Broadcast

Same as `@all` but filtered to containers tagged with the project name.
Resolution: if `@name` matches a project name (and not a nickname), treat
as project broadcast. Nicknames take priority over project names.

---

## Message Injection Mechanism

### Why File-Based

Raw `tmux send-keys` is unreliable for real content:
- Special characters, quotes, newlines need escaping
- Long messages (multi-paragraph instructions) break
- Race conditions if Claude is mid-output
- Paste buffer limits

File-based injection via hooks is the primary input path for all programmatic
input — Signal, TUI, manager containers, scripts. When a user is connected
and typing normally, the hooks are a no-op (no pending file → exit 0).

### Two-Hook Pattern

**`UserPromptSubmit` hook** — for when Claude is already idle:

```bash
#!/bin/bash
QUEUE_DIR="/home/node/.maestro/pending-messages"
if [ -d "$QUEUE_DIR" ] && ls "$QUEUE_DIR"/*.txt 1>/dev/null 2>&1; then
  for f in $(ls "$QUEUE_DIR"/*.txt | sort); do
    cat "$f" >&2
    rm -f "$f"
  done
  exit 2   # replace trigger text with queued message(s)
fi
exit 0
```

Flow: daemon writes file to queue → `tmux send-keys "" Enter` →
UserPromptSubmit fires → hook reads all queued files in order → Claude gets
the real message(s). The trigger text doesn't matter because it gets replaced.

**`Stop` hook extension** — for when Claude is busy:

```bash
#!/bin/bash
QUEUE_DIR="/home/node/.maestro/pending-messages"
if [ -d "$QUEUE_DIR" ] && ls "$QUEUE_DIR"/*.txt 1>/dev/null 2>&1; then
  for f in $(ls "$QUEUE_DIR"/*.txt | sort); do
    cat "$f" >&2
    rm -f "$f"
  done
  exit 2   # inject as next prompt, don't go idle
fi
touch /home/node/.maestro/claude-idle
```

Flow: Claude finishes turn → Stop hook fires → queued files exist → Claude
immediately processes them without ever going idle.

### Timing Summary

| Claude State | Action | Mechanism |
|---|---|---|
| Idle (waiting for input) | Write to queue + tmux send-keys Enter | UserPromptSubmit replaces trigger |
| Busy (working) | Write to queue, wait | Stop hook picks it up at next pause |
| Asking question | Use existing question-response flow | Or queue for after |

### Message Queueing

Use numbered files from the start (not "last message wins"):
```
/home/node/.maestro/pending-messages/
  001-1739641200.txt
  002-1739641205.txt
```

Files named with sequence number + timestamp. Hook reads all in sort order,
concatenates, then deletes. This correctly handles broadcasts (multiple
messages arriving before Claude stops) and rapid Signal commands.

---

## Additional Features

### Broadcast Messages

`@all <message>` sends to every active container. `@<project> <message>`
sends to all containers in a project. The daemon iterates matching containers
and writes to each one's message queue.

**Selective broadcasts (future):** `@idle <message>` (only idle containers),
`@working <message>` (only busy containers). Lower priority — `@all` and
`@project` cover most use cases.

### Container Dormant Detection

Today, the daemon detects idle/running/question states but not Claude process
death. If Claude crashes and the tmux session is empty, the container just
sits silently.

**Change:** When `isClaudeRunning()` transitions from true → false for a
running container, emit an `EventDormant` notification: "Claude exited in
container auth". This surfaces silent failures.

### `maestro restart`

If a container is stopped but not removed, `maestro restart <name-or-nick>`
should:
1. `docker start` the container
2. Re-launch Claude in tmux with a resume prompt
3. Re-register with daemon for monitoring

The container's filesystem is the context snapshot — git log, diffs, task
files, MAESTRO.md are all intact. Claude reconstructs context from these
artifacts. This is the simplest and most reliable resurrection path.

Full context snapshots (serializing Claude's conversation history for
injection on restart) can be a later optimization.

### EventBlocker Notification Type

A new event type for urgent container problems. When a container hits a wall
(firewall blocks a domain, dependency missing, tests fail and need human
judgment), it can emit a blocker notification that bypasses quiet hours and
uses urgent formatting.

`maestro-request notify --blocker "Cannot reach api.stripe.com"`

### Git Coordination (Future)

Multiple containers on the same project create branches from the same base.
When it's time to merge, conflicts are likely. Possible future additions:
- `maestro merge-check <container>` — dry-run merge against main
- `maestro merge-all <project>` — merge branches in optimal order
- Branch name visible in `list` output (trivial, do alongside list)

For now, the manager pattern handles coordination — the manager reviews
children's work and can detect conflicts by reading their diffs.

---

## Implementation Order

This is a single cohesive design delivered across three commits. Each commit
produces a working system — the commits are ordered by dependency, not by
importance. Everything below is part of the same feature set.

### Commit 1: Foundation (projects + nicknames + hooks)

The infrastructure layer that everything else builds on. After this commit,
projects work from CLI/TUI, nicknames resolve in `connect`, and the message
injection mechanism is testable.

1. **Projects config** — add `projects` map to config struct with `path` /
   `paths` / `primary` fields. Validation for mutual exclusion. Project
   resolution function (cwd detection + `-p` flag + `--no-project` escape
   hatch).

2. **Single-path project mode** — when `Project.Path` is set, use that path
   instead of cwd. Simplest project feature, validates wiring end-to-end.

3. **Multi-path project mode** — new copy function for each path →
   `/workspace/<basename>/`. Update `initializeGitBranch` (all repos),
   `writeMaestroMD` (layout documentation), `setupGitHubRemote` (iterate
   repos). Add Docker label `maestro.project=<name>`.

4. **Nicknames** — storage in `~/.maestro/nicknames.yml`, `maestro nick`
   command, `--nick` flag on `maestro new`, resolution in `maestro connect`.
   Display in TUI list.

5. **Message queue + hook extensions** — create `pending-messages/` queue
   directory (numbered files), extend Stop hook, add UserPromptSubmit hook.
   Test manually via `docker exec`.

6. **TUI project picker** — project selection in container creation form.

7. **Dormant detection** — `isClaudeRunning()` state transition emits
   `EventDormant` notification.

### Commit 2: Signal interaction

Wires Signal into the foundation from commit 1. After this commit, you can
list containers, send messages, broadcast, and create containers from phone.

8. **Signal command routing** — parse inbound messages as commands when they
   don't match pending questions. Command callback from Signal → daemon.

9. **`list` via Signal** — read state, LLM-summarize, format, reply.

10. **`@nick message` via Signal** — resolve nickname, write to queue,
    trigger injection, confirm.

11. **`@all` / `@project` broadcast** — fan-out to matching containers.

12. **`new` from Signal** — create container using project config.

### Commit 3: Manager + restart + polish

Brings the manager pattern to life and adds resilience. After this commit,
you can set up a manager, text it tasks, and restart containers that die.

13. **`maestro restart`** — restart stopped containers with resume prompt.

14. **EventBlocker** — urgent notification type, quiet-hours bypass.

15. **Manager prompt template** — ship a good default manager prompt that
    instructs Claude on child creation, monitoring, and reporting.

16. **Per-provider event filtering tests** — verify the filtering we just
    built works end-to-end with Signal commands.

### Future (separate work)

- `@idle` / `@working` selective broadcasts
- Container-to-sibling IPC (`list_siblings`, `send_sibling`)
- Full context snapshots for resurrection
- Git merge coordination (`merge-check`, `merge-all`)
- Container labels/tags beyond projects
- Resource/cost monitoring per container

---

## Open Questions

- **Manager prompt template:** Should Maestro ship a default manager prompt
  that users can customize? Or always freeform? A template would help with
  best practices (when to create children, how to report, etc.)

- **Multi-project managers:** Can one manager span multiple projects? Probably
  not — a manager is tied to a project because it needs the code. Cross-project
  coordination would be a "super-manager" that talks to project managers.

- **Message confirmation:** When `@nick message` is sent, reply immediately
  with "Sent to auth"? Or wait for Claude to process? Immediate is simpler
  and avoids long waits. Claude's response comes via normal notification flow.

- **Broadcast rate limiting:** Minimum interval between broadcasts (30s?) to
  prevent accidental spam from phone.

- **Project name vs nickname collision:** If a project is named "auth" and a
  container is nicknamed "auth", `@auth` should resolve to the nickname (single
  container), not the project (broadcast). Document this priority clearly.
