package main

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func wrapTool[I any, O any](toolName string, h mcp.ToolHandlerFor[I, O]) mcp.ToolHandlerFor[I, O] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input I) (*mcp.CallToolResult, O, error) {
		start := time.Now()
		res, out, err := h(ctx, req, input)

		// Only emit these extra logs when token tracking is explicitly enabled.
		// This keeps default behavior unchanged.
		if tokenTracking && toolName != "run_query" {
			inputTokens, _ := estimateTokensForValue(input)
			outputTokens := 0
			if err == nil {
				outputTokens, _ = estimateTokensForValue(out)
			}
			tokens := TokenUsage{
				InputEstimated:  inputTokens,
				OutputEstimated: outputTokens,
				TotalEstimated:  inputTokens + outputTokens,
				Model:           tokenModel,
			}

			fields := map[string]interface{}{
				"tool":        toolName,
				"duration_ms": time.Since(start).Milliseconds(),
				"tokens": map[string]interface{}{
					"input_estimated":  tokens.InputEstimated,
					"output_estimated": tokens.OutputEstimated,
					"total_estimated":  tokens.TotalEstimated,
					"model":            tokens.Model,
				},
			}
			if err != nil {
				fields["error"] = err.Error()
				logError("tool failed", fields)
			} else {
				logInfo("tool executed", fields)
			}
		}

		return res, out, err
	}
}

// Wrapped tool handlers used by both MCP and HTTP.
var (
	toolListDatabasesWrapped  = wrapTool("list_databases", toolListDatabases)
	toolListTablesWrapped     = wrapTool("list_tables", toolListTables)
	toolDescribeTableWrapped  = wrapTool("describe_table", toolDescribeTable)
	toolRunQueryWrapped       = toolRunQuery // run_query has dedicated query/audit logs with tokens
	toolPingWrapped           = wrapTool("ping", toolPing)
	toolServerInfoWrapped     = wrapTool("server_info", toolServerInfo)
	toolListConnectionsWrapped = wrapTool("list_connections", toolListConnections)
	toolUseConnectionWrapped   = wrapTool("use_connection", toolUseConnection)

	toolVectorSearchWrapped = wrapTool("vector_search", toolVectorSearch)
	toolVectorInfoWrapped   = wrapTool("vector_info", toolVectorInfo)

	toolListIndexesWrapped     = wrapTool("list_indexes", toolListIndexes)
	toolShowCreateTableWrapped = wrapTool("show_create_table", toolShowCreateTable)
	toolExplainQueryWrapped    = wrapTool("explain_query", toolExplainQuery)
	toolListViewsWrapped       = wrapTool("list_views", toolListViews)
	toolListTriggersWrapped    = wrapTool("list_triggers", toolListTriggers)
	toolListProceduresWrapped  = wrapTool("list_procedures", toolListProcedures)
	toolListFunctionsWrapped   = wrapTool("list_functions", toolListFunctions)
	toolListPartitionsWrapped  = wrapTool("list_partitions", toolListPartitions)
	toolDatabaseSizeWrapped    = wrapTool("database_size", toolDatabaseSize)
	toolTableSizeWrapped       = wrapTool("table_size", toolTableSize)
	toolForeignKeysWrapped     = wrapTool("foreign_keys", toolForeignKeys)
	toolListStatusWrapped      = wrapTool("list_status", toolListStatus)
	toolListVariablesWrapped   = wrapTool("list_variables", toolListVariables)
)

