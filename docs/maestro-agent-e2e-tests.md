# maestro-agent E2E Test Runbook

End-to-end tests for maestro-agent hooks running in a real Docker container. These validate the full hook lifecycle that unit tests can't cover (Docker exec, tmux interaction, settings.json wiring, state file paths).

## Quick Start

```bash
# 1. Build everything (both binary AND Docker image must be fresh)
make all

# 2. Create a test container
maestro new -en "test hook e2e"

# 3. Run all automated tests (65 tests across 8 suites)
docker/maestro-agent/e2e/run-all.sh <container-name>

# 4. Run interactive user-connection test (requires human)
docker/maestro-agent/e2e/test-ask-connect.sh <container-name>
```

## Test Scripts

All scripts live in `docker/maestro-agent/e2e/` and accept a container name (with or without `maestro-` prefix, or a fuzzy search term).

### Runner & Utilities

| Script | Purpose |
|---|---|
| `run-all.sh <container>` | Run all 8 automated test suites (65 tests) |
| `query.sh <container>` | Inspect container state: agent state, idle flag, question/response files, Claude process, tmux sessions, recent logs |
| `set-state.sh <container> <state> [--with-question]` | Set agent to a specific state (active, idle, question, waiting, starting, clearing). Correctly manages idle flag. |

### Test Suites

| Script | Tests | What's Tested |
|---|---|---|
| `test-image.sh` | 12 | Legacy scripts absent, new subcommands registered, `--idle` flag |
| `test-claude.sh` | 2 | Claude process running, tmux session exists |
| `test-settings.sh` | 15 | Hook wiring, timeouts, matchers, no legacy references, valid JSON |
| `test-pre-tool-use.sh` | 6 | Default → active, `--idle` → idle, question → active |
| `test-post-tool-use.sh` | 8 | AskUserQuestion cleanup, non-Ask preservation, EnterPlanMode, empty stdin |
| `test-ask-hook.sh` | 10 | Question extraction, state during wait, response delivery (stderr + exit 2), stale file handling |
| `test-host-display.sh` | 6 | Host-side `maestro list` indicators for each state |
| `test-logs.sh` | 6 | Log file exists, valid JSON structure, hook-specific fields |
| `test-ask-connect.sh` | 3 | **(INTERACTIVE)** User connection detection — requires human to `maestro connect` |

### Shared Library

`lib.sh` provides:
- **Container resolution** — accepts full names, short names, or fuzzy search
- **State helpers** — `read_state`, `has_idle_flag`, `set_agent_state`, etc.
- **Assertions** — `assert_eq`, `assert_contains`, `assert_not_contains`
- **Path constants** — mirrors `paths.go` (state file, idle flag, question file, etc.)

## Key Paths

The agent state file lives at `/home/node/.maestro/state/agent-state` (NOT `.maestro/agent-state`).

| File | Path |
|---|---|
| Agent state | `/home/node/.maestro/state/agent-state` |
| Claude PID | `/home/node/.maestro/state/claude-pid` |
| Idle flag (compat) | `/home/node/.maestro/claude-idle` |
| Question file | `/home/node/.maestro/current-question.json` |
| Response file | `/home/node/.maestro/question-response.txt` |
| Agent log | `/home/node/.maestro/logs/maestro-agent.log` |

## Gotchas & Lessons Learned

1. **State file path:** The agent state file is at `.maestro/state/agent-state`, not `.maestro/agent-state`. The `state/` subdirectory is easy to miss.

2. **Response file timing:** The ask hook removes `question-response.txt` before starting watchers (clears stale responses). The daemon must write the response AFTER seeing "question" state. Tests that pre-write the response file will hang.

3. **Ask hook exit code 2:** The ask hook uses `os.Exit(2)` for response delivery. This propagates through `docker exec` and can trigger `set -e` or `set -o pipefail` in test scripts. All ask hook invocations in test scripts use `; true` or `|| true` to handle this.

4. **isMainClaude() and docker exec:** When no `claude-pid` file exists (common for interactive containers), `isMainClaude()` returns `true` for all callers including `docker exec`. If a `claude-pid` file exists, hooks called via `docker exec` will exit silently.

5. **Docker image freshness:** Always `make all` (not just `make build`) before e2e tests. The maestro-agent binary is baked into the Docker image.

6. **Claude may exit during testing:** If testing over 20+ minutes, Claude may finish and exit. Hooks still work via `docker exec`, but `test-claude.sh` will fail.

7. **Backward-compat idle flag:** Both `idle` and `question` states create the `claude-idle` flag; all other states remove it.

8. **SIGPIPE with head:** Using `| head -1` with `set -o pipefail` causes exit 141 (SIGPIPE). Capture full output to a variable first, then extract lines.

## Missing Test Coverage

### Service (heartbeat, idle wake-up, clear timer)

The `maestro-agent service` background process has no e2e test suite. A `test-service.sh` should cover:

1. **Heartbeat delivery during idle state.** Write an `agent.yml` manifest with a short heartbeat interval (e.g. 5s) and a trivial heartbeat script. Set state to `idle`, start the service, and verify a `.txt` file appears in `pending-messages/` within the interval. This would have caught the inverted `SuppressWhileActive` condition (commit 9f54a51) where heartbeats never fired during idle with the default config.

2. **Heartbeat suppression during active state.** Same setup, but set state to `active` with `suppress_while_active: true`. Verify NO heartbeat file appears within 2x the interval.

3. **Idle wake-up.** Set state to `idle`, drop a message into `pending-messages/`, and verify the service sends "continue" to tmux and transitions to `active`. (Requires tmux running in the container.)

4. **Clear timer.** Write manifest with `clear_after: 5`, set state to `idle`, and verify the state transitions to `clearing` → `starting` within ~7s.

These tests need a running `maestro-agent service` process and a manifest, so the test script should write the manifest, start the service in the background, run assertions, and clean up. The service polls every 2s so timing assertions need ~3s tolerance.
