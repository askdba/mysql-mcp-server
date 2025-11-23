// cmd/mysql-mcp-server/main.go
package main

import (
	"context"
	"log"

	mycfg "github.com/askdba/mysql-mcp-server/internal/config"
	mydb "github.com/askdba/mysql-mcp-server/internal/mysql"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListDatabasesInput struct{}

type ListDatabasesOutput struct {
	Databases []string `json:"databases"`
}

type ListTablesInput struct {
	Database string `json:"database"`
}

type ListTablesOutput struct {
	Tables []string `json:"tables"`
}

type DescribeTableInput struct {
	Database string `json:"database"`
	Table    string `json:"table"`
}

type DescribeTableOutput struct {
	Columns []map[string]any `json:"columns"`
}

type RunQueryInput struct {
	SQL     string `json:"sql"`
	MaxRows int    `json:"max_rows"`
}

type RunQueryOutput struct {
	Rows []map[string]any `json:"rows"`
}

type serverState struct {
	db *mydb.Client
}

// ---- Tool functions ----

func (s *serverState) ListDatabasesTool(ctx context.Context, req *mcp.CallToolRequest, in ListDatabasesInput) (*mcp.CallToolResult, ListDatabasesOutput, error) {
	dbs, err := s.db.ListDatabases(ctx)
	if err != nil {
		return nil, ListDatabasesOutput{}, err
	}
	return nil, ListDatabasesOutput{Databases: dbs}, nil
}

func (s *serverState) ListTablesTool(ctx context.Context, req *mcp.CallToolRequest, in ListTablesInput) (*mcp.CallToolResult, ListTablesOutput, error) {
	tables, err := s.db.ListTables(ctx, in.Database)
	if err != nil {
		return nil, ListTablesOutput{}, err
	}
	return nil, ListTablesOutput{Tables: tables}, nil
}

func (s *serverState) DescribeTableTool(ctx context.Context, req *mcp.CallToolRequest, in DescribeTableInput) (*mcp.CallToolResult, DescribeTableOutput, error) {
	cols, err := s.db.DescribeTable(ctx, in.Database, in.Table)
	if err != nil {
		return nil, DescribeTableOutput{}, err
	}
	return nil, DescribeTableOutput{Columns: cols}, nil
}

func (s *serverState) RunQueryTool(ctx context.Context, req *mcp.CallToolRequest, in RunQueryInput) (*mcp.CallToolResult, RunQueryOutput, error) {
	// Optional safety: enforce read-only
	// You can strengthen this later with a parser.
	// naive check:
	// if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(in.SQL)), "SELECT") {
	//	   return nil, RunQueryOutput{}, fmt.Errorf("only SELECT queries are allowed")
	// }

	rows, err := s.db.RunQuery(ctx, in.SQL, in.MaxRows)
	if err != nil {
		return nil, RunQueryOutput{}, err
	}
	return nil, RunQueryOutput{Rows: rows}, nil
}

func main() {
	// Important: log package logs to stderr, safe for MCP stdio.
	cfg, err := mycfg.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	mysqlClient, err := mydb.New(mydb.Config{
		DSN:           cfg.DSN,
		MaxRows:       cfg.MaxRows,
		QueryTimeoutS: cfg.QueryTimeoutS,
	})
	if err != nil {
		log.Fatalf("db init error: %v", err)
	}
	defer mysqlClient.Close()

	state := &serverState{db: mysqlClient}

	impl := &mcp.Implementation{
		Name:    "mysql-mcp-server",
		Version: "0.2.0",
	}

	server := mcp.NewServer(impl, nil)

	// Tool wiring: may differ slightly by SDK version.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_databases",
		Description: "List all databases visible to the configured MySQL user",
	}, state.ListDatabasesTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List tables in a given database",
	}, state.ListTablesTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Describe a table (columns, types, nullability, keys)",
	}, state.DescribeTableTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_query",
		Description: "Run a (preferably read-only) SQL query on the configured database",
	}, state.RunQueryTool)

	ctx := context.Background()
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
