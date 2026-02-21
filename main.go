//go:build darwin || linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
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

	// Generate a relay ID and append it to the WebSocket URL
	relayID := generateUUID()
	if wsURL != "" {
		u, err := url.Parse(wsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "remux: bad REMUX_WS_URL: %v\n", err)
			os.Exit(1)
		}
		q := u.Query()
		q.Set("relay_id", relayID)
		u.RawQuery = q.Encode()
		wsURL = u.String()
	}

	// Enroll session if WebSocket relay is configured
	if wsURL != "" {
		if err := enrollSession(wsURL, wsToken, relayID); err != nil {
			fmt.Fprintf(os.Stderr, "remux: session enrollment failed: %v\n", err)
			os.Exit(1)
		}
	}

	r, err := New(command, args, wsURL, wsToken, wsMode, relayID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "remux: %v\n", err)
		os.Exit(1)
	}

	if err := r.Run(); err != nil {
		os.Exit(1)
	}
}

// enrollSession registers this session with the Greenlight server and blocks
// until the user approves it on their phone. Returns an error if rejected or timed out.
func enrollSession(wsURL, deviceID, sessionID string) error {
	// Derive HTTPS base URL from the WebSocket URL
	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("bad WebSocket URL: %w", err)
	}
	scheme := "https"
	if u.Scheme == "ws" {
		scheme = "http"
	}
	enrollURL := fmt.Sprintf("%s://%s/session/enroll", scheme, u.Host)

	body, err := json.Marshal(map[string]string{
		"device_id":  deviceID,
		"session_id": sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	client := &http.Client{Timeout: 65 * time.Second}
	resp, err := client.Post(enrollURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("enrollment request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("enrollment rejected (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		Approved bool   `json:"approved"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	if !result.Approved {
		if result.Message != "" {
			return fmt.Errorf("session enrollment %s", result.Message)
		}
		return fmt.Errorf("session enrollment rejected")
	}

	log.Printf("Session %s enrolled successfully", sessionID)
	return nil
}

func generateUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
