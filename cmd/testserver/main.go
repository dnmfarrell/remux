// testserver is a local WebSocket server for testing remux.
//
// It accepts a WebSocket connection and provides an interactive prompt
// where you can type text that gets injected into the remux PTY.
// PTY output received from remux is printed to stdout.
//
// Usage:
//   go run ./cmd/testserver
//   # In another terminal:
//   TETHER_WS_URL=ws://localhost:8080/ws ./remux

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
	"nhooyr.io/websocket"
)

var (
	mu    sync.Mutex
	conns []*websocket.Conn

	// terminal is the shared term.Terminal for all output once raw mode is active.
	// Protected by termMu. Nil until readStdin sets it up.
	termMu sync.Mutex
	terminal *term.Terminal
)

// writeOutput writes a line to the terminal. If the terminal is in raw mode,
// it writes through term.Terminal (which handles \n → \r\n). Otherwise it
// writes directly to stdout.
func writeOutput(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	termMu.Lock()
	t := terminal
	termMu.Unlock()
	if t != nil {
		fmt.Fprintln(t, msg)
	} else {
		fmt.Println(msg)
	}
}

func main() {
	addr := ":8080"
	if len(os.Args) > 1 {
		addr = ":" + os.Args[1]
	} else if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWS)
	srv := &http.Server{Addr: addr, Handler: mux}

	fmt.Printf("testserver listening on %s\n", addr)
	fmt.Println("Waiting for remux to connect...")
	fmt.Println()

	// Read lines from stdin and broadcast to all connected clients.
	// When readStdin returns (Ctrl-D / EOF), shut down the server.
	go func() {
		readStdin()
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		writeOutput("accept: %v", err)
		return
	}

	mu.Lock()
	conns = append(conns, conn)
	n := len(conns)
	mu.Unlock()

	writeOutput("client connected (%d total)", n)
	writeOutput("Type text and press Enter to inject into the PTY.")
	writeOutput("Escape sequences: \\n (newline), \\t (tab), \\x03 (Ctrl-C), \\x04 (Ctrl-D)")
	writeOutput("PTY output from remux will appear below.")

	// Read messages from remux (PTY output) until disconnect
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			break
		}
		termMu.Lock()
		t := terminal
		termMu.Unlock()
		if t != nil {
			t.Write(data)
		} else {
			os.Stdout.Write(data)
		}
	}

	mu.Lock()
	for i, c := range conns {
		if c == conn {
			conns = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	n = len(conns)
	mu.Unlock()

	writeOutput("client disconnected (%d remaining)", n)
}

func readStdin() {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Printf("failed to set raw mode: %v, falling back to cooked", err)
		readStdinCooked()
		return
	}
	defer term.Restore(fd, oldState)

	t := term.NewTerminal(os.Stdin, "> ")
	termMu.Lock()
	terminal = t
	termMu.Unlock()

	for {
		line, err := t.ReadLine()
		if err != nil {
			return
		}
		if line == "" {
			continue
		}

		data := unescape(line)

		mu.Lock()
		alive := conns[:0]
		for _, c := range conns {
			err := c.Write(context.Background(), websocket.MessageText, data)
			if err != nil {
				fmt.Fprintf(t, "write error: %v\n", err)
				c.Close(websocket.StatusGoingAway, "write error")
			} else {
				alive = append(alive, c)
			}
		}
		sent := len(alive)
		conns = alive
		mu.Unlock()

		if sent == 0 {
			fmt.Fprintf(t, "(no clients connected)\n")
		} else {
			fmt.Fprintf(t, "sent %d bytes to %d client(s)\n", len(data), sent)
		}
	}
}

// readStdinCooked is the fallback when stdin isn't a terminal (e.g. piped input).
func readStdinCooked() {
	var line string
	fmt.Print("> ")
	for {
		_, err := fmt.Scanln(&line)
		if err != nil {
			return
		}
		if line == "" {
			fmt.Print("> ")
			continue
		}

		data := unescape(line)

		mu.Lock()
		alive := conns[:0]
		for _, c := range conns {
			err := c.Write(context.Background(), websocket.MessageText, data)
			if err != nil {
				log.Printf("write error: %v", err)
				c.Close(websocket.StatusGoingAway, "write error")
			} else {
				alive = append(alive, c)
			}
		}
		sent := len(alive)
		conns = alive
		mu.Unlock()

		if sent == 0 {
			fmt.Println("(no clients connected)")
		} else {
			fmt.Printf("sent %d bytes to %d client(s)\n", len(data), sent)
		}
		fmt.Print("> ")
	}
}

// unescape replaces common escape sequences in the input string.
func unescape(s string) []byte {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\r", "\r")
	s = strings.ReplaceAll(s, "\\x03", "\x03")
	s = strings.ReplaceAll(s, "\\x04", "\x04")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return []byte(s)
}
