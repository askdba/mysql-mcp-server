# Development workflow

Human and AI contributors should follow this so work stays traceable, reviewable, and safe to merge—especially when using **git worktrees**, **feature branches**, and **multiple agents**.

## Principles

- **Branch or worktree** off the integration branch (`main`) for non-trivial changes.
- **Verify before finish:** unit tests green; add or run integration checks when behavior touches MySQL, MCP tools, HTTP, or SSH.
- **Finishing is mandatory:** merge or PR, then clean up branches/worktrees—even when the *implementation* already landed elsewhere, still run verification and housekeeping (issues, changelog, release notes as appropriate).
- **Multi-agent / parallel work** only when tasks are **independent** (different subsystems, unrelated failing tests). Do not split work that shares one design decision or one root cause across parallel agents without coordination.

## Typical flow

1. **Sync:** `git fetch` and branch from current `main` (or use a dedicated [git worktree](https://git-scm.com/docs/git-worktree) for the feature).
2. **Plan (optional but recommended for multi-issue or cross-cutting work):** capture a short checklist (repo `docs/` or your team’s planning location) so agents and humans share the same definition of done.
3. **Implement:** small, reviewable commits; match existing style and tests in the touched packages.
4. **Test:**
   - `go test ./...`
   - For parity with the QA unit job, also run: `go test -race -coverprofile=coverage.out -covermode=atomic ./...` then remove `coverage.out` if you do not want the artifact (do not commit it unless the project explicitly tracks coverage files).
   - If you changed integration-sensitive code: `make test-integration` or the Makefile targets documented in `README.md` (Docker / Compose constraints apply).
5. **Finish:** choose one path and complete it:
   - Push branch and open a **Pull Request** (preferred for shared repos), or
   - Merge locally to `main` if that is your team’s process, then delete the feature branch.
6. **Cleanup:** remove stale worktrees and local branches after merge.

## Grouping issues and PRs

- Prefer **one cohesive PR** when issues share one implementation (e.g. retry behavior + docs + tests).
- Use **separate PRs** when changes are independent and can be reviewed or reverted separately.
- “Already on `main`” does **not** mean “skip workflow”: it means you **verify on `main`**, close or update issues, and document releases as needed.

## CI parity (run before you push)

These mirror [`.github/workflows/go-ci.yml`](.github/workflows/go-ci.yml) and the **unit-test** portion of [`.github/workflows/qa.yml`](.github/workflows/qa.yml):

```bash
go mod download
go build ./...
go vet ./...
go test ./...
go test -race -coverprofile=coverage.out -covermode=atomic ./...
```

The full **QA** workflow on GitHub also runs golangci-lint (informational in CI), gosec/govulncheck (informational), integration tests against MySQL 8.0/8.4/9.0 and MariaDB 11.4, multi-OS builds, Docker build, and REST API smoke tests. Locally, use `make test-integration` (or Compose per `README.md`) when you touch database, MCP tool, HTTP, or SSH paths.

Optional locally: `golangci-lint run --timeout=5m` if you have it installed (same tool family as QA).

## Pull requests, CI, and bot review

1. Push a branch and open a **Pull Request** against `main` so **Go CI** and **QA Pipeline** run on GitHub Actions.
2. In the PR description, link work to GitHub issues using a **closing keyword** so the issue auto-closes when the PR merges: **`Closes #123`**, **`Fixes #123`**, or **`Resolves #123`** (one issue per line or comma-separated is fine). Plain **`Refs #123`** does **not** close the issue. See [Linking a pull request to an issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/linking-a-pull-request-to-an-issue).
3. Treat **required** outcomes as: successful **unit tests**, **build**, and **integration tests** (MySQL and MariaDB) jobs (the QA summary job fails the workflow if any of those fail; lint and some security steps are configured as non-blocking—see `qa.yml`).
4. Read and act on **automated review** feedback (for example GitHub **Copilot** review, **Cursor Bugbot**, or similar): fix correctness, security, and clear regressions; use judgment on pure style suggestions.
5. Re-push until checks you care about are green, then proceed with human review per team practice.

## Open backlog snapshot

**Snapshot date: 2026-04-05** (refreshed after closing delivered items). Regenerate with `gh issue list --state open` (or the GitHub UI). This table is a **working snapshot** for agents and contributors, not an automated export.

| # | Title |
|---|--------|
| 103 | Feature: write_query tool with explicit confirmation for INSERT/UPDATE/DELETE |
| 80 | [Feature]: Natural Language to SQL tool (ask_nl_sql) |
| 64 | Add support for local LLMs (Ollama, LM Studio, llama.cpp) |
| 24 | Add TiDB compatibility support |

**Recently delivered (closed):** #106 (`add_connection`, merged PR **#127**); #104 (richer EXPLAIN, merged PR **#126**); earlier: #102 (metrics HTTP sidecar), #117 (column masking), #119 (`schema_diff`), #120 (`search_schema`), #110 / #111 / #121 (retries, pagination, pool ping)—on `main` via PRs **#122**–**#124** and subsequent merges.

## AI-specific notes

- **Superpowers** (or similar): invoke relevant skills early; follow their checklists when they apply.
- **Parallel agents:** one agent per independent domain (example split: core library change vs. README vs. unrelated test file failures). Merge results carefully to avoid conflicting edits to the same files.
- **Ask mode / read-only:** no commits or pushes; still read `workflow.md` and `AGENTS.md` so recommendations match team process.
