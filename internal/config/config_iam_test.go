package config

import (
	"os"
	"testing"
)

func TestLoadGlobalIAMFromEnv_Disabled(t *testing.T) {
	os.Unsetenv("MYSQL_IAM_ENABLED")
	result := loadGlobalIAMFromEnv()
	if result != nil {
		t.Fatalf("expected nil when MYSQL_IAM_ENABLED unset, got %+v", result)
	}
}

func TestLoadGlobalIAMFromEnv_Enabled(t *testing.T) {
	t.Setenv("MYSQL_IAM_ENABLED", "1")
	t.Setenv("MYSQL_IAM_REGION", "us-west-2")

	result := loadGlobalIAMFromEnv()
	if result == nil {
		t.Fatal("expected non-nil IAMConfig")
	}
	if !result.Enabled {
		t.Error("expected Enabled to be true")
	}
	if result.Region != "us-west-2" {
		t.Errorf("expected Region us-west-2, got %q", result.Region)
	}
}

func TestLoadConnections_IAMPropagated(t *testing.T) {
	t.Setenv("MYSQL_IAM_ENABLED", "1")
	t.Setenv("MYSQL_IAM_REGION", "us-east-1")
	t.Setenv("MYSQL_DSN", "dbuser@tcp(cluster.us-east-1.rds.amazonaws.com:3306)/mydb")
	t.Cleanup(func() {
		os.Unsetenv("MYSQL_IAM_ENABLED")
		os.Unsetenv("MYSQL_IAM_REGION")
		os.Unsetenv("MYSQL_DSN")
	})

	conns, err := loadConnections()
	if err != nil {
		t.Fatalf("loadConnections error: %v", err)
	}
	if len(conns) == 0 {
		t.Fatal("expected at least one connection")
	}
	if conns[0].IAM == nil {
		t.Fatal("expected IAM config to be propagated to connection")
	}
	if !conns[0].IAM.Enabled {
		t.Error("expected IAM.Enabled to be true")
	}
	if conns[0].IAM.Region != "us-east-1" {
		t.Errorf("expected Region us-east-1, got %q", conns[0].IAM.Region)
	}
}
