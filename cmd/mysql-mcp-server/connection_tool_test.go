package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	mysqlclient "github.com/askdba/mysql-mcp-server/internal/mysql"
	"github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolAddConnectionValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       AddConnectionInput
		wantErrSubs string
	}{
		{
			name:        "root user rejected",
			input:       AddConnectionInput{Name: "test-root", DSN: "root:secret@tcp(localhost:3306)/db"},
			wantErrSubs: "security policy: runtime registration of 'root' user is not allowed",
		},
		{
			name:        "root user rejected case variant",
			input:       AddConnectionInput{Name: "test-root2", DSN: "Root:secret@tcp(localhost:3306)/db"},
			wantErrSubs: "security policy: runtime registration of 'root' user is not allowed",
		},
		{
			name:        "empty name",
			input:       AddConnectionInput{Name: "", DSN: "user:pass@tcp(localhost:3306)/db"},
			wantErrSubs: "name and dsn are required",
		},
		{
			name:        "empty DSN",
			input:       AddConnectionInput{Name: "myconn", DSN: ""},
			wantErrSubs: "name and dsn are required",
		},
		{
			name:        "invalid DSN format",
			input:       AddConnectionInput{Name: "myconn", DSN: "not-a-valid-dsn"},
			wantErrSubs: "invalid DSN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewConnectionManager()
			cfg := &config.Config{}
			ctx := context.Background()

			_, _, err := toolAddConnection(ctx, &mcp.CallToolRequest{}, tc.input, cm, cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErrSubs)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubs) {
				t.Errorf("error %q does not contain expected substring %q", err.Error(), tc.wantErrSubs)
			}
		})
	}
}

func TestToolAddConnectionNilManager(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	_, _, err := toolAddConnection(ctx, &mcp.CallToolRequest{}, AddConnectionInput{
		Name: "x",
		DSN:  "user:pass@tcp(localhost:3306)/db",
	}, nil, cfg)
	if err == nil || !strings.Contains(err.Error(), "connection manager not initialized") {
		t.Fatalf("expected connection manager error, got %v", err)
	}
}

func TestToolAddConnectionDuplicateName(t *testing.T) {
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer mockDB.Close()

	cm := NewConnectionManager()
	cfg := &config.Config{}
	ctx := context.Background()

	// Pre-populate like a real registration: same name in connections + configs
	cm.mu.Lock()
	cm.connections["existing"] = mockDB
	cm.configs["existing"] = config.ConnectionConfig{Name: "existing", DSN: "user:pass@tcp(localhost:3306)/db"}
	cm.mu.Unlock()

	input := AddConnectionInput{
		Name: "existing",
		DSN:  "appuser:pass@tcp(localhost:3306)/db",
	}

	_, _, addErr := toolAddConnection(ctx, &mcp.CallToolRequest{}, input, cm, cfg)
	if addErr == nil {
		t.Fatal("expected error for duplicate connection name, got nil")
	}
	if !strings.Contains(addErr.Error(), "already exists") {
		t.Errorf("unexpected error: %v", addErr)
	}
}

func TestToolAddConnectionRejectsUnsafeDSNOptions(t *testing.T) {
	tests := []struct {
		name, dsn, wantErr string
	}{
		{"allowAllFiles", "app:pw@tcp(db.example:3306)/d?allowAllFiles=true", "allowAllFiles is not allowed"},
		{"allowAllFiles numeric", "app:pw@tcp(db.example:3306)/d?allowAllFiles=1", "allowAllFiles is not allowed"},
		{"multiStatements", "app:pw@tcp(db.example:3306)/d?multiStatements=true", "multiStatements is not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewConnectionManager()
			_, _, err := toolAddConnection(context.Background(), &mcp.CallToolRequest{},
				AddConnectionInput{Name: "x", DSN: tc.dsn}, cm, &config.Config{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
			if len(cm.List()) != 0 {
				t.Errorf("rejected DSN must not be registered, got %v", cm.List())
			}
		})
	}
}

func TestCheckRuntimeDSNOptionsAllowsOrdinaryOptions(t *testing.T) {
	cfg, err := mysql.ParseDSN("app:pw@tcp(db.example:3306)/d?parseTime=true&charset=utf8mb4&tls=skip-verify&allowAllFiles=false")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRuntimeDSNOptions(cfg); err != nil {
		t.Fatalf("ordinary options rejected: %v", err)
	}
}

func TestRemoveConnectionDropsRetryClient(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	cm := NewConnectionManager()
	cm.mu.Lock()
	cm.connections["extra"] = db
	cm.configs["extra"] = config.ConnectionConfig{Name: "extra"}
	cm.mu.Unlock()
	client, err := mysqlclient.NewWithDB(db, mysqlclient.Config{MaxRows: 10, QueryTimeoutS: 1})
	if err != nil {
		t.Fatal(err)
	}
	cm.mu.Lock()
	cm.clients["extra"] = client
	cm.mu.Unlock()

	if err := cm.RemoveConnection("extra"); err != nil {
		t.Fatal(err)
	}
	cm.mu.RLock()
	_, left := cm.clients["extra"]
	cm.mu.RUnlock()
	if left {
		t.Fatal("RemoveConnection left the retry client (and its DSN credentials) in cm.clients")
	}
}

func TestSessionConnectionsHaveProcessWideCap(t *testing.T) {
	cm := NewConnectionManager()
	cm.mu.Lock()
	for i := 0; i < maxTotalSessionConnections-1; i++ {
		cm.connections[fmt.Sprintf("%ss%d/c", sessionConnPrefix, i)] = nil
	}
	cm.pendingAdds = map[string]struct{}{sessionConnPrefix + "pending/c": {}} // counts too
	cm.mu.Unlock()

	err := cm.AddConnectionIfAbsentWithPoolConfig(context.Background(),
		config.ConnectionConfig{Name: sessionConnPrefix + "new/c", DSN: "u:p@tcp(127.0.0.1:1)/d"}, &config.Config{})
	if !errors.Is(err, ErrTooManySessionConnections) {
		t.Fatalf("21st session connection: err = %v, want ErrTooManySessionConnections", err)
	}

	// Ordinary (config/stdio) connections are not subject to the session cap.
	err = cm.AddConnectionIfAbsentWithPoolConfig(context.Background(),
		config.ConnectionConfig{Name: "ops", DSN: "u:p@tcp(127.0.0.1:1)/d"}, &config.Config{PingTimeout: 100 * time.Millisecond})
	if errors.Is(err, ErrTooManySessionConnections) {
		t.Fatal("cap must not apply to non-session connections")
	}
}
