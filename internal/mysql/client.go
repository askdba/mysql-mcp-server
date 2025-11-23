// internal/mysql/client.go
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Client struct {
	db           *sql.DB
	maxRows      int
	queryTimeout time.Duration
}

type Config struct {
	DSN           string
	MaxRows       int
	QueryTimeoutS int
}

func New(cfg Config) (*Client, error) {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	return &Client{
		db:           db,
		maxRows:      cfg.MaxRows,
		queryTimeout: time.Duration(cfg.QueryTimeoutS) * time.Second,
	}, nil
}

func (c *Client) Close() error {
	return c.db.Close()
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.queryTimeout)
}

func (c *Client) ListDatabases(ctx context.Context) ([]string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	rows, err := c.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (c *Client) ListTables(ctx context.Context, database string) ([]string, error) {
	if database == "" {
		return nil, fmt.Errorf("database is required")
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
		return nil, err
	}

	rows, err := c.db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func (c *Client) DescribeTable(ctx context.Context, database, table string) ([]map[string]any, error) {
	if database == "" || table == "" {
		return nil, fmt.Errorf("database and table are required")
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
		return nil, err
	}

	rows, err := c.db.QueryContext(ctx, "DESCRIBE `"+table+"`")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		row := map[string]any{}
		for i, col := range cols {
			row[col] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// RunQuery is intentionally “read oriented” – callers should enforce SELECT only.
func (c *Client) RunQuery(ctx context.Context, sqlText string, maxRows int) ([]map[string]any, error) {
	if sqlText == "" {
		return nil, fmt.Errorf("sql is required")
	}
	if maxRows <= 0 || maxRows > c.maxRows {
		maxRows = c.maxRows
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	rows, err := c.db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	count := 0
	for rows.Next() {
		if count >= maxRows {
			break
		}
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = values[i]
		}
		result = append(result, row)
		count++
	}
	return result, rows.Err()
}
