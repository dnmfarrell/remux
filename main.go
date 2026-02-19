//go:build darwin || linux

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	// Default child command is the user's shell
	command := os.Getenv("SHELL")
	if command == "" {
		command = "/bin/sh"
	}
	var args []string

	// Parse arguments: everything after "--" is the child command
	for i, arg := range os.Args[1:] {
		if arg == "--" {
			rest := os.Args[i+2:]
			if len(rest) > 0 {
				command = rest[0]
				args = rest[1:]
			}
			break
		}
	}

	// Log to file to avoid polluting the terminal
	if logPath := os.Getenv("REMUX_LOG"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			log.SetOutput(f)
		}
	} else {
		logPath = filepath.Join(os.TempDir(), fmt.Sprintf("remux-%d.log", os.Getpid()))
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			log.SetOutput(f)
		}
	}

	wsURL := os.Getenv("REMUX_WS_URL")
	wsToken := os.Getenv("REMUX_WS_TOKEN")
	wsMode, err := ParseWSMode(os.Getenv("REMUX_WS_MODE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "remux: %v\n", err)
		os.Exit(1)
	}

	r, err := New(command, args, wsURL, wsToken, wsMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "remux: %v\n", err)
		os.Exit(1)
	}

	if err := r.Run(); err != nil {
		os.Exit(1)
	}
}
