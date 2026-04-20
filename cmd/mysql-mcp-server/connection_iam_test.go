package main

import (
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestApplyIAMTLSRequirement_AddsTLS(t *testing.T) {
	dsn := "dbuser@tcp(cluster.us-east-1.rds.amazonaws.com:3306)/mydb"
	result, err := applyIAMTLSRequirement(dsn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg, err := mysql.ParseDSN(result)
	if err != nil {
		t.Fatalf("failed to parse result DSN: %v", err)
	}
	if cfg.TLSConfig != "true" {
		t.Errorf("expected TLSConfig=true, got %q", cfg.TLSConfig)
	}
	if !cfg.AllowCleartextPasswords {
		t.Error("expected AllowCleartextPasswords=true")
	}
}

func TestApplyIAMTLSRequirement_PreservesExistingTLS(t *testing.T) {
	dsn := "dbuser@tcp(cluster.us-east-1.rds.amazonaws.com:3306)/mydb?tls=skip-verify"
	result, err := applyIAMTLSRequirement(dsn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg, err := mysql.ParseDSN(result)
	if err != nil {
		t.Fatalf("failed to parse result DSN: %v", err)
	}
	if cfg.TLSConfig != "skip-verify" {
		t.Errorf("expected TLSConfig=skip-verify to be preserved, got %q", cfg.TLSConfig)
	}
}

func TestApplyIAMTLSRequirement_InvalidDSN(t *testing.T) {
	_, err := applyIAMTLSRequirement("not a valid dsn %%%%")
	if err == nil {
		t.Fatal("expected error for invalid DSN, got nil")
	}
}
