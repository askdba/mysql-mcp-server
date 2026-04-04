// internal/mysql/client.go
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/dbretry"
	"github.com/askdba/mysql-mcp-server/internal/util"
)

type Client struct {
	db           *sql.DB
	maxRows      int
	queryTimeout time.Duration
	pingTimeout  time.Duration
	retryCfg     RetryConfig
}

type RetryConfig struct {
	MaxRetries  int
	MaxInterval time.Duration
}

type Config struct {
	DSN             string
	MaxRows         int
	QueryTimeoutS   int
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
	Retry           RetryConfig
}

func New(cfg Config) (*Client, error) {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, err
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 5
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	idleTime := cfg.ConnMaxIdleTime
	if idleTime <= 0 {
		idleTime = 5 * time.Minute
	}
	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = 5 * time.Second
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(idleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	applyConfigDefaults(&cfg)
	return &Client{
		db:           db,
		maxRows:      cfg.MaxRows,
		queryTimeout: time.Duration(cfg.QueryTimeoutS) * time.Second,
		pingTimeout:  pingTimeout,
		retryCfg:     cfg.Retry,
	}, nil
}

// NewWithDB constructs a Client from an existing *sql.DB.
// This is mainly useful for tests where we use a sqlmock.DB.
func NewWithDB(db *sql.DB, cfg Config) (*Client, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = 5 * time.Second
	}
	applyConfigDefaults(&cfg)
	return &Client{
		db:           db,
		maxRows:      cfg.MaxRows,
		queryTimeout: time.Duration(cfg.QueryTimeoutS) * time.Second,
		pingTimeout:  pingTimeout,
		retryCfg:     cfg.Retry,
	}, nil
}

const (
	defaultMaxRows      = 200
	defaultQueryTimeout = 30 * time.Second
)

// applyConfigDefaults fills in zero-value Config fields with sensible defaults.
// Negative MaxRows is clamped to 0 (caller explicitly wants no rows).
// Zero MaxRows is treated as unset and replaced with the server default.
func applyConfigDefaults(cfg *Config) {
	if cfg.MaxRows < 0 {
		cfg.MaxRows = 0
	} else if cfg.MaxRows == 0 {
		cfg.MaxRows = defaultMaxRows
	}
	if cfg.QueryTimeoutS <= 0 {
		cfg.QueryTimeoutS = int(defaultQueryTimeout.Seconds())
	}
	if cfg.Retry.MaxRetries <= 0 {
		cfg.Retry.MaxRetries = 3
	}
	if cfg.Retry.MaxInterval <= 0 {
		cfg.Retry.MaxInterval = 10 * time.Second
	}
}

func (c *Client) Close() error {
	return c.db.Close()
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.queryTimeout)
}

// ExecWithRetry runs op, retrying on transient MySQL/network errors up to
// RetryConfig.MaxRetries times with exponential backoff. The retry loop is
// bound to ctx: cancellation or deadline immediately stops retries.
func (c *Client) ExecWithRetry(ctx context.Context, op func(context.Context) error) error {
	dc := dbretry.Config{
		MaxRetries:  c.retryCfg.MaxRetries,
		MaxInterval: c.retryCfg.MaxInterval,
	}
	return dbretry.Do(ctx, c.db, dc, c.pingTimeout, func() error {
		return op(ctx)
	})
}

func (c *Client) ListDatabases(ctx context.Context) ([]string, error) {
	var dbs []string
	err := c.ExecWithRetry(ctx, func(ctx context.Context) error {
		ctx, cancel := c.withTimeout(ctx)
		defer cancel()

		rows, err := c.db.QueryContext(ctx, "SHOW DATABASES")
		if err != nil {
			return err
		}
		defer rows.Close()

		dbs = nil // Reset for retry
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			dbs = append(dbs, name)
		}
		return rows.Err()
	})
	return dbs, err
}

func (c *Client) ListTables(ctx context.Context, database string) ([]string, error) {
	if database == "" {
		return nil, fmt.Errorf("database is required")
	}

	dbName, err := util.QuoteIdent(database)
	if err != nil {
		return nil, fmt.Errorf("invalid database name: %w", err)
	}

	var tables []string
	err = c.ExecWithRetry(ctx, func(ctx context.Context) error {
		ctx, cancel := c.withTimeout(ctx)
		defer cancel()

		rows, err := c.db.QueryContext(ctx, "SHOW TABLES FROM "+dbName)
		if err != nil {
			return err
		}
		defer rows.Close()

		tables = nil // Reset for retry
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		return rows.Err()
	})
	return tables, err
}

func (c *Client) DescribeTable(ctx context.Context, database, table string) ([]map[string]any, error) {
	if database == "" || table == "" {
		return nil, fmt.Errorf("database and table are required")
	}

	dbName, err := util.QuoteIdent(database)
	if err != nil {
		return nil, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := util.QuoteIdent(table)
	if err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	var out []map[string]any
	err = c.ExecWithRetry(ctx, func(ctx context.Context) error {
		ctx, cancel := c.withTimeout(ctx)
		defer cancel()

		rows, err := c.db.QueryContext(ctx, "DESCRIBE "+dbName+"."+tableName)
		if err != nil {
			return err
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return err
		}

		out = nil // Reset for retry
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}

			row := map[string]any{}
			for i, col := range cols {
				row[col] = values[i]
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// rejectDML returns an error if sqlText is not a read-only statement.
func rejectDML(sqlText string) error {
	upper := strings.ToUpper(strings.TrimSpace(sqlText))
	if strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") ||
		strings.HasPrefix(upper, "DESC") ||
		strings.HasPrefix(upper, "EXPLAIN") ||
		strings.HasPrefix(upper, "WITH") {
		return nil
	}
	return fmt.Errorf("only read-only statements (SELECT/SHOW/DESCRIBE/EXPLAIN/WITH) are permitted")
}

// RunQuery executes a read-only SQL statement. Only SELECT, SHOW, DESCRIBE,
// EXPLAIN, and WITH (CTE) statements are permitted.
func (c *Client) RunQuery(ctx context.Context, sqlText string, maxRows int) ([]map[string]any, error) {
	if sqlText == "" {
		return nil, fmt.Errorf("sql is required")
	}
	if err := rejectDML(sqlText); err != nil {
		return nil, err
	}
	if maxRows <= 0 || maxRows > c.maxRows {
		maxRows = c.maxRows
	}
	if maxRows < 0 {
		maxRows = 0
	}

	var result []map[string]any
	err := c.ExecWithRetry(ctx, func(ctx context.Context) error {
		ctx, cancel := c.withTimeout(ctx)
		defer cancel()

		rows, err := c.db.QueryContext(ctx, sqlText)
		if err != nil {
			return err
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return err
		}

		result = nil // Reset for retry
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
				return err
			}
			row := map[string]any{}
			for i, col := range cols {
				row[col] = values[i]
			}
			result = append(result, row)
			count++
		}
		return rows.Err()
	})
	return result, err
}
