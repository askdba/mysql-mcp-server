package iamauth

import (
	"context"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestNewConnector_EmptyRegion(t *testing.T) {
	cfg := mysql.NewConfig()
	cfg.Addr = "cluster.us-east-1.rds.amazonaws.com:3306"
	cfg.User = "dbuser"

	_, err := NewConnector(context.Background(), cfg, Config{Region: ""})
	if err == nil {
		t.Fatal("expected error for empty Region, got nil")
	}
}

func TestNewConnector_ReturnsConnector(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "fake-session-token")
	t.Setenv("AWS_REGION", "us-east-1")

	cfg := mysql.NewConfig()
	cfg.Addr = "cluster.us-east-1.rds.amazonaws.com:3306"
	cfg.User = "dbuser"

	connector, err := NewConnector(context.Background(), cfg, Config{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("NewConnector returned error: %v", err)
	}
	if connector == nil {
		t.Fatal("expected non-nil connector")
	}
}

func TestNewConnector_AllowCleartextPasswords(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_REGION", "us-east-1")

	cfg := mysql.NewConfig()
	cfg.Addr = "cluster.us-east-1.rds.amazonaws.com:3306"
	cfg.User = "dbuser"

	_, err := NewConnector(context.Background(), cfg, Config{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("NewConnector returned error: %v", err)
	}
	if !cfg.AllowCleartextPasswords {
		t.Fatal("expected AllowCleartextPasswords to be true")
	}
}
