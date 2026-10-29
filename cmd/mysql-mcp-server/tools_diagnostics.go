package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxProcessList = 200
const maxInfoRunes = 4000

// truncateRunes shortens s to at most maxRunes Unicode code points and appends an ellipsis when truncated.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func toolProcessList(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ProcessListInput,
) (*mcp.CallToolResult, ProcessListOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := catalogRows(ctx, "process_list", nil, "SHOW FULL PROCESSLIST")
	if err != nil {
		return nil, ProcessListOutput{
			Note: fmt.Sprintf("SHOW FULL PROCESSLIST failed (need PROCESS privilege): %v", err),
		}, nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, ProcessListOutput{}, fmt.Errorf("process list columns: %w", err)
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}

	out := ProcessListOutput{Processes: []ProcessRow{}}
	n := 0
	raw := make([]sql.NullString, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		get := func(name string) string {
			if j, ok := idx[name]; ok && j < len(raw) && raw[j].Valid {
				return raw[j].String
			}
			return ""
		}
		sid := get("id")
		id, _ := strconv.ParseInt(sid, 10, 64)
		t, _ := strconv.Atoi(get("time"))
		info := truncateRunes(get("info"), maxInfoRunes)
		out.Processes = append(out.Processes, ProcessRow{
			ID:      id,
			User:    get("user"),
			Host:    get("host"),
			DB:      get("db"),
			Command: get("command"),
			Time:    t,
			State:   get("state"),
			Info:    info,
		})
		n++
		if n >= maxProcessList {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ProcessListOutput{}, err
	}
	return nil, out, nil
}

func toolKillQuery(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input KillQueryInput,
) (*mcp.CallToolResult, KillQueryOutput, error) {
	if input.ID <= 0 {
		return nil, KillQueryOutput{OK: false, Message: "id must be a positive thread id from process_list"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Safe: id is numeric only. KILL QUERY ends the current statement only; bare KILL drops the connection.
	q := fmt.Sprintf("KILL QUERY %d", input.ID)
	if _, err := getDB(ctx).ExecContext(ctx, q); err != nil {
		return nil, KillQueryOutput{OK: false, Message: err.Error()}, nil
	}
	return nil, KillQueryOutput{OK: true, Message: fmt.Sprintf("KILL QUERY %d issued", input.ID)}, nil
}

func toolReadAuditLog(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ReadAuditLogInput,
) (*mcp.CallToolResult, ReadAuditLogOutput, error) {
	if auditLogger == nil || !auditLogger.enabled {
		return nil, ReadAuditLogOutput{}, fmt.Errorf("audit log is not configured (set MYSQL_MCP_AUDIT_LOG)")
	}
	lines, truncated, err := auditLogger.ReadRecentLines(input.Lines)
	if err != nil {
		return nil, ReadAuditLogOutput{}, err
	}
	return nil, ReadAuditLogOutput{
		Path:      auditLogger.path,
		Lines:     lines,
		Truncated: truncated,
	}, nil
}

func toolSlowQueryLog(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SlowQueryLogInput,
) (*mcp.CallToolResult, SlowQueryLogOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	out := SlowQueryLogOutput{Settings: map[string]string{}}

	var slowOn, logOutput string
	if err := getDB(ctx).QueryRowContext(ctx, "SELECT @@slow_query_log, @@log_output").Scan(&slowOn, &logOutput); err != nil {
		out.Mode = "error"
		out.Message = fmt.Sprintf("could not read slow query log settings: %v", err)
		return nil, out, nil
	}
	out.Settings["slow_query_log"] = slowOn
	out.Settings["log_output"] = logOutput

	var slowName, slowVal string
	if err := getDB(ctx).QueryRowContext(ctx, "SHOW GLOBAL STATUS LIKE 'Slow_queries'").Scan(&slowName, &slowVal); err == nil {
		out.SlowQueries, _ = strconv.ParseInt(strings.TrimSpace(slowVal), 10, 64)
	}

	if strings.EqualFold(slowOn, "OFF") || slowOn == "0" {
		out.Mode = "disabled"
		out.Message = "slow_query_log is off; enable on server to collect slow queries"
		return nil, out, nil
	}

	if !strings.Contains(strings.ToUpper(logOutput), "TABLE") {
		var path sql.NullString
		_ = getDB(ctx).QueryRowContext(ctx, "SELECT @@slow_query_log_file").Scan(&path)
		out.Mode = "file"
		if path.Valid {
			out.Settings["slow_query_log_file"] = path.String
		}
		out.Message = "slow log is FILE-based; read the file on the server or set log_output=TABLE"
		return nil, out, nil
	}

	var rows *sql.Rows
	var err error
	if accessControlEnabled() {
		allowed := allowedDatabasesLower()
		if len(allowed) == 0 {
			out.Mode = "error"
			out.Message = "database allowlist is empty; cannot query mysql.slow_log"
			return nil, out, nil
		}
		ph := strings.Repeat("?,", len(allowed))
		if len(ph) > 0 {
			ph = ph[:len(ph)-1]
		}
		q := fmt.Sprintf(
			`SELECT * FROM mysql.slow_log WHERE LOWER(IFNULL(db, '')) IN (%s) ORDER BY start_time DESC LIMIT ?`,
			ph,
		)
		args := make([]interface{}, 0, len(allowed)+1)
		for _, db := range allowed {
			args = append(args, db)
		}
		args = append(args, limit)
		rows, err = getDB(ctx).QueryContext(ctx, q, args...)
	} else {
		rows, err = getDB(ctx).QueryContext(ctx,
			`SELECT * FROM mysql.slow_log ORDER BY start_time DESC LIMIT ?`, limit)
	}
	if err != nil {
		out.Mode = "error"
		out.Message = fmt.Sprintf("could not read mysql.slow_log: %v", err)
		return nil, out, nil
	}
	defer rows.Close()

	out.Mode = "table_rows"
	cols, err := rows.Columns()
	if err != nil {
		return nil, SlowQueryLogOutput{}, err
	}
	for rows.Next() {
		raw := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		r := SlowQueryLogRow{}
		for i, col := range cols {
			var v string
			if raw[i] == nil {
				v = ""
			} else {
				switch x := util.NormalizeValue(raw[i]).(type) {
				case string:
					v = x
				default:
					v = fmt.Sprint(x)
				}
			}
			switch strings.ToLower(col) {
			case "start_time":
				r.StartTime = v
			case "user_host":
				r.UserHost = v
			case "query_time":
				r.QueryTime = v
			case "lock_time":
				r.LockTime = v
			case "rows_sent":
				r.RowsSent, _ = strconv.ParseInt(v, 10, 64)
			case "rows_examined":
				r.RowsExamined, _ = strconv.ParseInt(v, 10, 64)
			case "db":
				r.Database = v
			case "last_insert_id":
				r.LastInsertID, _ = strconv.ParseInt(v, 10, 64)
			case "insert_id":
				r.InsertID, _ = strconv.ParseInt(v, 10, 64)
			case "sql_text":
				r.Query = v
			}
		}
		r.Query = truncateRunes(r.Query, maxInfoRunes)
		out.Rows = append(out.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, SlowQueryLogOutput{}, err
	}
	return nil, out, nil
}
