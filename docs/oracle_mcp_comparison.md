# Comparison with Oracle MCP Server and Suggested Updates

This report compares the Go-based `mysql-mcp-server` with the official Oracle version (`oracle/mcp`) and tracks the enhancements it suggested.

**Last refreshed: 2026-10-24** (mysql-mcp-server column and suggestion status, as of [v1.9.0-rc.1](https://github.com/askdba/mysql-mcp-server/releases/tag/v1.9.0-rc.1)). The `oracle/mcp` column reflects the original review (2026-02) and has **not** been re-verified; check [oracle/mcp](https://github.com/oracle/mcp) for its current features.

## Feature Comparison

| Feature Area | Local (`askdba/mysql-mcp-server`) | Oracle (`oracle/mcp`, as of 2026-02) |
| :--- | :--- | :--- |
| **Primary Target** | General use, performance, security. | MySQL AI, HeatWave, OCI users. |
| **Database Tools** | Deep introspection (about 30 tools). | Basic SQL execution only. |
| **AI / Vector** | Native MySQL 9.0+ vector support. | Native HeatWave GenAI and ML tools. |
| **Connectivity** | Standard DSN, SSL/TLS, multiple DSNs, SSH tunnel through a bastion host (host-key verification on by default), runtime `add_connection`. | SSH tunneling via bastion host. |
| **MCP transports** | stdio, and Streamable HTTP with bearer token, TLS and per-session connections (since v1.9.0-rc.1). Separate REST API. | Not re-verified. |
| **Server versions** | MySQL 8.0, 8.4, 9.7, 26.10 and MariaDB 11.4, 12.3, all tested in CI. | Not re-verified. |
| **Natural Language** | LLM-driven (via MCP client). | `ask_nl_sql`: Built-in NL to SQL. |
| **Cloud Integration** | None. | OCI Objects, Compartments, Buckets. |
| **Tracking** | Token tracking, audit logging. | None. |
| **Architecture** | Single Go binary (zero deps). | Python (requires environment setup). |

## Suggested Updates and Status

| # | Suggestion | Status |
|---|---|---|
| 1 | **SSH tunneling (bastion)** | **Done** in v1.7.0 ([#79](https://github.com/askdba/mysql-mcp-server/issues/79)); host keys are verified by default (`known_hosts` or a pinned fingerprint). |
| 2 | **Natural language to SQL (`ask_nl_sql`)**: a server-side tool that grounds the prompt with exact table schemas | **Open:** [#80](https://github.com/askdba/mysql-mcp-server/issues/80). |
| 3 | **Connection-specific execution**: let core tools target a named connection | **Partly done.** Tools still run on the active connection, but `use_connection` and runtime `add_connection` ([#106](https://github.com/askdba/mysql-mcp-server/issues/106), v1.7.0) switch it, and over Streamable HTTP each MCP session has its own active connection (v1.9.0-rc.1). A per-call `connection` parameter is not implemented. |
| 4 | **HeatWave / GenAI wrappers** (e.g. `ml_generate`), behind a flag | **Not started.** |
| 5 | **Data ingestion** for vector stores (local/cloud) | **Not started.** |

## Related

- [RFC 0001: MySQL-native MCP components](rfc/0001-mysql-component-architecture.md): server-side guardrails and an in-server MCP endpoint, and how this project positions itself if Oracle ships MCP natively.
