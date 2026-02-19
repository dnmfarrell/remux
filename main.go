//go:build darwin || linux

package main

import (
	"fmt"
	"os"
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
