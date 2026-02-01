# Comparison with Oracle MCP Server & Suggested Updates

This report compares the current Go-based `mysql-mcp-server` with the Oracle official version (`oracle/mcp`) and suggests potential enhancements.

## Feature Comparison

| Feature Area | Local (`askdba/mysql-mcp-server`) | Oracle (`oracle/mcp`) |
| :--- | :--- | :--- |
| **Primary Target** | General use, performance, security. | MySQL AI, HeatWave, OCI users. |
| **Database Tools** | Very deep (20+ introspection tools). | Basic SQL execution only. |
| **AI / Vector** | Native MySQL 9.0 Vector Support. | Native HeatWave GenAI/ML tools. |
| **Connectivity** | Standard DSN, SSL/TLS. | SSH Tunneling via Bastion host. |
| **Natural Language** | LLM-driven (via MCP client). | `ask_nl_sql`: Built-in NL to SQL. |
| **Cloud Integration** | None. | OCI Objects, Compartments, Buckets. |
| **Tracking** | Token tracking, Audit logging. | None. |
| **Architecture** | Single Go Binary (Zero deps). | Python (Requires environment Setup). |

## Suggested Updates for `askdba/mysql-mcp-server`

### 1. SSH Tunneling (Bastion)
Support connecting to MySQL instances through an SSH bastion. This is a common requirement for production databases.
- **Complexity**: High
- **Priority**: High

### 2. Natural Language to SQL (`ask_nl_sql`)
Add a tool that specifically handles NL -> SQL conversion using schema context. While Claude does this natively, a server-side tool can provide a more "grounded" prompt with exact table schemas.
- **Complexity**: Medium
- **Priority**: Medium

### 3. Connection-Specific Execution
Modify core tools to accept an optional `connection` parameter. Currently, the server uses a "switch-and-run" model. Allowing `run_query(sql, connection="prod")` would improve usability in multi-DB environments.
- **Complexity**: Medium
- **Priority**: Medium

### 4. HeatWave / GenAI Wrappers
Add support for MySQL HeatWave specific functions (like `ml_generate`) if the connected server supports them. This could be gated behind a `MYSQL_MCP_HEATWAVE=1` flag.
- **Complexity**: Low
- **Priority**: Low

### 5. Data Ingestion (Local/Cloud)
Tools to assist in loading data for vector stores, similar to `load_vector_store_local`. This could facilitate the initial setup of RAG-capable databases.
- **Complexity**: Medium
- **Priority**: Low

## Next Steps
1. [x] Research and Comparison
2. [x] Get feedback on priorities
3. [x] Create individual issues for selected features
