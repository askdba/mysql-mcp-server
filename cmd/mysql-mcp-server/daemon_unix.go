//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// maybeDaemonize forks the process and exits the parent when --daemon is set.
// The child process is started in a new session (detached from terminal).
// Only makes sense when running in HTTP mode; caller should avoid passing --daemon for stdio MCP.
func maybeDaemonize(parsed parsedArgs) {
	if !parsed.daemon {
		return
	}
	// Build child args: same as current but without --daemon and -d
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if a == "--daemon" || a == "-d" {
			continue
		}
		args = append(args, a)
	}
	cmd := exec.CommandContext(context.Background(), os.Args[0], args...) // #nosec G204 G702 -- re-execs this binary with its own args for --daemon
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: failed to start child: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
