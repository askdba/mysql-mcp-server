# Changelog

All notable changes to this project will be documented in this file.

The format is based on "Keep a Changelog" and this project follows
Semantic Versioning.

## [Unreleased]

## [1.7.1-rc.4] - 2026-06-05

### Fixed

- **Lint**: Removed unused `getClient` function from `connection.go`; changed `%v` to `%w` in `tools_extended.go` to preserve error wrapping; rewrote two `if-else` chains to `switch` statements in `search_schema` query builder.
- **CI**: Added MySQL 9.7 to the integration test matrix.

### Tests

- **`TestQueryAdvisorResourceURIs`**: New unit test asserting the `query_advisor` prompt's closing instruction contains both MCP resource URIs (`docs://mysql-mcp-server/query-optimization-guide` and `docs://mysql-mcp-server/query-optimization-comprehensive`).

## [1.7.1-rc.3] - 2026-06-04

### Fixed

- **Docker image build**: Added `.dockerignore` exceptions for `docs/embed.go`, `docs/query_optimization_guide.md`, and `docs/mysql_query_optimization_comprehensive.md` so the Go embed directives can locate the optimization guide files inside the Docker build context.

## [1.7.1-rc.2] - 2026-06-04

### Added

- **MCP Resources**: Two query optimization guides are now available as MCP resources (`docs://mysql-mcp-server/query-optimization-guide`, `docs://mysql-mcp-server/query-optimization-comprehensive`), surfacing SQL optimization patterns and advanced indexing strategies to Claude on demand. Content is embedded in the binary at build time.
- **`run_query` tool description**: Updated to direct users toward the `query_advisor` prompt for complex query optimization workflows.
- **`query_advisor` prompt**: Now references the two MCP resource URIs in its closing instruction for detailed index strategies and optimization patterns.

## [1.7.1-rc.1] - 2026-06-04

### Added

- **`MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS`** (opt-in, default off): lifts the default block on the read-only diagnostic schemas `information_schema`, `performance_schema`, and `sys`, so `run_query` / `explain_query` can reference them for index diagnostics (cardinality via `information_schema.STATISTICS`, index usage via `performance_schema.table_io_waits_summary_by_index_usage`, redundant/unused indexes via `sys.*`). The `mysql` schema remains blocked unconditionally, and access is still subject to the connection's MySQL grants. Orthogonal to `MYSQL_MCP_ALLOWED_DATABASES`: both are enforced, so when an allowlist is set the relevant system schemas must also be added to it. As part of this, `explain_query` now runs the same `ValidateSQLCombined` checks as `run_query` (it previously did not, so EXPLAIN could reach system schemas regardless of any guard). Configurable via env var or the `security.allow_system_schemas` config-file key.

## [1.7.0] - 2026-04-19

General availability release. Promotes rc.4 to GA plus CI/release pipeline hardening.

### Added

- **`add_connection`** (optional): register a new named MySQL connection at runtime via MCP when **`MYSQL_MCP_EXTENDED=1`** and **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`**; rejects duplicate names, invalid DSNs, and the **`root`** MySQL user ([#106](https://github.com/askdba/mysql-mcp-server/issues/106)).
- **`search_schema`**: Find tables and columns matching a pattern across all accessible databases.
- **`schema_diff`**: Compare table and column structures between two databases.
- **Column Masking**: Redact sensitive data in `run_query` results using **`MYSQL_MCP_MASK_COLUMNS`** (e.g., `email,password,token`).
- **`run_query`** / **`ping`**: exponential-backoff retries for transient MySQL/network errors, with pool **`Ping`** after **`driver.ErrBadConn`** ([#110](https://github.com/askdba/mysql-mcp-server/issues/110), [#121](https://github.com/askdba/mysql-mcp-server/issues/121)).
- **`run_query`**: **`offset`** pagination for SELECT/UNION, returning **`has_more`** and **`next_offset`** ([#111](https://github.com/askdba/mysql-mcp-server/issues/111)).
- **`MYSQL_MCP_METRICS_HTTP`**: optional HTTP sidecar for `/status` and `/api/metrics/tokens` while MCP uses stdio ([#102](https://github.com/askdba/mysql-mcp-server/issues/102)).

### Security

- **SSH bastion host keys**: tunnel verifies server host key by default using `known_hosts`; opt-out requires `MYSQL_SSH_STRICT_HOST_KEY_CHECKING=false`.

### Changed

- **`getEnvBool`**: accepts `true`, `yes`, `on`, `y` (case-insensitive) in addition to `1` for all `MYSQL_MCP_*` flags.
- **CI/release pipeline** ([#128](https://github.com/askdba/mysql-mcp-server/issues/128)): pre-release QA gate in `release.yml` (unit + integration tests run before GoReleaser fires); `gofmt -l` format enforcement; integration test failures now block `qa-summary`; `golangci-lint` pinned to `v1.64.8`.

---

## [1.7.0-rc.4] - 2026-04-11

Fourth release candidate: optional runtime DSN registration (`add_connection`), SSH bastion host key verification by default, and extended tooling improvements.

### Security

- **SSH bastion host keys**: the tunnel now verifies the server host key by default using OpenSSH-style **`known_hosts`** (default file `~/.ssh/known_hosts`, or **`MYSQL_SSH_KNOWN_HOSTS`** / config **`known_hosts`**) or a pinned fingerprint (**`MYSQL_SSH_HOST_KEY_FINGERPRINT`** / **`host_key_fingerprint`**). To disable verification (MITM risk), you must **opt in** with **`MYSQL_SSH_STRICT_HOST_KEY_CHECKING=false`** or **`ssh_strict_host_key_checking: false`**. See README.

### Added

- **`add_connection`** (optional): register a new named MySQL connection at runtime via MCP when **`MYSQL_MCP_EXTENDED=1`** and **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`**; rejects duplicate names, invalid DSNs, and the **`root`** MySQL user ([#106](https://github.com/askdba/mysql-mcp-server/issues/106)).
- **`search_schema`**: Find tables and columns matching a pattern across all accessible databases.
- **`schema_diff`**: Compare table and column structures between two databases.
- **Column Masking**: Redact sensitive data in `run_query` results using **`MYSQL_MCP_MASK_COLUMNS`** (e.g., `email,password,token`).
- **`run_query`** / **`ping`**: exponential-backoff retries for transient MySQL/network errors (bad pooled connections, deadlocks, lock wait timeouts, etc.), with an optional pool **`Ping`** after **`driver.ErrBadConn`** to recover faster after MySQL restarts ([#110](https://github.com/askdba/mysql-mcp-server/issues/110), [#121](https://github.com/askdba/mysql-mcp-server/issues/121)).
- **`run_query`**: **`offset`** pagination for SELECT/UNION (server-side **`LIMIT … OFFSET`**), returning **`has_more`** and **`next_offset`** ([#111](https://github.com/askdba/mysql-mcp-server/issues/111)).

## [1.7.0-rc.3] - 2026-03-31

Third release candidate: metrics HTTP sidecar for stdio MCP (Claude Desktop) and friendlier boolean env parsing.

### Added

- **`MYSQL_MCP_METRICS_HTTP`**: optional HTTP listener on **`MYSQL_HTTP_PORT`** while MCP uses **stdio** — **`GET /health`**, **`GET /api/metrics/tokens`**, **`GET /status`** in-process with the MCP server so token metrics match Claude Desktop usage ([#102](https://github.com/askdba/mysql-mcp-server/issues/102)).
- **SSH tunneling (bastion host)**: connect to MySQL via `ssh_host`, `ssh_user`, `ssh_key_path`, and optional `ssh_port` (config file or `MYSQL_SSH_*` env vars). `key_path` supports `~` and `~/path` (expanded to user home). In this release, host key verification was not yet enforced; strict verification is documented under **[Unreleased]** ([#79](https://github.com/askdba/mysql-mcp-server/issues/79)).

### Changed

- **`getEnvBool`**: treats **`true`**, **`yes`**, **`on`**, **`y`** as true (case-insensitive), not only **`1`**, for **`MYSQL_MCP_*`** and related flags.
- **Full REST vs sidecar**: when **`MYSQL_MCP_HTTP=1`**, **`MetricsHTTP`** is cleared so the metrics-only listener does not run alongside the full HTTP API.

---

## [1.7.0-rc.2] - 2026-03-30

Second release candidate: integration-test identity, HTTP token UX, and compose port safety.

### Added

- **`mcpuser` / `mcppass00`** for integration tests and docs: `tests/sql/mcp_test_user.sql`, `mcp_test_user_sakila.sql`, mounted in `docker-compose.test.yml`; CI applies `mcp_test_user.sql` after `init.sql`; Makefile / QA / README / Sakila step docs use this DSN instead of `root` + `testpass`.
- **`make test-sakila-local`**: run Sakila tests against a local MySQL when `MYSQL_TEST_DSN` or `MYSQL_SAKILA_DSN` is set (no Docker).

### Changed

- **HTTP REST**: when **`MYSQL_MCP_HTTP`** is set, **`GET /status`** (token dashboard) is **on by default**; set **`MYSQL_MCP_TOKEN_CARD=0`** to disable. Homebrew / GoReleaser caveats updated.
- **Docker Compose (MySQL 8.0)**: host port **13306** → container **3306** so host **3306** can stay free for a local MySQL; Makefile and README DSN examples updated.

### Fixed

- Sakila integration test: clearer timeout / ping error hint (ports, Error 1045).

---

## [1.7.0-rc.1] - 2026-03-29

Release candidate: performance, observability, metadata discovery, and HTTP token dashboard. See [docs/releasing.md](docs/releasing.md) for tagging; GA will be **v1.7.0** after validation.

### Added

- **Live token monitoring (HTTP mode)** ([#96](https://github.com/askdba/mysql-mcp-server/pull/96), closes [#83](https://github.com/askdba/mysql-mcp-server/issues/83)): in-process `TokenMetrics`, **`GET /api/metrics/tokens`**, optional **`GET /status`** dashboard (auto-refresh). Enable with **`MYSQL_MCP_TOKEN_CARD=1`**, **`--token-card`**, or **`features.token_card: true`** in config.
- **Query performance & payload controls** ([#101](https://github.com/askdba/mysql-mcp-server/pull/101), [#100](https://github.com/askdba/mysql-mcp-server/issues/100)): env aliases **`MYSQL_POOL_SIZE`** (→ max open conns) and **`MYSQL_QUERY_TIMEOUT`** (milliseconds; `MYSQL_QUERY_TIMEOUT_SECONDS` wins when set); server-side **`LIMIT`** injection for `SELECT`/`UNION`; **`truncated`** and **`warning`** on `run_query` results (`SELECT *` hint); improved `run_query` tool description.
- **EXPLAIN guidance** ([#98](https://github.com/askdba/mysql-mcp-server/pull/98), [#82](https://github.com/askdba/mysql-mcp-server/issues/82)): `explain_query` returns **`warnings`** from plan analysis; pre-allocated result slices; clamp negative **`maxRows`** before allocation (Codex review).
- **Status & variables** ([#97](https://github.com/askdba/mysql-mcp-server/issues/97)): **`list_status`** / **`list_variables`** use **`performance_schema.global_status`** / **`global_variables`** when possible, with **`SHOW GLOBAL STATUS` / `SHOW GLOBAL VARIABLES`** fallback.
- **`test-steps.md`**: Sakila multi-version matrix with **`wait_docker_healthy`** / **`wait_mysqladmin_ping`** helpers and `<repo-root>` placeholder.

### Changed

- **`explain_query`**: safer “unused index” warning when access `type` is unknown (avoids false positives on `<NIL>`).
- **`.gitignore`**: root-only **`/mysql-mcp-server`** and **`/.worktrees/`** so **`cmd/mysql-mcp-server`** is not ignored.

### Fixed

- **`truncated`**: set only when a row exists beyond the row cap (not when the result size exactly equals the limit).

### Documentation

- README: env vars, REST endpoints, performance tuning, Sakila test references aligned with this RC.
- Comprehensive MySQL Query Optimization Guide: [`docs/mysql_query_optimization_comprehensive.md`](docs/mysql_query_optimization_comprehensive.md) ([#92](https://github.com/askdba/mysql-mcp-server/pull/92)).

### CI

- MariaDB job result included in QA pipeline summary output ([#92](https://github.com/askdba/mysql-mcp-server/pull/92)).

---

## v1.6.0 - 2026-02-10
### Added
- `--silent` / `-s`: suppress INFO and WARN logs (ERROR still printed to stderr). Useful for production or when running under a process manager.
- `--daemon` / `-d`: run in background (fork and detach; intended for HTTP mode on Unix). On Windows, use a service manager instead.
- Example systemd unit in `contrib/systemd/mysql-mcp-server.service` and launchd plist in `contrib/launchd/com.askdba.mysql-mcp-server.plist`.
- Documentation: [docs/silent-and-daemon.md](docs/silent-and-daemon.md). Examples: [examples/config.yaml](examples/config.yaml) comments and [examples/production-usage.md](examples/production-usage.md).
- SSH tunneling (bastion host) support: connect to MySQL via `ssh_host`, `ssh_user`, `ssh_key_path`, and optional `ssh_port` (config file or `MYSQL_SSH_*` env vars). `key_path` supports `~` and `~/path` (expanded to user home). Bastion host key verification is not performed.
- Native support for MariaDB 10.x and 11.x.
- Automatic server type detection (`mysql` vs `mariadb`) in `server_info` tool.
- MariaDB 11.4 integration test target in `Makefile` and `docker-compose.test.yml`.
- Robust Unicode support for MariaDB initialization scripts.

### Changed
- Refactored schema discovery tools (`list_databases`, `list_tables`, `describe_table`) to use `information_schema` for better compatibility and performance.
- Upgraded `list_tables` to include engine type, estimated row count, and comments.
- Upgraded `describe_table` to return comprehensive column details including collation and comments.

### Fixed
- Daemon mode now requires HTTP mode: `--daemon` without HTTP enabled exits with a clear error instead of forking an idle stdio process.

## v1.5.0 - 2026-01-17

### Added
- Architecture documentation with diagrams to explain system flows.
- GitHub issue and PR templates to standardize contributions.
- SSL/TLS configuration examples in config templates.

### Changed
- Global SSL settings now apply to JSON connection definitions.
- SSL "preferred" maps to "skip-verify" for Go MySQL driver compatibility.
- Updated dependencies, including the MCP SDK.

### Fixed
- Linter warnings in tests (errorlint, staticcheck).
- Error comparisons in tests now use errors.Is for wrapped errors.
- SQL validator empty branch removed.

### Tests
- Improved unit test coverage for internal MySQL client.
- Improved HTTP handler coverage for cmd/mysql-mcp-server.

### Documentation
- Updated README for SSL/TLS behavior and configuration.
- Corrected config file search paths in architecture docs.

### Dependencies
- github.com/modelcontextprotocol/go-sdk v1.1.0 -> v1.2.0
- github.com/dlclark/regexp2 v1.10.0 -> v1.11.5
- github.com/google/jsonschema-go v0.3.0 -> v0.4.2
- golang.org/x/oauth2 v0.30.0 -> v0.34.0
