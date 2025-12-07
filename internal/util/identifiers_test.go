// internal/util/identifiers_test.go
package util

import (
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{"valid simple", "users", "`users`", false},
		{"valid with underscore", "user_accounts", "`user_accounts`", false},
		{"valid with numbers", "table123", "`table123`", false},
		{"empty string", "", "", true},
		{"contains space", "user accounts", "", true},
		{"contains semicolon", "users;", "", true},
		{"contains backtick", "users`drop", "", true},
		{"contains tab", "users\ttable", "", true},
		{"contains newline", "users\ntable", "", true},
		{"contains backslash", "users\\table", "", true},
		{"too long", string(make([]byte, 65)), "", true},
		{"max length (64)", string(make([]byte, 64)), "`" + string(make([]byte, 64)) + "`", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuoteIdent(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("QuoteIdent() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("QuoteIdent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"standard DSN",
			"user:password@tcp(localhost:3306)/db",
			"user:****@tcp(localhost:3306)/db",
		},
		{
			"no password",
			"user@tcp(localhost:3306)/db",
			"user@tcp(localhost:3306)/db",
		},
		{
			"no @ symbol",
			"invalid-dsn",
			"invalid-dsn",
		},
		{
			"empty password",
			"user:@tcp(localhost:3306)/db",
			"user:****@tcp(localhost:3306)/db",
		},
		{
			"special chars in password (colon)",
			"user:p:ss:word@tcp(localhost:3306)/db",
			"user:****@tcp(localhost:3306)/db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskDSN(tt.input)
			if got != tt.want {
				t.Errorf("MaskDSN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  interface{}
	}{
		{"nil value", nil, nil},
		{"byte slice", []byte("hello"), "hello"},
		{"string", "hello", "hello"},
		{"int", 42, 42},
		{"float", 3.14, 3.14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeValue(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateQuery(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		maxLen int
		want   string
	}{
		{"short query", "SELECT 1", 100, "SELECT 1"},
		{"exact length", "SELECT 1", 8, "SELECT 1"},
		{"truncated", "SELECT * FROM users WHERE id = 1", 10, "SELECT * F..."},
		{"zero max", "SELECT 1", 0, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateQuery(tt.query, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}
