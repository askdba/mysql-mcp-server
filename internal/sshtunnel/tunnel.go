// Package sshtunnel provides SSH port forwarding for MySQL connections through a bastion host.
package sshtunnel

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const defaultSSHPort = 22

// Config holds SSH bastion connection parameters.
type Config struct {
	Host    string // Bastion hostname
	User    string // SSH username
	KeyPath string // Path to private key file
	Port    int    // SSH port (0 = default 22)

	// InsecureIgnoreHostKey disables host key verification (MITM risk). Default false:
	// verification is strict unless this is true (opt-in insecure).
	InsecureIgnoreHostKey bool

	// KnownHostsPath is the OpenSSH known_hosts file. Empty defaults to ~/.ssh/known_hosts
	// when verifying (after tilde expansion).
	KnownHostsPath string

	// HostKeyFingerprint pins the server key (SHA256:... or MD5:... OpenSSH-style).
	// When set, KnownHostsPath is ignored for the callback (fingerprint wins).
	HostKeyFingerprint string
}

// expandTilde returns path with a leading "~" or "~/" expanded to the user's home directory.
func expandTilde(path string) (string, error) {
	if path == "" || (path != "~" && !strings.HasPrefix(path, "~/")) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~ in path: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func buildHostKeyCallback(cfg Config) (ssh.HostKeyCallback, error) {
	if cfg.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	if fp := strings.TrimSpace(cfg.HostKeyFingerprint); fp != "" {
		return fingerprintHostKeyCallback(fp), nil
	}

	khPath := strings.TrimSpace(cfg.KnownHostsPath)
	if khPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ssh host key verification: resolve home for default known_hosts: %w", err)
		}
		khPath = filepath.Join(home, ".ssh", "known_hosts")
	}
	var err error
	khPath, err = expandTilde(khPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(khPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ssh host key verification: known_hosts file %q not found (set MYSQL_SSH_KNOWN_HOSTS, ssh.known_hosts, or MYSQL_SSH_HOST_KEY_FINGERPRINT / ssh_host_key_fingerprint, or opt in to insecure mode with MYSQL_SSH_STRICT_HOST_KEY_CHECKING=false)", khPath)
		}
		return nil, fmt.Errorf("ssh host key verification: stat known_hosts %q: %w", khPath, err)
	}
	cb, err := knownhosts.New(khPath)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts %q: %w", khPath, err)
	}
	return cb, nil
}

// fingerprintHostKeyCallback accepts only a key matching the expected OpenSSH fingerprint.
func fingerprintHostKeyCallback(expected string) ssh.HostKeyCallback {
	expNorm := normalizeFingerprintInput(expected)
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if fingerprintEqual(key, expNorm) {
			return nil
		}
		return fmt.Errorf("ssh: host key fingerprint mismatch (expected %s)", strings.TrimSpace(expected))
	}
}

type fingerprintNorm struct {
	algo string // "sha256" or "md5"
	raw  string // base64 (sha256) or hex no colons (md5)
}

func normalizeFingerprintInput(s string) fingerprintNorm {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return fingerprintNorm{algo: "sha256", raw: ""}
	}
	// Split algorithm prefix (case-insensitive) from payload; do not alter SHA256 base64 case.
	if i := strings.IndexByte(s, ':'); i >= 0 {
		prefix := strings.ToLower(s[:i])
		payload := s[i+1:]
		switch prefix {
		case "sha256":
			return fingerprintNorm{algo: "sha256", raw: payload}
		case "md5":
			return fingerprintNorm{algo: "md5", raw: strings.ReplaceAll(strings.ToLower(payload), ":", "")}
		}
	}
	// Bare MD5 hex with colons (OpenSSH style), not a URL path.
	if strings.Contains(s, ":") && !strings.Contains(s, "/") {
		return fingerprintNorm{algo: "md5", raw: strings.ReplaceAll(strings.ToLower(s), ":", "")}
	}
	return fingerprintNorm{algo: "sha256", raw: s}
}

func fingerprintEqual(key ssh.PublicKey, exp fingerprintNorm) bool {
	switch exp.algo {
	case "sha256":
		sum := sha256.Sum256(key.Marshal())
		got := base64.RawStdEncoding.EncodeToString(sum[:])
		return subtleConstantTimeEq(got, exp.raw)
	case "md5":
		sum := md5.Sum(key.Marshal()) //nolint:gosec // MD5 used only for SSH legacy fingerprint comparison
		got := fmt.Sprintf("%x", sum[:])
		return subtleConstantTimeEq(got, exp.raw)
	default:
		return false
	}
}

func subtleConstantTimeEq(a, b string) bool {
	return len(a) == len(b) && subtleConstantTimeBytes([]byte(a), []byte(b))
}

func subtleConstantTimeBytes(x, y []byte) bool {
	if len(x) != len(y) {
		return false
	}
	var v byte
	for i := range x {
		v |= x[i] ^ y[i]
	}
	return v == 0
}

// Tunnel starts a local listener that forwards connections to remoteAddr via SSH.
// Returns the local address (e.g. "127.0.0.1:12345") and a close function.
// remoteAddr should be the MySQL server address (e.g. "db.example.com:3306").
// KeyPath may start with "~/" or be "~" to mean the current user's home directory.
func Tunnel(cfg Config, remoteAddr string) (localAddr string, closeFn func(), err error) {
	if cfg.Host == "" || cfg.User == "" || cfg.KeyPath == "" {
		return "", nil, fmt.Errorf("ssh tunnel requires host, user, and key_path")
	}

	keyPath, err := expandTilde(cfg.KeyPath)
	if err != nil {
		return "", nil, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return "", nil, fmt.Errorf("read SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return "", nil, fmt.Errorf("parse SSH key: %w", err)
	}

	port := cfg.Port
	if port <= 0 {
		port = defaultSSHPort
	}
	sshAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))

	hostKeyCB, err := buildHostKeyCallback(cfg)
	if err != nil {
		return "", nil, err
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCB,
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		return "", nil, fmt.Errorf("ssh dial: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return "", nil, fmt.Errorf("listen: %w", err)
	}

	localAddr = listener.Addr().String()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go proxy(client, remoteAddr, conn)
		}
	}()

	closeFn = func() {
		listener.Close()
		client.Close()
	}
	return localAddr, closeFn, nil
}

// proxy opens a connection to remoteAddr via the SSH client and copies data both ways.
func proxy(client *ssh.Client, remoteAddr string, local net.Conn) {
	defer local.Close()
	remote, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remote.Close()
	go func() { _, _ = io.Copy(remote, local) }()
	_, _ = io.Copy(local, remote)
}
