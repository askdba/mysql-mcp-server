// cmd/mysql-mcp-server/resources.go
package main

import (
	"context"

	"github.com/askdba/mysql-mcp-server/docs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerResources(server *mcp.Server) {
	server.AddResource(
		&mcp.Resource{
			URI:         "docs://mysql-mcp-server/query-optimization-guide",
			Name:        "SQL Query Optimization Guide",
			Description: "Practical SQL optimization patterns and query rewriting techniques with before/after examples.",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      req.Params.URI,
						MIMEType: "text/markdown",
						Text:     docs.QueryOptimizationGuide,
					},
				},
			}, nil
		},
	)

	server.AddResource(
		&mcp.Resource{
			URI:         "docs://mysql-mcp-server/query-optimization-comprehensive",
			Name:        "Comprehensive MySQL Query Optimization Guide",
			Description: "Deep technical guide covering optimizer statistics, advanced indexing, execution plan analysis, and operational best practices.",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      req.Params.URI,
						MIMEType: "text/markdown",
						Text:     docs.QueryOptimizationComprehensive,
					},
				},
			}, nil
		},
	)
}
