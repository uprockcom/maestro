# maestro-request E2E Test Runbook

End-to-end tests for `maestro-request` — the container-side IPC binary that Claude uses to communicate with the host daemon. These validate request file creation, argument validation, wait/poll behavior, and daemon-unavailable fallback paths that unit tests can't cover.

## Quick Start

```bash
# 1. Build everything (both binary AND Docker image must be fresh)
make all

# 2. Create a test container
maestro new -en "test request e2e"

# 3. Run all automated tests (~75 tests across 11 suites)
docker/maestro-request-go/e2e/run-all.sh <container-name>
```

## Delegating to a Subagent

The e2e scripts are self-contained — spawn a Bash subagent (model: sonnet) with the `run-all.sh` command and it will run the full suite autonomously:

```
Spawn a Bash subagent (model: sonnet) with:
  docker/maestro-request-go/e2e/run-all.sh <container>
```

This avoids polluting the main context with dozens of docker exec calls. The subagent will report pass/fail counts when done.

## Test Scripts

All scripts live in `docker/maestro-request-go/e2e/` and accept a container name (with or without `maestro-` prefix, or a fuzzy search term).

### Runner & Utilities

| Script | Purpose |
|---|---|
| `run-all.sh <container>` | Run all 11 automated test suites (~75 tests) |
| `query.sh <container>` | Inspect container state: request files, pending messages, daemon-ipc presence, hostname |

### Test Suites

Suites are ordered container-local first (no daemon needed), then daemon-dependent:

| Script | Tests | What's Tested |
|---|---|---|
| `test-new.sh` | 8 | Request file creation, action/task/parent/status/timestamp fields, `--branch` flag, daemon fallback |
| `test-done.sh` | 5 | Action="exit" wire protocol, parent=hostname, status=pending, daemon fallback |
| `test-notify.sh` | 8 | Title/message fields, length validation (64/256 limits), boundary tests, daemon fallback |
| `test-request.sh` | 7 | Type validation (domain/memory/cpus/ip), request_type/request_value fields, daemon fallback |
| `test-wait-script.sh` | 8 | Exit code propagation, stdout capture, timeout kills process, `--timeout 0` infinite, multi-word commands |
| `test-wait-message.sh` | 7 | Pre-queued immediate return, multiple messages, file deletion, sort order, timeout exit 2, newline trimming |
| `test-wait-request.sh` | 8 | Already-fulfilled, polling until fulfilled, failed→exit 1, timeout→exit 1, single-check mode, invalid target status |
| `test-wait-composite.sh` | 8 | `wait any` first-wins/canceled-others/timeout, `wait all` both-succeed/results-array/one-fails/timeout |
| `test-status.sh` | 3 | Daemon reachable→JSON, no daemon-ipc→exit 1, fake config→exit 1 |
| `test-wait-daemon.sh` | 3 | Daemon available→immediate, JSON output, timeout→exit 1 |
| `test-claude.sh` | 8 | `answer` validation (no flags→exit 1), request file creation for answer/read/send, arg joining, daemon required for all |

### Shared Library

`lib.sh` provides:
- **Container resolution** — accepts full names, short names, or fuzzy search
- **Request file helpers** — `list_requests`, `count_requests`, `latest_request_id`, `request_field`, `clean_requests`, `write_request_file`
- **Message queue helpers** — `write_message`, `clean_messages`, `count_messages`
- **Daemon helpers** — `has_daemon_ipc`, `disable_daemon`, `restore_daemon`
- **JSON parsing** — `cjq` routes jq through the container (host may not have jq)
- **Assertions** — `assert_eq`, `assert_contains`, `assert_not_contains`, `assert_exit_code`
- **Path constants** — mirrors `request.go` and `daemon.go`

## Key Paths

| File | Path |
|---|---|
| Request files | `/home/node/.maestro/requests/<uuid>.json` |
| Pending messages | `/home/node/.maestro/pending-messages/<name>.txt` |
| Daemon IPC config | `/home/node/.maestro/daemon/daemon-ipc.json` |

## Two Testing Tiers

### Tier 1: Container-Local (no daemon needed)

These tests work by running `maestro-request` commands and inspecting the request files created on disk. For wait commands, synthetic request files or message files are written directly.

- `test-new.sh`, `test-done.sh`, `test-notify.sh`, `test-request.sh`
- `test-wait-script.sh`, `test-wait-message.sh`, `test-wait-request.sh`, `test-wait-composite.sh`

### Tier 2: Daemon-Dependent

These need the daemon running (or at minimum `daemon-ipc.json` present) for some tests. Tests gracefully handle daemon being present-but-unreachable vs. completely absent.

- `test-status.sh`, `test-wait-daemon.sh`, `test-claude.sh`

## Gotchas & Lessons Learned

1. **Wire protocol action names:** `done` command creates action=`"exit"` (not `"done"`). `claude answer` creates action=`"answer_question"`. Always check the Go source for the actual wire value.

2. **Exit code conventions:** `wait message`, `wait any`, `wait all` use exit code **2** for timeout (not 1). All other commands use exit 1 for errors/timeouts. Tests must use `|| true` or explicit exit code capture.

3. **Daemon disable/restore:** Tests that need daemon-unavailable behavior use `mv daemon-ipc.json .bak` and restore after. Always restore even on test failure — the restore runs unconditionally after each test.

4. **jq on host vs container:** The host machine may not have `jq`. All JSON parsing uses the `cjq` helper which pipes through `docker exec -i` to use the container's jq.

5. **`wait script` streams output:** The standalone `wait script` command streams stdout/stderr directly (not captured internally). But `docker exec` captures it, so test assertions on output work normally.

6. **`wait script --timeout` parsing:** The `--timeout` flag must be the last two args (flag parsing is disabled for `wait script`). This is different from other wait commands where `--timeout` is a regular cobra flag.

7. **Simulating request fulfillment:** For `wait request` tests, write request JSON files directly to `/home/node/.maestro/requests/`. The polling loop reads the file regardless of who wrote it.

8. **`wait any`/`wait all` spec strings:** Each spec is a single quoted argument with space-separated type+args: `"script echo hello"`, `"message"`, `"request <id> fulfilled"`.

9. **Docker image freshness:** Always `make all` (not just `make build`) before e2e tests. The `maestro-request` binary is baked into the Docker image at build time.

10. **`request` valid types:** Only `domain`, `memory`, `cpus`, `ip`. Any other type returns an error via cobra (exit 1).
