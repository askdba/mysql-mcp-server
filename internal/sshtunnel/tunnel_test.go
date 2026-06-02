package sshtunnel

import (
	"path/filepath"
	"testing"
)

func TestTunnel_RequiresHostUserKeyPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing host", Config{User: "u", KeyPath: "/tmp/k"}},
		{"missing user", Config{Host: "h", KeyPath: "/tmp/k"}},
		{"missing key_path", Config{Host: "h", User: "u"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Tunnel(tt.cfg, "db:3306")
			if err == nil {
				t.Error("expected error when required SSH config is missing")
			}
		})
	}
}

func TestTunnel_InvalidKeyPath(t *testing.T) {
	_, _, err := Tunnel(Config{
		Host:    "bastion.example.com",
		User:    "deploy",
		KeyPath: "/nonexistent/key",
		Port:    22,
	}, "mysql.internal:3306")
	if err == nil {
		t.Error("expected error for nonexistent key file")
	}
}

func TestBuildHostKeyCallback_Insecure(t *testing.T) {
	t.Parallel()
	cb, err := buildHostKeyCallback(Config{InsecureIgnoreHostKey: true})
	if err != nil || cb == nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}
}

func TestNormalizeFingerprintInput_SHA256PreservesBase64Case(t *testing.T) {
	t.Parallel()
	n := normalizeFingerprintInput("SHA256:AbCd+EfGh")
	if n.algo != "sha256" || n.raw != "AbCd+EfGh" {
		t.Fatalf("got %+v", n)
	}
}

func TestNormalizeFingerprintInput_MD5PrefixStripsColons(t *testing.T) {
	t.Parallel()
	n := normalizeFingerprintInput("MD5:aa:bb:cc:dd")
	if n.algo != "md5" || n.raw != "aabbccdd" {
		t.Fatalf("got %+v", n)
	}
}

func TestBuildHostKeyCallback_StrictMissingKnownHosts(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "no_such_known_hosts")
	_, err := buildHostKeyCallback(Config{KnownHostsPath: p})
	if err == nil {
		t.Fatal("expected error when known_hosts file is missing")
	}
}

func TestTunnel_ExpandTildeKeyPath(t *testing.T) {
	// KeyPath "~/nonexistent_key" is expanded to $HOME/nonexistent_key, then ReadFile fails
	_, _, err := Tunnel(Config{
		Host:    "bastion.example.com",
		User:    "deploy",
		KeyPath: "~/nonexistent_ssh_key_12345",
		Port:    22,
	}, "mysql.internal:3306")
	if err == nil {
		t.Error("expected error when key path expands to nonexistent file")
	}
}
