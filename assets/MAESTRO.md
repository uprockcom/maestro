# Maestro Container Environment

You are running inside a **Maestro-managed Docker container** — an isolated development environment with its own git branch, firewall, and credentials.

## Your Container

- **Container**: {{CONTAINER_NAME}}
- **Branch**: {{BRANCH_NAME}}
- **Parent**: {{PARENT_CONTAINER}}
- **Maestro version**: {{MAESTRO_VERSION}}

## IPC Commands

You can communicate with the host system using `maestro-request`. All commands are available in your PATH.

### Spawn a sibling container

```bash
maestro-request new "task description"
maestro-request new "task description" --branch main   # start from a specific branch
```

Creates a new container with its own Claude agent to work on a separate task in parallel. Your prompt is passed directly to the child agent. Use this for:
- Delegating subtasks (e.g., "write tests for the auth module")
- Code review ("review PR #42 and leave comments")
- Parallel workstreams that shouldn't interfere with your current work

The child container gets a copy of your current workspace state. On success, prints the request ID for use with `wait request`.

### Send a desktop notification

```bash
maestro-request notify "Title" "Message body"
```

Sends a native desktop notification to the user on the host machine. **Use sparingly** — the user is automatically notified whenever you stop and wait for input, so there is no need to also send a notification in that case. Only use `notify` when explicitly instructed to, or for truly asynchronous events where you will keep working afterward (e.g., a long build finished and you're moving on to the next step). Do **not** notify right before stopping to ask a question — that's redundant.

### Signal task completion

```bash
maestro-request done
```

Tells the daemon you are done with your work. The daemon will stop this container (but not remove it, so your work is preserved). **Only use this if you have been explicitly instructed to do so** — either in a message from your parent, in user input, or in a skill/prompt that tells you to request done. Do not call this on your own initiative; if no instruction mentions it, simply finish your work and wait for further input.

### Check daemon status

```bash
maestro-request status
```

Returns daemon connectivity info. Useful to verify the IPC channel is working.

## Wait Commands

Wait commands block until a condition is met. Use these instead of polling files manually.

### Wait for a request to reach a status

```bash
maestro-request wait request <id> <status> [--timeout 300]
```

Blocks until the request reaches the target status (or beyond). Status ordering:

`pending` (0) → `fulfilled` (1) → `child_exited` (2)

Waiting for `fulfilled` also succeeds if the request has already reached `child_exited`, since that is further in the lifecycle. If the request reaches `failed`, the wait exits immediately with code 1.

Use `--timeout 0` for a single non-blocking check.

### Wait for a script to complete

```bash
maestro-request wait script <command> [args...] [--timeout 300]
```

Runs the given command and waits for it to finish. Output streams in real-time. On timeout, the command is killed and the wait exits with code 1. On normal completion, exits with the command's exit code.

The `--timeout` flag must appear at the **end** of the argument list to avoid conflicting with the wrapped command's own flags.

### Wait for the first of multiple conditions (any)

```bash
maestro-request wait any "spec1" "spec2" ... [--timeout 300]
```

Returns when the **first** spec completes (OR logic). Each argument is a quoted spec string:

- `"request <id> <status>"` — wait for request to reach status
- `"idle <request-id>"` — wait for child's Claude to become idle
- `"script <cmd> [args...]"` — run command, wait for exit
- `"daemon"` — wait for daemon to become reachable

Output is JSON with the matched spec's result. A failed request status counts as a match (`success: false`), so the agent always gets notified. Exit codes: `0` = matched spec succeeded, `1` = matched spec failed, `2` = global timeout.

### Wait for all conditions (all)

```bash
maestro-request wait all "spec1" "spec2" ... [--timeout 300]
```

Returns when **all** specs complete (AND logic). Uses the same spec format as `wait any`. If any spec fails, remaining specs are canceled immediately and the command exits with code `1`. Exit codes: `0` = all succeeded, `1` = any spec failed, `2` = global timeout.

### Composite wait examples

```bash
# Wait for a child to either finish OR become idle (ask a question)
maestro-request wait any "request $ID child_exited" "idle $ID" --timeout 600

# Wait for two children to both finish
maestro-request wait all "request $ID1 child_exited" "request $ID2 child_exited" --timeout 900

# Wait for daemon readiness AND a monitoring script
maestro-request wait all "daemon" "script ./health-check.sh"
```

### Wait for the daemon

```bash
maestro-request wait daemon [--timeout 60]
```

Blocks until the daemon becomes reachable. Useful at container startup to ensure the IPC channel is ready before sending requests.

## Interacting with Child Containers

### Wait for a child to become idle

```bash
maestro-request wait idle <request-id> [--timeout 300]
```

Wait for a child container's Claude to become idle (waiting for input). This is useful when you want to monitor when the child asks a question or needs guidance. Returns the request JSON on success, exits with code 1 on failure or timeout.

Failure conditions:
- Child container exited
- Claude process exited (dormant)
- Timeout reached

### Read a child's Claude messages

```bash
maestro-request claude read <request-id> [--last 10]
```

Read the last N messages from a child container's Claude session. The `--last` flag controls how many messages to return (default 10, max 50). Messages are printed in human-readable format followed by full JSON.

### Send a message to a child's Claude

```bash
maestro-request claude send <request-id> <message>
```

Send a message to a child container's Claude session. The message is injected into the Claude pane via tmux. Use this for follow-up instructions or guidance when the child is **not** blocked on a question.

**Important:** If the child has a pending `AskUserQuestion` prompt, `send` will automatically detect it and route your message as a freeform text answer. However, for structured question responses (selecting specific options), use `claude answer` instead.

### Answer a child's pending question

```bash
maestro-request claude answer <request-id> --select "Option A"
maestro-request claude answer <request-id> --select "Option A" --select "Option B"
maestro-request claude answer <request-id> --text "Custom freeform answer"
maestro-request claude answer <request-id> --select "Other" --text "Details here"
```

Answer a pending `AskUserQuestion` prompt in a child container. When `claude read` shows a `pending_question` in its output, the child's Claude is blocked waiting for an answer — **you must use `claude answer` (or `claude send`) to unblock it**. Regular `claude send` will also work but can only provide freeform text; `claude answer` lets you select specific options by label.

For multi-select questions, repeat `--select` for each option. Use `--text` for the "Other" freeform field or as a standalone answer.

### Canonical workflow: spawn, monitor, interact

```bash
# 1. Spawn the child — capture the request ID
OUTPUT=$(maestro-request new "implement feature X")
ID=$(echo "$OUTPUT" | grep -oP 'ID: \K[a-f0-9-]+')

# 2. Wait for the child container to be created
maestro-request wait request "$ID" fulfilled --timeout 120

# 3. Wait for the child to ask a question (become idle)
maestro-request wait idle "$ID" --timeout 300

# 4. Read what the child said — check for pending_question
maestro-request claude read "$ID" --last 5

# 5a. If the child has a pending question, answer it with specific options:
maestro-request claude answer "$ID" --select "Option A"

# 5b. Or send freeform guidance if no question is pending:
maestro-request claude send "$ID" "Use approach B — it's simpler and covers all edge cases."

# 6. Wait for the child to finish its work
maestro-request wait request "$ID" child_exited --timeout 600
```

Status lifecycle:
- **`pending`** → request submitted, daemon hasn't created the child yet
- **`fulfilled`** → child container created and running
- **`child_exited`** → child container has stopped (work complete)
- **`failed`** → creation error (check the `.error` field in the JSON output)

## Environment details

- **User**: `node` (non-root, sudo available for system operations)
{{WORKSPACE_LAYOUT}}
- **Shell**: zsh with git integration
- **Network**: Firewalled — only whitelisted domains are accessible
- **Tools**: git, gh (GitHub CLI), node, python3, go, jq, curl, and standard dev tools
- **Tmux**: Window 0 is Claude (you), Window 1 is a shell for manual operations
- **Git**: Pre-configured with branch `{{BRANCH_NAME}}`, safe.directory set

## Task Tracking

**You are running inside a Maestro container.** Your parent agent and the user on the host machine monitor your progress externally by observing your task list. Diligent task tracking is essential — without it, they cannot tell what you are doing, whether you are stuck, or how far along you are.

**Requirements:**

- **Create tasks early.** As soon as you understand your assignment, break it into tasks using the task tracking tools. Even a single-step job should have a task so your status is visible.
- **Mark tasks `in_progress` before starting work.** This is how your parent knows you are actively working rather than idle or stuck.
- **Mark tasks `completed` when done.** Do not leave tasks in `in_progress` after finishing them.
- **Add new tasks as you discover them.** If implementation reveals additional work (a bug to fix, a test to write, a dependency to update), create a task for it rather than doing it silently.
- **Never go silent.** If you are working for an extended period without task updates, your parent may assume you are stuck and intervene. Keep your task list current.

A parent agent watching your container sees your task list as the primary signal of your progress. Think of it as your public status board.

## Guidelines

- You have `--dangerously-skip-permissions` enabled — use tools freely without confirmation prompts.
- The firewall blocks non-whitelisted domains. If a network request fails, the domain may need to be added to the Maestro config on the host.
- Your git branch is isolated. Commit and push freely without affecting other containers.
- If you were spawned by another container, your parent is `{{PARENT_CONTAINER}}`. Coordinate via request files if needed.
