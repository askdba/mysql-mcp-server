// internal/util/identifiers.go
package util

import (
	"fmt"
	"strings"
)

// QuoteIdent safely quotes a MySQL identifier, returning an error if the name
// contains potentially dangerous characters.
func QuoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("identifier cannot be empty")
	}
	// Reject identifiers with dangerous characters that could enable SQL injection
	if strings.ContainsAny(name, " \t\n\r;`\\") {
		return "", fmt.Errorf("identifier contains invalid characters: %q", name)
	}
	// Additional check: reject identifiers that are too long (MySQL limit is 64)
	if len(name) > 64 {
		return "", fmt.Errorf("identifier too long: %d characters (max 64)", len(name))
	}
	return "`" + name + "`", nil
}

// MaskDSN hides password in DSN for display.
// DSN format: user:password@tcp(host:port)/database
func MaskDSN(dsn string) string {
	atIdx := strings.Index(dsn, "@")
	if atIdx == -1 {
		return dsn
	}
	colonIdx := strings.Index(dsn, ":")
	if colonIdx == -1 || colonIdx > atIdx {
		return dsn
	}
	return dsn[:colonIdx+1] + "****" + dsn[atIdx:]
}

// NormalizeValue converts raw DB value into something JSON-friendly.
func NormalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	default:
		return x
	}
}

// TruncateQuery truncates a query string to maxLen characters.
func TruncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen] + "..."
}
