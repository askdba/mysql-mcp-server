# internal/toolpack

Vendored copy of the `mysql_mcp` SQL tool pack ([RFC 0001](../../docs/rfc/0001-mysql-component-architecture.md) §5.1).

- **Source:** [askdba/myvector `sql/mcp/`](https://github.com/askdba/myvector/tree/main/sql/mcp), Apache-2.0 ([`sql/LICENSE`](sql/LICENSE)). Do not edit `sql/*.sql` here; change them in myvector, then copy them over and update `SourceRef` and, for a new pack version, `Version` in `toolpack.go`.
- **Embedded** in the binary for `mysql-mcp-server install-schema`.
- **Parity:** `cmd/mysql-mcp-server/toolpack_integration_test.go` (`-tags=integration`) runs every catalog tool with and without the pack and requires identical output.
