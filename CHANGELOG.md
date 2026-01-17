# Changelog

All notable changes to this project will be documented in this file.

The format is based on "Keep a Changelog" and this project follows
Semantic Versioning.

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
