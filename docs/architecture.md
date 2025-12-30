# MySQL MCP Server Architecture

This document provides detailed architecture diagrams for the MySQL MCP Server.

## Table of Contents

- [High-Level Architecture](#high-level-architecture)
- [Component Structure](#component-structure)
- [Request Flow](#request-flow)
  - [MCP Mode](#mcp-mode-stdio)
  - [HTTP REST API Mode](#http-rest-api-mode)
- [Configuration Loading](#configuration-loading)
- [Connection Management](#connection-management)
- [Tool Categories](#tool-categories)

---

## High-Level Architecture

The MySQL MCP Server acts as a bridge between AI clients and MySQL databases, supporting both MCP protocol (stdio) and HTTP REST API modes.

```mermaid
graph TB
    subgraph "AI Clients"
        Claude[("Claude Desktop<br/>(MCP Client)")]
        Cursor[("Cursor IDE<br/>(MCP Client)")]
        ChatGPT[("ChatGPT<br/>Custom GPT")]
        HTTPClient[("HTTP Clients<br/>curl, Postman")]
    end
    
    subgraph "MySQL MCP Server"
        direction TB
        MCP["MCP Protocol Handler<br/>(stdio JSON-RPC)"]
        HTTP["HTTP REST API<br/>(Port 9306)"]
        Tools["Tool Handlers<br/>(Core, Extended, Vector)"]
        CM["Connection Manager<br/>(Pool per DSN)"]
        Config["Configuration<br/>(File + Env)"]
    end
    
    subgraph "MySQL Databases"
        DB1[("Production<br/>MySQL 8.x/9.x")]
        DB2[("Staging<br/>MySQL 8.x")]
        DB3[("Development<br/>MySQL/MariaDB")]
    end
    
    Claude -->|"stdio<br/>MCP Protocol"| MCP
    Cursor -->|"stdio<br/>MCP Protocol"| MCP
    ChatGPT -->|"HTTP/HTTPS"| HTTP
    HTTPClient -->|"HTTP/HTTPS"| HTTP
    
    MCP --> Tools
    HTTP --> Tools
    Tools --> CM
    Config -.->|"loads"| CM
    
    CM -->|"mysql driver<br/>+TLS"| DB1
    CM -->|"mysql driver"| DB2
    CM -->|"mysql driver"| DB3
```

---

## Component Structure

The codebase is organized into clear packages with specific responsibilities.

```mermaid
graph TB
    subgraph "cmd/mysql-mcp-server"
        main["main.go<br/>Entry point, MCP server setup"]
        tools["tools.go<br/>Core tool handlers"]
        toolsExt["tools_extended.go<br/>Extended tool handlers"]
        http["http.go<br/>REST API handlers"]
        conn["connection.go<br/>Connection manager"]
        types["types.go<br/>Input/output types"]
        logging["logging.go<br/>Structured logging"]
        tokenEst["token_estimator.go<br/>Token counting"]
    end
    
    subgraph "internal/config"
        config["config.go<br/>Config loading"]
        file["file.go<br/>File config parser"]
    end
    
    subgraph "internal/mysql"
        client["client.go<br/>MySQL client wrapper"]
    end
    
    subgraph "internal/api"
        middleware["middleware.go<br/>HTTP middleware"]
        ratelimit["ratelimit.go<br/>Rate limiter"]
        response["response.go<br/>Response helpers"]
    end
    
    subgraph "internal/util"
        validator["sql_validator.go<br/>SQL validation"]
        parser["sql_parser.go<br/>SQL parsing"]
        identifiers["identifiers.go<br/>Identifier quoting"]
    end
    
    main --> tools
    main --> toolsExt
    main --> http
    main --> conn
    tools --> types
    toolsExt --> types
    http --> types
    
    main --> config
    config --> file
    
    conn --> client
    tools --> client
    toolsExt --> client
    
    http --> middleware
    http --> ratelimit
    http --> response
    
    tools --> validator
    tools --> parser
    tools --> identifiers
```

---

## Request Flow

### MCP Mode (stdio)

In MCP mode, the server communicates via stdin/stdout using JSON-RPC.

```mermaid
sequenceDiagram
    participant Client as AI Client<br/>(Claude/Cursor)
    participant MCP as MCP Server<br/>(main.go)
    participant Tool as Tool Handler<br/>(tools.go)
    participant CM as Connection<br/>Manager
    participant MySQL as MySQL<br/>Database

    Client->>MCP: JSON-RPC Request<br/>{"method": "tools/call", "params": {...}}
    MCP->>MCP: Parse request<br/>Identify tool
    
    alt Tool: mysql_query
        MCP->>Tool: Execute mysql_query
        Tool->>Tool: Validate SQL<br/>(read-only check)
        Tool->>CM: Get connection
        CM->>MySQL: Execute query
        MySQL-->>CM: Result rows
        CM-->>Tool: Return results
        Tool->>Tool: Format response<br/>(apply row limit)
        Tool-->>MCP: Tool result
    else Tool: list_tables
        MCP->>Tool: Execute list_tables
        Tool->>CM: Get connection
        CM->>MySQL: SHOW TABLES
        MySQL-->>CM: Table list
        CM-->>Tool: Return tables
        Tool-->>MCP: Tool result
    end
    
    MCP-->>Client: JSON-RPC Response<br/>{"result": {...}}
```

### HTTP REST API Mode

In HTTP mode, the server exposes RESTful endpoints.

```mermaid
sequenceDiagram
    participant Client as HTTP Client<br/>(ChatGPT/curl)
    participant MW as Middleware<br/>(Rate Limit, Logging)
    participant Handler as HTTP Handler<br/>(http.go)
    participant Tool as Tool Handler<br/>(tools.go)
    participant CM as Connection<br/>Manager
    participant MySQL as MySQL<br/>Database

    Client->>MW: POST /api/query<br/>{"sql": "SELECT...", "database": "mydb"}
    MW->>MW: Check rate limit
    
    alt Rate Limited
        MW-->>Client: 429 Too Many Requests<br/>Retry-After: 1
    else Allowed
        MW->>Handler: Forward request
        Handler->>Handler: Parse JSON body
        Handler->>Tool: Execute mysql_query
        Tool->>Tool: Validate SQL
        Tool->>CM: Get connection
        CM->>MySQL: Execute query
        MySQL-->>CM: Result rows
        CM-->>Tool: Return results
        Tool-->>Handler: Tool result
        Handler-->>MW: JSON response
        MW-->>Client: 200 OK<br/>{"success": true, "data": {...}}
    end
```

---

## Configuration Loading

Configuration is loaded from multiple sources with a clear priority order.

```mermaid
flowchart TB
    Start([Start]) --> FindConfig{Config file<br/>exists?}
    
    FindConfig -->|"Yes"| LoadFile["Load config file<br/>(YAML/JSON)"]
    FindConfig -->|"No"| UseDefaults["Use default values"]
    
    LoadFile --> ApplyEnv["Apply environment<br/>variable overrides"]
    UseDefaults --> ApplyEnv
    
    ApplyEnv --> LoadConns{Connection<br/>source?}
    
    LoadConns -->|"MYSQL_CONNECTIONS"| ParseJSON["Parse JSON array"]
    LoadConns -->|"MYSQL_DSN*"| ParseDSN["Parse DSN env vars"]
    LoadConns -->|"Config file"| UseFileConns["Use file connections"]
    
    ParseJSON --> ApplySSL["Apply MYSQL_SSL<br/>to connections<br/>without explicit SSL"]
    ParseDSN --> ApplySSL
    UseFileConns --> Validate
    
    ApplySSL --> Validate{Valid<br/>config?}
    
    Validate -->|"Yes"| Ready([Config Ready])
    Validate -->|"No"| Error([Error: No DSN])
    
    subgraph "Priority (High to Low)"
        direction LR
        P1["1. Environment Variables"]
        P2["2. Config File"]
        P3["3. Defaults"]
        P1 --> P2 --> P3
    end
```

### Config File Search Order

The server searches for config files in the following order (first found wins):

```mermaid
flowchart TB
    subgraph "1. Override (Highest Priority)"
        A["--config flag"]
        B["MYSQL_MCP_CONFIG env var"]
    end
    
    subgraph "2. Current Directory"
        C["./mysql-mcp-server.yaml"]
        D["./mysql-mcp-server.yml"]
        E["./mysql-mcp-server.json"]
    end
    
    subgraph "3. User Config"
        F["~/.config/mysql-mcp-server/config.yaml"]
        G["~/.config/mysql-mcp-server/config.yml"]
        H["~/.config/mysql-mcp-server/config.json"]
    end
    
    subgraph "4. System Config (Lowest Priority)"
        I["/etc/mysql-mcp-server/config.yaml"]
        J["/etc/mysql-mcp-server/config.yml"]
        K["/etc/mysql-mcp-server/config.json"]
    end
    
    A --> C
    B --> C
    C --> D --> E --> F --> G --> H --> I --> J --> K
    K --> L(["First found wins"])
```

---

## Connection Management

The Connection Manager handles multiple database connections with connection pooling.

```mermaid
graph TB
    subgraph "Connection Manager"
        CM["ConnectionManager<br/>- connections map<br/>- activeConn string<br/>- mutex"]
    end
    
    subgraph "Connection Pools"
        Pool1["Pool: default<br/>MaxOpen: 10<br/>MaxIdle: 5"]
        Pool2["Pool: production<br/>MaxOpen: 10<br/>MaxIdle: 5"]
        Pool3["Pool: staging<br/>MaxOpen: 10<br/>MaxIdle: 5"]
    end
    
    subgraph "Pool Configuration"
        direction LR
        MaxOpen["MaxOpenConns<br/>(MYSQL_MAX_OPEN_CONNS)"]
        MaxIdle["MaxIdleConns<br/>(MYSQL_MAX_IDLE_CONNS)"]
        Lifetime["ConnMaxLifetime<br/>(MYSQL_CONN_MAX_LIFETIME_MINUTES)"]
        IdleTime["ConnMaxIdleTime<br/>(MYSQL_CONN_MAX_IDLE_TIME_MINUTES)"]
    end
    
    subgraph "MySQL Servers"
        DB1[("localhost:3306<br/>default")]
        DB2[("prod-server:3306<br/>production")]
        DB3[("staging:3306<br/>staging")]
    end
    
    CM --> Pool1
    CM --> Pool2
    CM --> Pool3
    
    Pool1 -->|"SSL: skip-verify"| DB1
    Pool2 -->|"SSL: true"| DB2
    Pool3 -->|"SSL: false"| DB3
    
    MaxOpen -.-> Pool1
    MaxIdle -.-> Pool1
    Lifetime -.-> Pool1
    IdleTime -.-> Pool1
```

### Connection Selection Flow

```mermaid
flowchart TB
    Request["Tool Request"] --> HasConn{Connection<br/>specified?}
    
    HasConn -->|"Yes"| FindConn["Find named connection"]
    HasConn -->|"No"| UseActive["Use active connection"]
    
    FindConn --> Exists{Connection<br/>exists?}
    Exists -->|"Yes"| GetPool["Get connection pool"]
    Exists -->|"No"| Error["Error: Unknown connection"]
    
    UseActive --> GetPool
    GetPool --> Execute["Execute query"]
    Execute --> Return["Return results"]
```

---

## Tool Categories

Tools are organized into three categories based on functionality.

```mermaid
graph TB
    subgraph "Core Tools (Always Available)"
        direction LR
        mysql_query["mysql_query<br/>Execute read-only SQL"]
        list_databases["list_databases<br/>Show all databases"]
        list_tables["list_tables<br/>Show tables in database"]
        describe_table["describe_table<br/>Show table structure"]
        ping["ping<br/>Test connection"]
        server_info["server_info<br/>MySQL version info"]
        list_connections["list_connections<br/>Show all DSNs"]
        use_connection["use_connection<br/>Switch active DSN"]
    end
    
    subgraph "Extended Tools (MYSQL_MCP_EXTENDED=1)"
        direction LR
        list_indexes["list_indexes"]
        show_create_table["show_create_table"]
        explain_query["explain_query"]
        list_views["list_views"]
        list_triggers["list_triggers"]
        list_procedures["list_procedures"]
        list_functions["list_functions"]
        list_partitions["list_partitions"]
        get_database_size["get_database_size"]
        get_table_sizes["get_table_sizes"]
        list_foreign_keys["list_foreign_keys"]
        show_status["show_status"]
        show_variables["show_variables"]
    end
    
    subgraph "Vector Tools (MYSQL_MCP_VECTOR=1)"
        direction LR
        vector_search["vector_search<br/>Similarity search"]
        get_vector_info["get_vector_info<br/>Vector column info"]
    end
    
    Core["Enable:<br/>Default"] --> mysql_query
    Extended["Enable:<br/>MYSQL_MCP_EXTENDED=1"] --> list_indexes
    Vector["Enable:<br/>MYSQL_MCP_VECTOR=1"] --> vector_search
```

---

## Security Architecture

```mermaid
graph TB
    subgraph "Input Validation"
        SQLValid["SQL Validator<br/>- Read-only enforcement<br/>- Dangerous statement blocking"]
        IdentValid["Identifier Validator<br/>- Proper quoting<br/>- Injection prevention"]
    end
    
    subgraph "Connection Security"
        TLS["TLS/SSL Options<br/>- true: verify certs<br/>- skip-verify: no verify<br/>- preferred: opportunistic"]
        DSNMask["DSN Masking<br/>- Passwords hidden in logs"]
    end
    
    subgraph "API Security"
        RateLimit["Rate Limiting<br/>- Per-IP tracking<br/>- Configurable RPS/burst"]
        AuditLog["Audit Logging<br/>- Query logging<br/>- Connection tracking"]
    end
    
    Request["Incoming Request"] --> SQLValid
    SQLValid --> IdentValid
    IdentValid --> TLS
    TLS --> RateLimit
    RateLimit --> AuditLog
    AuditLog --> Execute["Execute Query"]
```

---

## Deployment Options

```mermaid
graph TB
    subgraph "Deployment Methods"
        Binary["Binary<br/>Homebrew / Direct download"]
        Docker["Docker<br/>ghcr.io/askdba/mysql-mcp-server"]
        Source["From Source<br/>go install"]
    end
    
    subgraph "Integration Modes"
        MCPMode["MCP Mode (stdio)<br/>Claude Desktop, Cursor"]
        HTTPMode["HTTP Mode<br/>ChatGPT, REST clients"]
    end
    
    subgraph "Configuration Sources"
        EnvVars["Environment Variables"]
        ConfigFile["Config File (YAML/JSON)"]
        CLI["Command Line Args"]
    end
    
    Binary --> MCPMode
    Binary --> HTTPMode
    Docker --> MCPMode
    Docker --> HTTPMode
    Source --> MCPMode
    Source --> HTTPMode
    
    EnvVars --> Binary
    EnvVars --> Docker
    ConfigFile --> Binary
    ConfigFile --> Docker
```

---

## Data Flow Summary

```mermaid
flowchart LR
    subgraph Input
        AI["AI Client"]
        HTTP["HTTP Client"]
    end
    
    subgraph Processing
        Parse["Parse Request"]
        Validate["Validate"]
        Route["Route to Tool"]
        Execute["Execute"]
        Format["Format Response"]
    end
    
    subgraph Output
        Response["Response"]
    end
    
    AI --> Parse
    HTTP --> Parse
    Parse --> Validate
    Validate --> Route
    Route --> Execute
    Execute --> Format
    Format --> Response
```

