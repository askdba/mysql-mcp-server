# RFC 0001: MySQL-native MCP: components and a shared SQL tool pack

| Field | Value |
|---|---|
| **Status** | Draft. Brainstorm decisions recorded (§3, round 2 in §3a); self-reviewed 2026-10-18; updated 2026-10-23 |
| **Date** | 2026-10-18 |
| **Targets** | Guard component: MySQL 8.4 LTS, 9.7 LTS, 26.7, 26.10. In-server MCP: **26.10+**. SQL tool pack: 8.0 → 26.10 |
| **Unchanged** | Go server keeps full support for MySQL 8.0, MariaDB and managed cloud |
| **Code home** | Components and SQL tool pack: [`askdba/myvector`](https://github.com/askdba/myvector) repo (§7). Go-side changes: this repo |
| **Related** | [`docs/architecture.md`](../architecture.md), [`docs/oracle_mcp_comparison.md`](../oracle_mcp_comparison.md), myvector `docs/BUILD_MODES.md`, myvector `docs/COMPONENT_MIGRATION_PLAN.md` |

---

## 1. Summary

`mysql-mcp-server` is a standalone Go binary that reaches MySQL as an ordinary client. This RFC makes MCP a **MySQL-native** capability. It has three parts, which ship independently:

1. **SQL tool pack:** a `mysql_mcp` schema of stored procedures and views that implement the tool logic once. It runs on everything from 8.0 to 26.10, managed cloud included. The Go server and both components call it.
2. **`component_mcp_guard` (8.4+):** a native component that enforces MCP policy **inside the server** (statement abort through event tracking). It also records MCP activity in `performance_schema`, status variables and the audit stream.
3. **`component_mcp` (26.10+):** an MCP endpoint served by `mysqld` itself, with **no sidecar**. It runs every tool call under the caller's MySQL account, by impersonating it through command services. It runs its own listener built on the MySQL Router HTTP libraries that Oracle's `component_mysql_rest_service` (MRS) also uses (§3a).

**Privileges are the first line of defense.** A `GRANT SELECT`-only account is already enforced by the server today. The guard component adds the controls that grants cannot express (§5.2); it is not a replacement for least-privilege accounts.

The Go server remains the primary product. It is the only option for 8.0, MariaDB, RDS, Aurora, Cloud SQL, Azure and HeatWave, none of which can load third-party components.

Embedding the Go binary in `mysqld` is **rejected** (§6.3).

---

## 2. Background

### 2.1 Current state (v1.8.0, when this RFC was written)

- A Go 1.26 binary using `modelcontextprotocol/go-sdk` v1.8.0. MCP runs over stdio, with optional REST (`MYSQL_MCP_HTTP=1`) and a metrics sidecar.
- About 30 tools:
  - Core: `list_databases`, `list_tables`, `describe_table`, `run_query`, `ping`, `server_info`, plus the connection tools.
  - Vector (9.0+).
  - Extended: `explain_query`, `list_indexes`, `schema_diff`, `search_schema`, …
  - Diagnostics: `process_list`, `kill_query`, `slow_query_log`, `read_audit_log`.
- Safety is **client-side only**:
  - SQL validator and parser (`internal/util/`).
  - `MYSQL_MCP_ALLOWED_DATABASES`, `MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS`.
  - `MYSQL_MCP_MASK_COLUMNS`, `MYSQL_MCP_AUDIT_LOG`.
- CI covers MySQL 8.0, 8.4, 9.7 and 26.7, and MariaDB 11.4 and 12.3.
- **Since then** ([v1.9.0-rc.1](https://github.com/askdba/mysql-mcp-server/releases/tag/v1.9.0-rc.1)): MCP over Streamable HTTP (`MYSQL_MCP_TRANSPORT=http`, Phase 0b), CI on MySQL 26.10 instead of 26.7, and end-to-end smoke tests on every supported server.

### 2.2 Version landscape (updated 2026-10-23)

| Line | Status | Latest upstream tag | Role in this RFC |
|---|---|---|---|
| 8.0 | Past upstream EOL. **We keep supporting it** in the Go server | n/a | SQL tool pack only (no event-tracking services) |
| 8.4 | LTS | `mysql-8.4.11` | Guard component |
| 9.7 | **Current LTS** (GA 2026-04-21); last sequential version number | `mysql-9.7.2` | Guard component (primary) |
| 26.7 | Innovation; first CalVer (`YY.M.P`) release (26.7.0 on 2026-07-28, 26.7.1 on 2026-08-18) | `mysql-26.7.0` | Guard component |
| 26.10 | Innovation, **GA 2026-10-20** (26.10.0; EA was 2026-09-18). Official image `mysql:26.10` (amd64 + arm64) since 2026-10-21 | `mysql-26.10.0` | Guard component; **baseline for in-server MCP** |
| MariaDB 11.4 / 12.3 | Plugin API only | n/a | SQL tool pack (adapted, best effort) |
| Managed cloud | No custom components | n/a | SQL tool pack (where `CREATE ROUTINE` is allowed) + Go server |

Components have no stable binary ABI across releases, so every artifact is built for one server release (§7.1). Separately, upgrades must pass through each LTS line (8.4 → 9.7 → 26.x). Users therefore install a new component build at every hop.

**Platforms:** the components are Linux only (`linux/amd64`, `linux/arm64`), the same as MyVector. On macOS and Windows, use the Go server.

### 2.3 Why 26.10 matters

The 26.10.0 release notes (EA 2026-09-18; the [GA notes of 2026-10-20](https://dev.mysql.com/doc/relnotes/mysql/26.10/en/news-26-10-0.html) keep everything below) add exactly what an in-server MCP endpoint needs:

- **`component_mysql_rest_service` (MRS as a component).** It serves HTTPS from inside `mysqld`, with no Router process. It comes with:
  - System variables: `mysql_rest_service.http_port` (default 33443), `ssl_cert` / `ssl_key` / `ssl_cipher`, `io_worker_threads`, `http_max_connections`, `http_max_request_body_size` / `http_max_response_body_size`.
  - Status: `mysql_rest_service.state` (`STOPPED` … `READY` / `ERROR`).
  - Control functions: `mysql_rest_service_start()` / `stop()` / `restart()`, which require `MYSQL_REST_SERVICE_ADMIN`.

  This means Oracle now ships and supports the "HTTP listener inside `mysqld`" pattern. Our draft had treated that as the riskiest and most speculative part.
- **`MYSQL_COMMAND_PASSWORD`** (command services): **password-authenticated**, server-local Classic Protocol sessions that go through the normal authentication path.
  - Per-user execution itself does **not** need this. Since 8.4, `MYSQL_COMMAND_USER_NAME` / `MYSQL_COMMAND_HOST_NAME` set the account into the session's security context (verified in §2.4).
  - What 26.10 adds is the *choice* to have the server verify credentials, instead of the component impersonating an account it has already authenticated.
- **`account_locked`**, readable via `mysql_account_database_security_context_lookup`. Impersonation skips the normal login checks, so the endpoint must check lock state itself. Before 26.10 there was no component API for that.
- **Command-service fixes:**
  - Prepared `CALL` returns OUT parameters to component consumers. This is nice to have; the tool pack returns result sets.
  - The DOM consumer returns correct `MYSQL_FIELD` metadata.
- **Build-relevant changes:**
  - OpenSSL ≥ 1.1.1 required, OpenSSL 4.0 supported.
  - `psi_thread_v7` (v4–v6 deprecated).
  - GCC 16.1 fixes.

26.7 had already added `mysql_command_session_state` and `field_info("mysql_type")`, which give correct JSON typing of results.

### 2.4 Verified on the local source trees

The MyVector project already builds a GPLv2 component for 8.4, 9.7 and 26.7. Its source trees on the build host (`~/myvector/mysql-server-mysql-{8.4.8,9.7.0,26.7.0}`) were checked directly:

| Service header (`include/mysql/components/services/`) | 8.4.8 | 9.7.0 | 26.7.0 |
|---|---|---|---|
| `event_tracking_{query,parse,table_access,connection,command,general,authentication,…}_service.h` | ✅ | ✅ | ✅ |
| `mysql_command_services.h`, `mysql_command_consumer.h` | ✅ | ✅ | ✅ |
| `mysql_query_attributes.h` | ✅ | ✅ | ✅ |
| `pfs_plugin_table_service.h` | ✅ | ✅ | ✅ |
| `component_sys_var_service.h`, `component_status_var_service.h`, `dynamic_privilege.h` | ✅ | ✅ | ✅ |
| `mysql_command_session_state.h` | ❌ | ❌ | ✅ |
| `MYSQL_COMMAND_USER_NAME` / `HOST_NAME` (impersonate an account in a `"local"` session) | ✅ | ✅ | ✅ |
| `MYSQL_COMMAND_CLIENT_FLAGS` | ❌ | ❌ | ✅ |
| `MYSQL_COMMAND_PASSWORD` | ❌ | ❌ | ❌ (26.10) |
| `mysql_event_tracking_query_data.sql_command` (statement class without parsing) | ✅ | ✅ | ✅ |
| `components/audit_api_message_emit` | ✅ | ✅ | ✅ |

- **Abort semantics confirmed.** In `sql/sql_audit.h`, every `mysql_event_tracking_*_notify` returns "0 continue server flow, otherwise abort". A consumer returning `true` from `notify` therefore stops the statement, so **server-side enforcement is available from 8.4**.
- **Reference consumers** exist in `components/test/event_tracking_test/` and `components/test/test_event_tracking_consumer.cc`.

---

## 3. Brainstorm decisions (2026-10-18)

| Question | Decision |
|---|---|
| Primary goals | **All of:** zero-sidecar MCP in `mysqld`; server-side guardrails; native observability; positioning against Oracle's MCP |
| 26.10 | **Baseline for in-server MCP.** The guard component still covers 8.4, 9.7 and 26.7 |
| In-server transport | **Explore both** (built on MRS vs. our own listener); a spike decides. *Superseded in §3a: reuse the Router HTTP libraries* |
| SQL tool pack | **Yes, as the shared foundation** for the Go server, the components and MRS. *MRS is no longer a consumer (§3a): it has no extension point, and publishing `mysql_mcp.*` through MRS REST was rejected* |
| Component code home | **Inside the myvector repo** (reuses its GPLv2 component build, CI and Docker infrastructure) |
| SQL tool pack home | **myvector repo** too; the Go server vendors it; `sql/mcp/` is Apache-2.0 (§7.2) |

## 3a. Brainstorm round 2 (2026-10-23)

Facts established against the `mysql-26.10.0` source and the Go driver:

- **MRS is not an extension point.** `component_mysql_rest_service` provides no services: its `BEGIN_COMPONENT_PROVIDES` block is empty ([`mrs_component.cc`](https://github.com/mysql/mysql-server/blob/mysql-26.10.0/components/mysql_rest_service/mrs_component.cc)). Other components cannot register handlers on its listener.
- **What MRS does have is reusable code.** It statically links MySQL Router's `http_server`, TLS (`router_openssl`) and `http_auth_realm` / `http_auth_backend` libraries. These are built in-tree with `ROUTER_BUILD_MRS_DEPENDENCIES_ONLY` ([`CMakeLists.txt`](https://github.com/mysql/mysql-server/blob/mysql-26.10.0/components/mysql_rest_service/CMakeLists.txt)). It is also a working reference for impersonated command-service sessions: `mysql_security_context_factory`, `mysql_account_database_security_context_lookup` and `mysql_security_context_options`.
- **The Go driver has no query attributes.** `go-sql-driver/mysql` v1.10.1 lacks `CLIENT_QUERY_ATTRIBUTES`. The upstream implementation ([PR #1784](https://github.com/go-sql-driver/mysql/pull/1784)) was closed unmerged while new features are on hold; [#1785](https://github.com/go-sql-driver/mysql/issues/1785) is open. The driver does send per-connection **connection attributes** (`connectionAttributes` DSN parameter). Components can read them with `mysql_connection_attributes_iterator`, which exists in 8.4, 9.7 and 26.7.
- **`table_access` events carry the table.** `mysql_event_tracking_table_access_data` has `table_database`, `table_name` and the access subclass (read, insert, update or delete). The schema allow-list can be enforced exactly, without parsing statements.

| Question | Decision |
|---|---|
| In-server transport (was: spike T1 vs T2) | **Reuse the Router HTTP libraries** that MRS links: build `component_mcp` in-tree and link `http_server`, TLS and HTTP-auth objects. Oracle-maintained, proven code instead of our own internet-facing parser. Cost: an in-tree build per server release (§7.1) |
| Labeling MCP traffic (was: query attributes) | **Connection attributes + SQL comments.** Per connection: `program_name=mysql-mcp-server` and the transport, set through the driver's `connectionAttributes`. Per statement: a `/* mcp:tool=<name> req=<id> */` comment prefix. Identity stays privilege-based (`MCP_USER`); labels are informational and cannot widen access. No driver change |
| Schema allow-list enforcement | **`event_tracking_table_access`** (exact table and database per access) |
| Guard scope | **Our MCP server only.** The guard is designed and documented for `mysql-mcp-server` (its tool names, `mysql_mcp` routines and labels), not as a generic AI-agent guard |
| Sequencing | **Update this RFC and start Phase 0 now**, in parallel with the v1.9.0 release candidate |

---

## 4. Goals and non-goals

**Goals**

- G1 Zero-sidecar: `INSTALL COMPONENT` + point an MCP client at `mysqld` (26.10+).
- G2 Server-side guardrails that a client-side bypass cannot widen (8.4+).
- G3 Native observability: `performance_schema`, status variables, error log, audit (8.4+).
- G4 One implementation of tool logic, shared by every delivery path (8.0+).
- G5 Positioning: the deepest MySQL-native MCP (tool depth + guardrails), not just "another transport".

**Non-goals**

- Dropping the Go server, or its 8.0, MariaDB or cloud support.
- Porting client-only features into components: SSH tunnels, multi-DSN / `add_connection`, tiktoken token metrics, daemon mode, REST mode.
- Write tools beyond issue #103.
- **Known parity gap, not a goal for v1:** column masking (`MYSQL_MCP_MASK_COLUMNS`) and token metrics stay Go-only. Until masking is ported, `component_mcp` must refuse to start when `mcp.mask_columns` is set. It must never silently return unmasked data.

---

## 5. Architecture

```
            ┌───────────── SQL tool pack: schema `mysql_mcp` ─────────────┐
            │ procs/views: list_tables, describe_table, indexes, sizes,   │
            │ search_schema, process_list, … · versioned · 8.0 → 26.10    │
            └──────▲─────────────────────▲──────────────────────▲────────┘
                   │ CALL                │ CALL (command svc)   │ CALL
  AI client ─stdio/HTTP─▶ Go server      │                      │
  (any MySQL, cloud, 8.0, MariaDB)       │                      │
                   │ conn attrs + /*mcp*/│                      │
                   ▼                     │                      │
  ┌──────────────────────────────── mysqld (8.4+) ───────────────────────────────┐
  │ component_mcp_guard                         component_mcp (26.10+)           │
  │  • event_tracking_query/table_access:       • MCP Streamable HTTP endpoint   │
  │    abort disallowed stmts for MCP sessions    (Router HTTP libs, own port)   │
  │  • mcp.* sysvars · mcp.* status vars        • auth → account; SQL runs as    │
  │  • P_S.mcp_tool_calls · MCP_USER/MCP_ADMIN    that account (impersonated     │
  │  • audit_api_message_emit on deny             session) → mysql_mcp.* procs   │
  └──────────────────────────────────────────────────────────────────────────────┘
```

### 5.1 SQL tool pack (`mysql_mcp` schema)

- Stored procedures and views for every tool that is pure SQL over `information_schema` / `performance_schema` / `sys`:
  - `list_databases`, `list_tables`, `describe_table`, `show_create_table`, `list_indexes`
  - `list_views`, `list_triggers`, `list_procedures`, `list_functions`, `list_partitions`
  - `database_size`, `table_size`, `foreign_keys`, `search_schema`
  - `list_status`, `list_variables`, `process_list`
  - `vector_info` (9.0+)
- **Not in SQL:**
  - `run_query`, which needs validation and row limits and stays in the caller.
  - `explain_query`. A procedure would have to `PREPARE` caller-supplied SQL, which turns the pack into a generic dynamic-SQL executor that bypasses the caller's validator. The caller runs `EXPLAIN` itself after validation.
  - `schema_diff` across servers, and the connection tools.
- **Identifier arguments** (schema, table names) are validated against `information_schema` before any dynamic SQL is built. Free-form SQL is never accepted as an argument.
- `SQL SECURITY INVOKER` everywhere, so tools see exactly what the caller's grants allow.
- **Privileges:**
  - Installing needs `CREATE ROUTINE`/`CREATE VIEW` on `mysql_mcp`, a one-time DBA step.
  - MCP accounts need only `EXECUTE` on `mysql_mcp` plus their normal data grants.
- A `mysql_mcp.version()` function lets callers check compatibility.
- Version variants are chosen at install time (e.g. no `VECTOR` on 8.x).
- **Go server:**
  - A new `mysql-mcp-server install-schema` subcommand installs the vendored pack.
  - At connect time the server detects `mysql_mcp.version()`. If the pack is present it calls it; otherwise it uses the current embedded SQL. Behavior is identical either way, which is enforced by tests.
- Risk: some managed services restrict `CREATE ROUTINE` or `DEFINER`. The fallback to embedded SQL covers them.

### 5.2 `component_mcp_guard` (8.4, 9.7, 26.7, 26.10)

- **MCP session identity:**
  - The **`MCP_USER` dynamic privilege** on the account is authoritative. It is checked once, at `event_tracking_connection` connect/change-user time, and cached per connection id. It is not checked on every statement, so non-MCP sessions pay one lookup per statement against a connection-id set.
  - Labels are informational and feed observability (§3a). The connection attribute `program_name=mysql-mcp-server` marks the connection, read with `mysql_connection_attributes_iterator` at connect time. A `/* mcp:tool=<name> req=<id> */` comment prefix on each statement gives the tool and request id, visible in the `event_tracking_query` text.
  - A client cannot opt out by dropping the labels: enforcement keys on the privilege.
- **Scope:** built for `mysql-mcp-server` only (§3a). Policy and observability assume its tools, its `mysql_mcp` routines and its labels.
- **What it adds over GRANTs.** Grants stay the primary control: MCP accounts should be `SELECT`-only. The guard covers what grants cannot express:
  - `mcp.enabled`: a kill switch for all MCP sessions at once, without touching accounts.
  - `mcp.read_only` (default `ON`): denies by `sql_command` class even when the account *has* write grants. This is the case when a DBA reuses an existing account, or grants `EXECUTE` on procedures that write.
  - `mcp.allowed_schemas` / `mcp.deny_system_schemas` (default `ON`): useful when the account has global `SELECT`, which is common for diagnostics accounts.
  - `mcp.max_execution_time_ms`: a floor that the session cannot raise with `SET`.
  - Observability (below), which no grant provides.
- **Enforcement** happens in `event_tracking_query` (using `sql_command`) and `_table_access`, before execution.
- **Known limits of `sql_command` classification**, each covered by a test:
  - `SELECT … INTO OUTFILE` (denied when `mcp.read_only`; also needs `FILE`).
  - Locking reads (`FOR UPDATE` / `FOR SHARE`).
  - `SELECT` of a stored function that writes: `table_access` events catch the write.
  - `CALL` of procedures (denied by default unless the routine is in `mysql_mcp`).
  - `SET` / `DO`.
- **Observability:**
  - `performance_schema.mcp_tool_calls`: a ring buffer of tool, request id, user, digest, rows, latency and outcome.
  - Status variables `mcp.statements_total`, `mcp.statements_denied`, …
  - Denials go to the error log and to `audit_api_message_emit`.
- **Administration:** `mcp.*` changes require `SYSTEM_VARIABLES_ADMIN` + `MCP_ADMIN`, and persist with `SET PERSIST` or `INSTALL COMPONENT … SET`.
- **Go-side changes:**
  - Set `connectionAttributes` (`program_name:mysql-mcp-server,mcp_transport:<stdio|http>`) on every DSN, and prefix statements with the `/* mcp:tool=… req=… */` comment.
  - Report the component in `server_info`.
  - Map the denial error code to a clean MCP tool error.

### 5.3 `component_mcp` (26.10+)

- An MCP **Streamable HTTP** (JSON-RPC 2.0) endpoint inside `mysqld`.
- **Request path (all tools except `run_query`):** authenticate the MCP caller (bearer token) → resolve the mapped account → check `account_locked` (26.10) → open a `"local"` `mysql_command_factory` session with `MYSQL_COMMAND_USER_NAME` / `HOST_NAME` (impersonation; no stored passwords) → `CALL mysql_mcp.<tool>(…)` → map result metadata (`mysql_type`, session state) to MCP JSON.
- **`run_query` path (separate; there is no `mysql_mcp` procedure for it, see §5.1).** It runs the caller's SQL directly on the impersonated session, after the same authentication and account steps:
  1. **Session setup** before the statement:
     - `SET SESSION transaction_read_only = ON`
     - `max_execution_time` = `mcp.max_execution_time_ms`
     - `sql_select_limit` = `mcp.max_rows + 1`. The server stops producing rows past the cap, and the extra row reveals truncation.
  2. **Single statement only.** The session is opened without `CLIENT_MULTI_STATEMENTS` (`MYSQL_COMMAND_CLIENT_FLAGS`, 26.7+), so `;`-chained statements are a parse error.
  3. **Validation by the server, not by string matching.** `component_mcp_guard` is required (the endpoint refuses to start without it). Its `event_tracking_query` hook rejects any `sql_command` other than `SELECT`, `SHOW`, `DESCRIBE`/`EXPLAIN` and `WITH … SELECT`. Its `event_tracking_table_access` hook rejects tables outside `mcp.allowed_schemas`, and system schemas unless allowed. This is the server-side equivalent of the Go server's validator (`cmd/mysql-mcp-server/tools.go`, `toolRunQuery`) and its referenced-schema allow-list (`access.go`). It also covers views, stored functions and subqueries, which string-level checks miss.
  4. **Row limit on the consumer side as well.** The command-service row callback stops after `mcp.max_rows` and marks the result `truncated`, matching the Go output shape (`columns`, `rows`, `truncated`).
  5. **Masking:** until column masking is ported, `run_query` is disabled whenever `mcp.mask_columns` is set (see §4). The endpoint never returns unmasked data.
  6. **Audit:** every `run_query`, allowed or denied, is recorded through the guard's `performance_schema.mcp_tool_calls` and `audit_api_message_emit`.
  7. **Tests:** the MTR suite gets a `run_query` matrix covering writes, multi-statements, `INTO OUTFILE`, locking reads, cross-schema views, stored functions that write, and row truncation. Each case must match the Go server's verdict for the same input.
- `MYSQL_COMMAND_PASSWORD` is an **optional** mode for deployments that want the server to verify a password per request.
- **Why 26.10, when impersonation works from 8.4:**
  - MRS proves the in-server HTTPS stack, and its Router HTTP libraries are the ones `component_mcp` links (§3a).
  - The `account_locked` check closes the gap that impersonation skips the login checks.
  - The command-service metadata fixes.
  - 26.7 has neither `component_mysql_rest_service` nor Router's `ROUTER_BUILD_MRS_DEPENDENCIES_ONLY` build mode (both first appear in the 26.10 source), so it could not reuse the Router HTTP libraries, and it has no `account_locked` check.
- `run_query` reuses the guard component's policy, so `component_mcp` hard-requires `component_mcp_guard` (above).
- Off by default (`mcp.http_enabled=OFF`). Binds to loopback by default; TLS is mandatory for anything else.
- **Transport (decided 2026-10-23, §3a).** `component_mcp` links the MySQL Router HTTP stack that `component_mysql_rest_service` uses: `http_server`, `router_openssl`, `http_auth_realm` and `http_auth_backend`. It runs its own listener (`mcp.http_port`), separate from MRS's, and handles MCP JSON-RPC itself with the MySQL-bundled rapidjson.
  - **Rejected: publishing `mysql_mcp.*` through MRS REST.** MRS has no extension point, so an MCP translation layer would have to run outside `mysqld`. That brings the sidecar back.
  - **Rejected: our own embedded HTTP library.** It would be an internet-facing parser we maintain inside `mysqld`.
  - **Phase 4 is now a build spike, not a choice:** build `component_mcp` in-tree against `mysql-26.10.0` with `ROUTER_BUILD_MRS_DEPENDENCIES_ONLY`, serve one MCP tool over the Router HTTP server, and measure build time, `.so` size and the listener's thread budget. Also check whether `http_auth_backend` can carry bearer tokens or whether the endpoint checks tokens itself.

---

## 6. Options rejected

### 6.1 Go server unchanged, "component-aware" only

Folded into §5.2's Go-side changes. On its own it delivers none of G1–G3.

### 6.1a Remote MCP from the Go server (Streamable HTTP): complementary, not rejected

- Today the Go server offers MCP over stdio only; its HTTP mode is REST, not MCP. The `go-sdk` already supports Streamable HTTP.
- Adding it gives remote MCP on **every** MySQL version and on managed cloud, for a fraction of `component_mcp`'s cost.
- It does not meet G1 (it is still a sidecar), so it does not replace §5.3. But it should land **first**, as a separate issue in this repo (#140):
  - it covers most "remote MCP" demand now;
  - it gives `component_mcp` a reference implementation to match for conformance and parity tests;
  - it lowers the stakes of the §10 go/no-go.

### 6.2 Component that spawns and supervises the Go binary

Forking a large multi-threaded `mysqld` is risky, and systemd or containers already do supervision better. It delivers nothing that §5 doesn't.

### 6.3 Go server loaded into `mysqld` (`-buildmode=c-shared`)

- **Not unloadable:** Go `c-shared` libraries cannot be safely `dlclose`d (golang/go#11100), so `UNINSTALL COMPONENT` would be unsafe.
- **Signal conflicts:** the Go runtime's handlers (`SIGSEGV`, `SIGPROF`, and `SIGURG` preemption) collide with `mysqld`'s.
- **One Go runtime per process.**
- **Unaccounted memory:** the GC heap is invisible to `performance_schema` memory accounting.
- **No real code reuse:** the code would still need rewriting against command services through cgo.
- **Rejected.**

---

## 7. What is required

### 7.1 Build and CI: reuse the MyVector pipeline

MyVector already proves the build model on this host:

- **No full server build.** The server tree is **configured only**, to generate headers (`cmake .. -DWITH_UNIT_TESTS=OFF -DWITH_ROUTER=OFF -DWITH_RAPID=OFF -DWITH_NDB=OFF -DWITH_GROUP_REPLICATION=OFF …` into `bld-<arch>`).
- Each component is a **standalone CMake project** with `MYSQL_SOURCE_DIR` / `MYSQL_BUILD_DIR`. This corrects the earlier draft's "must build in-tree, 20–40 min per cell". It applies to `component_mcp_guard` and the SQL tool pack.
- **Exception: `component_mcp` builds in-tree** (§3a). It links Router object libraries that exist only inside a configured and partially built server tree (`ROUTER_BUILD_MRS_DEPENDENCIES_ONLY`), so its CI job builds those targets per 26.x release. Expect a longer build than the guard's; measure it in Phase 4.
- **Build environment:** an `oraclelinux:9` container, `gcc-toolset-14`, `-static-libstdc++ -static-libgcc`, and the MySQL devel RPMs from the CDN (main path, falling back to the archive).
  - Reuse `scripts/build-component-{8.4,9.7,26.7}-docker.sh`.
  - Add `build-component-26.10-docker.sh` in Phase 0 (the `mysql-26.10.0` tag exists since GA).
- **Multi-arch** via `DOCKER_PLATFORM` (`linux/amd64`, `linux/arm64`), with a per-architecture `bld-<arch>` dir (already handled).
- **Packaging:** `Dockerfile.component` pattern (`FROM mysql:<ver>`; `.so` + JSON manifest into the plugin dir; install SQL in `docker-entrypoint-initdb.d`).
- **Release gate:** extend `scripts/pre-release-test.sh` and `.github/workflows/pre-release-gate.yml`.

**New work in myvector:**

| Item | Detail |
|---|---|
| Layout | `src/mcp/guard/`, `src/mcp/endpoint/`, `sql/mcp/` (tool pack), `mysql-test/suite/mcp/` |
| CMake | New targets `component_mcp_guard`, `component_mcp`, built from the same top-level `CMakeLists.txt` and selected by option (`-DMYVECTOR_BUILD_MCP=ON`). They stay separate `.so` files from `myvector_component` so each installs independently |
| Manifests | `mcp_guard.json`, `mcp.json` (name, description, license, version) |
| Docker | Image tags such as `ghcr.io/askdba/myvector:mysql9.7-mcp`, or a separate `mysql-mcp` image family (decide at release) |
| Source trees | Refresh to the latest tags: 8.4.8 → **8.4.11**, 9.7.0 → **9.7.2**, and add **26.10.0** (tagged at GA, 2026-10-20) |
| Version parsing | CalVer (`YY.M.P`, `MYSQL_PREVIOUS_LTS_VERSION_ID`) in component code and in the Go server's version gates |
| ABI policy | One artifact per (server patch release × arch). Rebuild on every patch release; never promise cross-patch compatibility |

### 7.2 Licensing

- myvector is **GPLv2**, and **askdba owns its copyright**, as it does this repo's. The components are GPLv2, which is fine.
- **SQL tool pack:** the Go server (Apache-2.0) vendors it and embeds it in the binary. **The tool-pack files must therefore carry a permissive license**, e.g. `sql/mcp/LICENSE` = Apache-2.0 or MIT, with an SPDX header on each file.
  - This works because the SQL scripts are separate files, are not linked into the component, and are authored fresh for this project.
  - **No external sign-off is needed.** askdba holds the copyright, so it can license new files in its own repo as it chooses. The only requirement: the tool pack is new code, so no third-party GPL contributions may be copied into `sql/mcp/`.
  - Housekeeping: add a per-directory note to myvector's `NOTICE` and `LICENSE` section ("`sql/mcp/` is licensed under Apache-2.0"). Also check that `NOTICE`'s copyright line ("p3io.in") matches current ownership.
- No Go or C++ implementation code is shared between the Go server and the components. The one shared artifact is the Apache-2.0 SQL tool pack (`sql/mcp/`), which the Go server vendors and `component_mcp` calls through command services. Otherwise they interact only through the wire (connection attributes, statement comment labels, error codes, `CALL mysql_mcp.*`).

### 7.3 Testing

- **MTR** suite `mysql-test/suite/mcp/`: install/uninstall, sysvar persistence, deny paths per statement class, `P_S` contents, privilege checks, and the endpoint's JSON-RPC conformance (26.10).
- **Tool-pack parity:** this repo's Testcontainers matrix (8.0, 8.4, 9.7, 26.7, MariaDB) runs every tool both via the embedded SQL and via `mysql_mcp.*`, and diffs the outputs.
- **ASAN/UBSAN** component builds in CI.
- **Fuzzing** of HTTP and JSON-RPC parsing, blocking for any `component_mcp` release.
- **Overhead:** sysbench OLTP read-only with the guard installed but idle (non-MCP sessions) must stay under 1% p99 regression.

### 7.4 Security

- Default deny (`mcp.read_only=ON`, `mcp.deny_system_schemas=ON`). MCP identity comes from **privilege**, never only from an attribute.
- **Endpoint:**
  - Off by default and loopback by default.
  - TLS required for non-loopback binds; reuse the server's or MRS's TLS configuration.
  - Honors `account_locked`.
  - Rate limits per account.
  - No passwords in MCP payloads, and none stored by default. An OAuth 2.1 bearer token or a token table maps to an account, which is impersonated (§5.3).
  - Token hashes live in a server table readable only by `MCP_ADMIN`, or in the keyring component.
  - Impersonation makes the component fully trusted. A token→account mapping must never resolve to an account holding `SUPER`, `SYSTEM_USER` or `MCP_ADMIN`; the component enforces this at mapping time and again on every request.

### 7.5 Documentation

- myvector: `docs/MCP_COMPONENTS.md` (install, sysvars, `P_S`, version matrix, upgrade = redownload or rebuild per server release).
- This repo: README section "MySQL-native mode", `install-schema` docs, and an `architecture.md` diagram update.

---

## 8. Support matrix (proposed)

| Server | Go server | SQL tool pack | `component_mcp_guard` | `component_mcp` |
|---|---|---|---|---|
| MySQL 8.0 | ✅ | ✅ | ❌ (no event tracking) | ❌ |
| MySQL 8.4 LTS | ✅ | ✅ | ✅ | ❌ |
| MySQL 9.7 LTS | ✅ | ✅ | ✅ primary | ❌ |
| MySQL 26.7 | ✅ | ✅ | ✅ | ❌ (no MRS component, no `account_locked` check) |
| MySQL 26.10+ | ✅ | ✅ | ✅ | 🧪 → ✅ after go/no-go |
| Percona Server 8.4 / 9.x | ✅ | ✅ | 🟡 best effort | ❌ |
| MariaDB | ✅ | 🟡 adapted | ❌ | ❌ |
| Managed cloud | ✅ | 🟡 if routines allowed | ❌ | ❌ |

Innovation releases are supported only until the next one ships. Each new Innovation release gets a new guard/endpoint build; older Innovation builds are not maintained.

---

## 9. Plan

| Phase | Deliverable | Depends on | Estimate |
|---|---|---|---|
| 0 | **In review:** [askdba/myvector#227](https://github.com/askdba/myvector/pull/227). Refresh source trees (8.4.11, 9.7.2, 26.10.0); hello-world `component_mcp_guard` building on 8.4, 9.7, 26.10 via the MyVector scripts (lifecycle test 7/7 on each); add `sql/mcp/LICENSE` (Apache-2.0) and the `NOTICE` entry | — | 1 week |
| 0b | **Done.** Streamable HTTP MCP transport in the Go server (§6.1a): [#140](https://github.com/askdba/mysql-mcp-server/issues/140), [PR #141](https://github.com/askdba/mysql-mcp-server/pull/141), shipped in [v1.9.0-rc.1](https://github.com/askdba/mysql-mcp-server/releases/tag/v1.9.0-rc.1) | — | done |
| 1 | SQL tool pack v1 + Go `install-schema` + parity tests | 0 (license) | 2–3 weeks |
| 2 | `component_mcp_guard`: privileges, sysvars, read-only + schema enforcement, status vars, MTR | 0 | 3–4 weeks |
| 3 | `P_S.mcp_tool_calls`, audit emission; Go connection attributes + statement comment labels, `server_info` detection, error mapping; Docker images | 2 | 2–3 weeks |
| 4 | `component_mcp` build spike on 26.10: in-tree build with the Router HTTP libraries, one tool served, build time / size / thread budget, token auth (§5.3). **Exit requires** a deny-path test that the guard's policy reaches `run_query` in impersonated `"local"` sessions: an account without `MCP_USER` is not treated as MCP, and an MCP account's write is rejected. This answers open question 4 | 0 (26.10.0 source tag: available) | 2–3 weeks |
| 5 | `component_mcp` MVP (5 core tools, loopback), fuzzing, go/no-go | 1, 2, 4 | 4–6 weeks |

Phases 1 and 2 can run in parallel: they touch different code (SQL/Go vs. C++) and share only the policy names.

## 10. Go/no-go for `component_mcp` GA

All of the following must hold:

- Zero `mysqld` crashes over 72h of fuzzing plus a soak test under ASAN.
- Guard overhead under 1% p99 on non-MCP traffic.
- The auth design (OAuth 2.1 / token → account, credential storage) passes security review.
- MCP spec conformance (Streamable HTTP, current spec revision) passes against at least two clients.
- A maintainer owns the C++ code in myvector.

## 11. Open questions

1. ~~Does MRS (26.10) expose an extension point for custom handlers?~~ Resolved: no, it provides no services. `component_mcp` reuses its Router HTTP libraries instead (§3a).
2. ~~Does `go-sql-driver/mysql` v1.10 send query attributes?~~ Resolved: no (upstream [PR #1784](https://github.com/go-sql-driver/mysql/pull/1784) closed unmerged). Use connection attributes plus statement comments (§3a).
3. ~~`table_access` events or digest parsing for `mcp.allowed_schemas`?~~ Resolved: `table_access` events, which carry the database and table per access (§3a). Phase 2 tests must confirm that views and stored functions raise events for their underlying tables.
4. Do impersonated `"local"` command-service sessions count against `max_connections`, `max_user_connections` and the thread pool (Community since 26.7)? Do they fire `event_tracking_connection` with the impersonated account, so that the guard's per-connection `MCP_USER` cache works for them? Verify in the 26.10 source; `component_mysql_rest_service` uses the same impersonated-session pattern and is the reference to study. Phase 4 cannot close until a deny-path test proves the guard sees these sessions (§9).
5. ~~Licensing sign-off for `sql/mcp/`~~ Resolved: askdba owns the myvector copyright and licenses `sql/mcp/` as Apache-2.0 (§7.2).
6. Branding and images: ship in `ghcr.io/askdba/myvector` with tags, or as a separate `mysql-mcp` image family?
7. Will Oracle ship MCP on MRS natively? The guard stays scoped to our server (§3a). If Oracle ships one, our differentiation is tool depth, the guard and support from 8.0 to 26.10 plus MariaDB.

---

## 12. References

- MySQL 26.10.0 release notes (GA 2026-10-20; EA was 2026-09-18): `component_mysql_rest_service`, `MYSQL_COMMAND_PASSWORD`, `account_locked`, `psi_thread_v7`: <https://dev.mysql.com/doc/relnotes/mysql/26.10/en/news-26-10-0.html>
- MySQL 26.10 release notes index (26.10.0 GA 2026-10-20, EA 2026-09-18; 26.7.1, 26.7.0): <https://dev.mysql.com/doc/relnotes/mysql/26.10/en/>
- MySQL 26.7.0 release notes: `mysql_command_session_state`, `field_info("mysql_type")`, CalVer: <https://dev.mysql.com/doc/relnotes/mysql/26.7/en/news-26-7-0.html>
- MySQL releases (Innovation and LTS): <https://dev.mysql.com/doc/refman/26.7/en/mysql-releases.html>
- Oracle blog, calendar versioning: <https://blogs.oracle.com/mysql/a-more-predictable-mysql-release-model-calendar-versions-lts-and-innovation>
- Local source trees: `~/myvector/mysql-server-mysql-{8.4.8,9.7.0,26.7.0}`, in particular `include/mysql/components/services/`, `sql/sql_audit.h` and `components/test/event_tracking_test/`
- myvector build scripts: `scripts/build-component-*-docker.sh`, `Dockerfile.component`, `scripts/pre-release-test.sh`
- golang/go#11100 (unloading `c-shared` libraries)
