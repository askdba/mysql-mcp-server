// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DSN           string
	MaxRows       int
	QueryTimeoutS int
}

func Load() (*Config, error) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("MYSQL_DSN env var is required, e.g. user:pass@tcp(127.0.0.1:3306)/dbname?parseTime=true")
	}

	maxRows := readIntWithDefault("MYSQL_MCP_MAX_ROWS", 200)
	qTimeout := readIntWithDefault("MYSQL_MCP_QUERY_TIMEOUT_S", 10)

	return &Config{
		DSN:           dsn,
		MaxRows:       maxRows,
		QueryTimeoutS: qTimeout,
	}, nil
}

func readIntWithDefault(env string, def int) int {
	if v := os.Getenv(env); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
