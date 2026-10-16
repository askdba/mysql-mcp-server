# Agent instructions

This file guides AI coding agents (Cursor, Codex, Copilot CLI, and similar) working on **mysql-mcp-server**.

## Workflow

Follow **[workflow.md](./workflow.md)** for branching, worktrees, multi-agent splits, verification, and how to finish work (merge / PR / cleanup). Do not skip that process because a change already exists on `main`—**verification and finishing steps still apply**.

## Priority

1. **Explicit user or maintainer instructions** in this repo (e.g. issue text, PR comments, chat) take precedence.
2. **Installed agent skills** (for example the Superpowers plugin) override generic defaults where they apply.
3. Otherwise use normal Go and project conventions.

## Project basics

- **Language:** Go (`go 1.26+` per `go.mod`).
- **Tests:** Run `go test ./...` before treating work as complete. Integration tests (`make test-integration` or Compose-backed targets in the `Makefile`) matter when you touch connection, tools, or Docker/SSH paths.
- **Scope:** Keep changes focused on the requested task; avoid unrelated refactors.

## Before opening a PR

Run **CI parity** commands in [`workflow.md`](./workflow.md) (build, vet, tests, optional race coverage) so local results match [`.github/workflows/go-ci.yml`](.github/workflows/go-ci.yml) and the QA unit job. After you push, **Go CI** and **QA** run on GitHub; address **bot / automated review** feedback when it flags real bugs, security issues, or regressions.

## Superpowers alignment (summary)

When the environment provides Superpowers-style skills (`using-git-worktrees`, `writing-plans`, `subagent-driven-development`, `dispatching-parallel-agents`, `finishing-a-development-branch`, etc.), use them in line with **[workflow.md](./workflow.md)**. Parallel agents are for **independent** subproblems; shared design or one root cause should stay sequential.
