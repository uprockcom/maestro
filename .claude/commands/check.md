It seems like we're ready to commit our work. Before we do so, let's run our pre-commit checks.

## Task Tracking

Create a task for this check run (`subject: "Run pre-commit checks"`, `activeForm: "Running pre-commit checks"`) and mark it `in_progress`. Mark it `completed` when all checks pass, or when you've reported failures to the user. This signals completion to external monitoring systems.

---

Please perform the following tasks in order, and don't hesitate to ask for my input if something needs additional guidance:

1. **Fix formatting first**: Run `gofmt -w .` from the main project directory to auto-fix any Go formatting issues.

2. **Run pre-commit checks**: Run `./scripts/pre_commit.sh` which will:
   - Verify Go formatting is correct
   - Check compilation (`go build ./...`)
   - Run all Go tests (`go test ./...`)
   - Run golangci-lint (if available)

   If this fails, fix the issues and re-run until it passes.

3. **Prepare commit**:
   - Review `git status` and prepare a list of files to commit
   - Ensure all relevant files are staged
   - Prepare a commit message that summarizes the changes — clear, concise, but complete
   - Remember: don't include "Claude Code" or "Co-Authored-By" in the commit message

4. **Commit the changes**

5. **Offer to open a PR**: After committing, ask the user if they would like to open a pull request. Only if the user says yes:
   - Push the branch and open the PR
   - Request a Copilot code review by running `./scripts/request_copilot_review.sh`
   - **If running in a Maestro environment** (i.e., the `maestro-request` command is available on PATH):
     1. Spawn a parallel code review by running:
        ```bash
        OUTPUT=$(maestro-request new "/pr_review <PR_NUMBER>")
        ID=$(echo "$OUTPUT" | grep -oP 'ID: \K[a-f0-9-]+')
        ```
        Replace `<PR_NUMBER>` with the actual PR number. If the spawn fails or `$ID` is empty, update your task's `activeForm` to `"Awaiting Copilot review"` and fall back to waiting for Copilot only via `maestro-request wait script ./scripts/wait_for_copilot_review.sh --timeout 1800`. Once the wait completes (exit 0), proceed to `/address_review`. If it exits non-zero, stop and wait for instructions. Skip steps 2-4.
     2. Update your current task's `activeForm` to `"Awaiting code reviews"` before waiting.
     3. Use `maestro-request wait all` to block until **both** the Copilot review and the parallel code review complete:
        ```bash
        maestro-request wait all "script ./scripts/wait_for_copilot_review.sh" "request $ID child_exited" --timeout 1800
        ```
        Do NOT run `wait_for_copilot_review.sh` directly — you must use `maestro-request wait all` so the orchestrator can track the wait.
     4. If the wait exits 0 (both reviews completed), execute the `/address_review` skill to address any feedback. If the wait exits non-zero, check for available feedback by running `gh pr view <PR_NUMBER> --json comments` and `./scripts/view_line_comments.sh <PR_NUMBER>`. If at least one review produced feedback, still proceed with `/address_review`. Only stop and wait for instructions if neither review produced any feedback.
   - **If NOT in a Maestro environment** (i.e., `maestro-request` is not available): stop after requesting the Copilot review — do not wait or attempt to address feedback automatically.
