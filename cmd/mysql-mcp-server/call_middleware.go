// cmd/mysql-mcp-server/call_middleware.go
package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectionMetaKey names the connection a tool call used, in the result's _meta.
const connectionMetaKey = "mysql-mcp-server/connection"

// toolCallMiddleware pins each tools/call to one connection (see callConn) and, over the
// HTTP transport when the session can see more than one connection, names that
// connection in the result. A session's use_connection choice ends with the session
// (for example after the idle timeout); a client that starts a new session runs on the
// default connection again, and the name in every result makes that visible.
func toolCallMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" || connManager == nil {
				return next(ctx, method, req)
			}
			ctx, _ = connManager.PinFor(ctx)
			if r, ok := req.(*mcp.CallToolRequest); ok && r.Params != nil {
				ctx = withCallLabel(ctx, r.Params.Name)
			}
			res, err := next(ctx, method, req)
			if r, ok := res.(*mcp.CallToolResult); ok && r != nil && r.IsError {
				explainGuardRefusal(r)
			}
			if sc := sessionConnFrom(ctx); sc != nil {
				if r, ok := res.(*mcp.CallToolResult); ok && r != nil && connManager.visibleConnectionCount(sc) > 1 {
					// Report the pin after the call: use_connection / add_connection move it.
					annotateConnection(r, sc.displayName(callConnFrom(ctx).get()))
				}
			}
			return res, err
		}
	}
}

// annotateConnection names the connection in the result's _meta and in a text block.
func annotateConnection(r *mcp.CallToolResult, name string) {
	if name == "" {
		return
	}
	if r.Meta == nil {
		r.Meta = mcp.Meta{}
	}
	r.Meta[connectionMetaKey] = name
	r.Content = append(r.Content, &mcp.TextContent{Text: fmt.Sprintf("connection: %s", name)})
}
