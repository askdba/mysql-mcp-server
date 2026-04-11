# Changelog

All notable changes to this project will be documented in this file.

The format is based on "Keep a Changelog" and this project follows
Semantic Versioning.

## [Unreleased]

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
... rest of the file ...
