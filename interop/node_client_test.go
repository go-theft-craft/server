// Package interop verifies the Go server against the pinned Node
// minecraft-protocol client over loopback TCP.
//
// It is the lane that catches what a Go-to-Go test cannot: two of our own
// implementations can agree with each other and with no real client. The Node
// package is pinned to the same 1.66.2 that minecraft-protocol's own lane
// uses, so the two repositories test against the same thing.
//
// Every process started here is bound to a timeout and cleaned up, and nothing
// binds or dials outside 127.0.0.1.
package interop

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/server/internal/server"
	"github.com/go-theft-craft/server/internal/server/config"
)

const (
	loopback      = "127.0.0.1"
	clientTimeout = 60 * time.Second
)

// clientEvent is one newline-delimited JSON message from the Node client.
type clientEvent struct {
	Event      string `json:"event"`
	Message    string `json:"message"`
	Threshold  int    `json:"threshold"`
	Bytes      int    `json:"bytes"`
	Compressed bool   `json:"compressed"`
	LevelType  string `json:"levelType"`
}

// freePort reserves an ephemeral loopback port and releases it.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

// startServer runs the real server on a loopback port until the test ends.
func startServer(t *testing.T, threshold int) int {
	t.Helper()

	port := freePort(t)

	settings := config.DefaultConfig()
	settings.Port = port
	settings.OnlineMode = false
	settings.CompressionThreshold = threshold
	// A flat world with no pre-generation: this lane tests the connection,
	// not terrain generation, and pre-generating a 500-chunk radius would
	// dominate its runtime.
	settings.GeneratorType = config.GeneratorFlat
	settings.WorldRadius = 0
	settings.ViewDistance = 2
	settings.AutoSaveMinutes = 0

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	settings.PrivateKey = key

	// No storage: nothing this lane does should touch the developer's world.
	instance, err := server.New(settings, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		if err := instance.Start(ctx); err != nil {
			t.Errorf("server exited: %v", err)
		}
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("the server did not stop")
		}
	})

	waitForListener(t, port)

	return port
}

func waitForListener(t *testing.T, port int) {
	t.Helper()

	address := net.JoinHostPort(loopback, strconv.Itoa(port))
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = conn.Close()

			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("the server never listened on %s", address)
}

// runClient runs the Node client against the server and returns what it saw.
func runClient(t *testing.T, port int, username string) []clientEvent {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node is not available: %v", err)
	}

	root, err := filepath.Abs("node")
	if err != nil {
		t.Fatalf("resolve harness directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
		t.Skipf("run `task test:interop` so the pinned client is installed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, node, filepath.Join(root, "client.mjs"),
		"--port", strconv.Itoa(port),
		"--username", username,
	)
	command.Dir = root

	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	if err := command.Start(); err != nil {
		t.Fatalf("start client: %v", err)
	}

	var events []clientEvent

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var event clientEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode client event %q: %v", scanner.Text(), err)
		}

		events = append(events, event)
	}

	diagnostics, _ := io.ReadAll(stderr)

	if err := command.Wait(); err != nil {
		t.Fatalf("client failed: %v\nevents: %+v\nstderr: %s", err, events, diagnostics)
	}

	return events
}

func findEvent(events []clientEvent, name string) (clientEvent, bool) {
	for _, event := range events {
		if event.Event == name {
			return event, true
		}
	}

	return clientEvent{}, false
}

// A real 1.8.8 client must reach play and receive chunk data, with
// compression disabled and with it negotiated at the default threshold.
func TestNodeClientJoins(t *testing.T) {
	cases := []struct {
		name           string
		threshold      int
		wantCompressed bool
	}{
		{name: "compression disabled", threshold: -1, wantCompressed: false},
		{name: "compression at 256", threshold: 256, wantCompressed: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			port := startServer(t, testCase.threshold)
			events := runClient(t, port, "InteropBot")

			if _, ok := findEvent(events, "login"); !ok {
				t.Fatalf("the client never joined: %+v", events)
			}

			chunk, ok := findEvent(events, "map_chunk")
			if !ok {
				t.Fatalf("the client received no chunk data: %+v", events)
			}
			if chunk.Bytes == 0 {
				t.Fatal("chunk data arrived empty")
			}

			compress, sawCompress := findEvent(events, "compress")
			if sawCompress != testCase.wantCompressed {
				t.Fatalf("compression negotiated = %v, want %v", sawCompress, testCase.wantCompressed)
			}
			if testCase.wantCompressed && compress.Threshold != testCase.threshold {
				t.Fatalf("threshold = %d, want %d", compress.Threshold, testCase.threshold)
			}

			joined, ok := findEvent(events, "joined")
			if !ok {
				t.Fatalf("the client did not report a completed join: %+v", events)
			}
			if joined.Compressed != testCase.wantCompressed {
				t.Fatalf("client saw compression = %v, want %v", joined.Compressed, testCase.wantCompressed)
			}
		})
	}
}

// The harness must never reach past loopback.
func TestHarnessIsLoopbackOnly(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("node", "client.mjs"))
	if err != nil {
		t.Fatalf("read client.mjs: %v", err)
	}

	if !strings.Contains(string(source), "'127.0.0.1'") {
		t.Fatal("the client harness must pin the loopback address")
	}
	if strings.Contains(string(source), "auth: 'microsoft'") ||
		strings.Contains(string(source), "sessionserver") {
		t.Fatal("the client harness must not contact a session server")
	}
}
