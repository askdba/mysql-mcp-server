# CI/Release Workflow Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix seven CI/release workflow gaps identified in issue #128 — pre-release QA gate, format enforcement, integration-test blocking, health-poll, lint pinning, action version alignment, and stale docs.

**Architecture:** All changes are in `.github/workflows/` YAML files and `workflow.md`. No Go code changes. Each task is independently testable via `gh workflow view` or direct inspection.

**Tech Stack:** GitHub Actions, GoReleaser, golangci-lint, gofmt, bash

---

## File Map

| File | Changes |
|------|---------|
| `.github/workflows/release.yml` | Add `pre-release-check` job; gate `goreleaser` on it; align `build-push-action` to `@v6` |
| `.github/workflows/qa.yml` | Fix format check; expand `qa-summary` failure policy; replace `sleep 3`; pin lint version; align `build-push-action` to `@v6` |
| `workflow.md` | Remove stale issue #104 from backlog |

---

### Task 1: Fix no-op format check in qa.yml

**Files:**
- Modify: `.github/workflows/qa.yml:34-38`

Current code (no-op):
```yaml
- name: Check formatting
  run: |
    echo "Skipping format check - code is verified formatted locally"
    go fmt ./... || true
```

- [ ] **Step 1: Replace with real gofmt enforcement**

Edit `.github/workflows/qa.yml`, replace the `Check formatting` step:
```yaml
      - name: Check formatting
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "The following files are not gofmt-formatted:"
            echo "$unformatted"
            exit 1
          fi
```

- [ ] **Step 2: Verify the YAML is valid**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/qa.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 3: Verify locally that current code passes**

```bash
unformatted=$(gofmt -l .) && [ -z "$unformatted" ] && echo "All files formatted" || echo "Unformatted: $unformatted"
```
Expected: `All files formatted`

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/qa.yml
git commit -m "fix(ci): enforce gofmt -l format check instead of no-op go fmt"
```

---

### Task 2: Pin golangci-lint to a specific version

**Files:**
- Modify: `.github/workflows/qa.yml` — `golangci-lint-action` step

- [ ] **Step 1: Update lint action version**

In `.github/workflows/qa.yml`, find:
```yaml
      - name: Install golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
          args: --timeout=5m
        continue-on-error: true  # Don't block pipeline on lint issues
```

Replace with:
```yaml
      - name: Install golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: v1.64.8
          args: --timeout=5m
        continue-on-error: true  # Don't block pipeline on lint issues
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/qa.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/qa.yml
git commit -m "fix(ci): pin golangci-lint to v1.64.8 to prevent surprise breakage"
```

---

### Task 3: Replace sleep 3 with health-poll in api-tests

**Files:**
- Modify: `.github/workflows/qa.yml` — `Start REST API server` step

- [ ] **Step 1: Replace sleep with poll loop**

In `.github/workflows/qa.yml`, find:
```yaml
      - name: Start REST API server
        env:
          MYSQL_DSN: "mcpuser:mcppass00@tcp(127.0.0.1:3306)/testdb?parseTime=true"
          MYSQL_MCP_HTTP: "1"
          MYSQL_MCP_EXTENDED: "1"
        run: |
          ./mysql-mcp-server &
          sleep 3
```

Replace with:
```yaml
      - name: Start REST API server
        env:
          MYSQL_DSN: "mcpuser:mcppass00@tcp(127.0.0.1:3306)/testdb?parseTime=true"
          MYSQL_MCP_HTTP: "1"
          MYSQL_MCP_EXTENDED: "1"
        run: |
          ./mysql-mcp-server &
          for i in $(seq 1 20); do
            if curl -sf http://localhost:9306/health > /dev/null 2>&1; then
              echo "Server ready after ${i}s"
              break
            fi
            echo "Waiting for server... (${i}/20)"
            sleep 1
          done
          curl -sf http://localhost:9306/health > /dev/null || { echo "Server did not start"; exit 1; }
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/qa.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/qa.yml
git commit -m "fix(ci): replace sleep 3 with health-poll loop in api-tests"
```

---

### Task 4: Expand qa-summary failure policy to include integration tests

**Files:**
- Modify: `.github/workflows/qa.yml:410-413`

- [ ] **Step 1: Expand the failure condition**

In `.github/workflows/qa.yml`, find:
```yaml
          # Only fail on unit tests and build - lint/security are informational
          if [ "${{ needs.unit-tests.result }}" != "success" ] || \
             [ "${{ needs.build.result }}" != "success" ]; then
            echo "❌ Critical jobs failed"
            exit 1
          fi
```

Replace with:
```yaml
          # Fail on unit tests, build, and integration tests; lint/security are informational
          if [ "${{ needs.unit-tests.result }}" != "success" ] || \
             [ "${{ needs.build.result }}" != "success" ] || \
             [ "${{ needs.integration-tests.result }}" != "success" ] || \
             [ "${{ needs.integration-tests-mariadb.result }}" != "success" ]; then
            echo "❌ Critical jobs failed"
            exit 1
          fi
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/qa.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/qa.yml
git commit -m "fix(ci): include integration tests in qa-summary failure condition"
```

---

### Task 5: Align docker/build-push-action to @v6 in qa.yml

**Files:**
- Modify: `.github/workflows/qa.yml` — Docker Build job

- [ ] **Step 1: Update the action version**

In `.github/workflows/qa.yml`, find:
```yaml
      - name: Build Docker image
        uses: docker/build-push-action@v5
```

Replace with:
```yaml
      - name: Build Docker image
        uses: docker/build-push-action@v6
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/qa.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/qa.yml
git commit -m "fix(ci): align docker/build-push-action to @v6 in qa.yml"
```

---

### Task 6: Add pre-release-check job to release.yml

This is the highest-severity fix: gate `goreleaser` and `docker` on passing unit + integration tests.

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Add pre-release-check job**

In `.github/workflows/release.yml`, insert a new job BEFORE the `goreleaser` job. Add after line 11 (`jobs:`):

```yaml
  pre-release-check:
    name: Pre-release QA Gate
    runs-on: ubuntu-latest
    services:
      mysql:
        image: mysql:8.0
        env:
          MYSQL_ROOT_PASSWORD: testpass
          MYSQL_DATABASE: testdb
        ports:
          - 3306:3306
        options: >-
          --health-cmd="mysqladmin ping -h localhost -u root -ptestpass"
          --health-interval=10s
          --health-timeout=5s
          --health-retries=5
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - name: Download modules
        run: go mod download

      - name: Build
        run: go build ./...

      - name: Unit tests
        run: go test -race -covermode=atomic ./...

      - name: Wait for MySQL
        run: |
          for i in $(seq 1 30); do
            if mysqladmin ping -h 127.0.0.1 -u root -ptestpass --silent 2>/dev/null; then
              echo "MySQL ready"
              break
            fi
            echo "Waiting for MySQL... (${i}/30)"
            sleep 2
          done

      - name: Initialize test database
        run: |
          mysql -h 127.0.0.1 -u root -ptestpass < tests/sql/init.sql
          mysql -h 127.0.0.1 -u root -ptestpass < tests/sql/mcp_test_user.sql

      - name: Integration tests
        env:
          MYSQL_DSN: "mcpuser:mcppass00@tcp(127.0.0.1:3306)/testdb?parseTime=true"
          MYSQL_TEST_DSN: "mcpuser:mcppass00@tcp(127.0.0.1:3306)/testdb?parseTime=true"
        run: go test -v -tags=integration ./...

```

- [ ] **Step 2: Gate goreleaser on pre-release-check**

In `.github/workflows/release.yml`, find:
```yaml
  goreleaser:
    runs-on: ubuntu-latest
    steps:
```

Replace with:
```yaml
  goreleaser:
    runs-on: ubuntu-latest
    needs: pre-release-check
    steps:
```

- [ ] **Step 3: Validate YAML**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML valid"
```
Expected: `YAML valid`

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "fix(ci): add pre-release-check job; gate goreleaser on unit + integration tests"
```

---

### Task 7: Remove stale issue #104 from workflow.md backlog

**Files:**
- Modify: `workflow.md`

- [ ] **Step 1: Update the backlog table**

In `workflow.md`, find the backlog table row:
```markdown
| 104 | Enhancement: richer EXPLAIN as structured output (document existing explain_query) |
```

Remove it entirely. The row for issue #104 is gone; PR #126 merged it.

Also update the "Recently delivered" line to include #104:

Find:
```markdown
**Recently delivered (closed):** #106 (`add_connection`, merged PR **#127**); earlier: #102 (metrics HTTP sidecar),
```

Replace with:
```markdown
**Recently delivered (closed):** #106 (`add_connection`, merged PR **#127**); #104 (richer EXPLAIN, merged PR **#126**); earlier: #102 (metrics HTTP sidecar),
```

- [ ] **Step 2: Commit**

```bash
git add workflow.md
git commit -m "docs: remove stale issue #104 from workflow.md backlog (merged in PR #126)"
```

---

### Task 8: Open PR

- [ ] **Step 1: Push branch**

```bash
git push -u origin fix/issue-128-workflow-hardening
```

- [ ] **Step 2: Create PR**

```bash
gh pr create \
  --title "fix(ci): workflow hardening — pre-release gate, format enforcement, summary policy, lint pinning" \
  --body "$(cat docs/superpowers/plans/2026-04-19-issue-128-workflow-hardening.md | head -10)" \
  --base main
```

Use this PR body:
```
## Summary

- Add `pre-release-check` job to `release.yml` that runs unit + integration tests before GoReleaser fires
- Gate `goreleaser` job on `pre-release-check` success
- Fix no-op format check: replace `go fmt ./... || true` with real `gofmt -l` enforcement
- Expand `qa-summary` failure policy to block on integration test failures (MySQL + MariaDB)
- Replace fragile `sleep 3` with health-poll loop in `api-tests`
- Pin `golangci-lint` to `v1.64.8`
- Align `docker/build-push-action` to `@v6` in `qa.yml`
- Remove stale issue #104 from `workflow.md` backlog

Closes #128

## Test plan
- [ ] All YAML files parse without error
- [ ] `gofmt -l .` returns empty (no unformatted files)
- [ ] GitHub Actions run green on this PR
```
