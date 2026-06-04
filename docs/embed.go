// docs/embed.go
package docs

import _ "embed"

// QueryOptimizationGuide is the content of query_optimization_guide.md,
// embedded at build time.
//
//go:embed query_optimization_guide.md
var QueryOptimizationGuide string

// QueryOptimizationComprehensive is the content of
// mysql_query_optimization_comprehensive.md, embedded at build time.
//
//go:embed mysql_query_optimization_comprehensive.md
var QueryOptimizationComprehensive string
