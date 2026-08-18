// Package examples_test builds each example and checks it reaches the point
// of listening.
//
// It is deliberately shallow. Deep behavior is covered by the parent module's
// tests and by the interoperability lane; what this catches is an example that
// stopped compiling or stopped starting after an API change, which is the way
// examples rot.
package examples_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const startTimeout = 30 * time.Second

// metricsAddr is loopback, like the example's own default. A metrics endpoint
// is not a public one.
const metricsAddr = "127.0.0.1:25805"

// assertMetricsAnswer polls the observed example's endpoint until it serves
// the server's own metrics.
func assertMetricsAnswer(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		body, err := fetch("http://" + metricsAddr + "/metrics")
		if err == nil && strings.Contains(body, "minecraft_") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("the observed example never served its own metrics")
}

func fetch(url string) (string, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx // a fixed loopback URL in a test
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	return string(body), err
}

func TestEachExampleBuildsAndStarts(t *testing.T) {
	for _, name := range []string{"minimal", "flat", "vanilla", "custom", "observed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			binary := filepath.Join(t.TempDir(), name)

			build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./"+name)
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build %s: %v\n%s", name, err, out)
			}

			ctx, cancel := context.WithTimeout(t.Context(), startTimeout)
			defer cancel()

			// Each example binds the default port, so the subtests give them
			// distinct ones rather than running one at a time.
			run := exec.CommandContext(ctx, binary, arguments(t, name)...)
			stdout, err := run.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			if err := run.Start(); err != nil {
				t.Fatalf("start %s: %v", name, err)
			}
			defer func() {
				_ = run.Process.Kill()
				_ = run.Wait()
			}()

			scanner := bufio.NewScanner(stdout)
			started := false
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "server started") {
					started = true

					break
				}
			}
			if !started {
				t.Fatalf("%s never logged that it started", name)
			}

			if name == "observed" {
				// The reason this example exists rather than a README
				// snippet: a sink that stopped compiling or stopped being
				// wired shows up here as an endpoint that answers nothing.
				assertMetricsAnswer(t)
			}
		})
	}
}

// arguments gives each example a distinct port so the subtests can run in
// parallel, and keeps vanilla's data and its world off the developer's disk
// and out of the minutes a 500-chunk pre-generation would cost.
func arguments(t *testing.T, name string) []string {
	t.Helper()

	switch name {
	case "vanilla":
		return []string{"-port", "25701", "-data-dir", t.TempDir(), "-world-radius", "0"}
	case "flat":
		return []string{"-port", "25702"}
	case "custom":
		return []string{"-port", "25704"}
	case "observed":
		return []string{
			"-port", "25705", "-data-dir", t.TempDir(),
			"-world-radius", "0", "-metrics", metricsAddr,
		}
	default:
		return []string{"-port", "25703"}
	}
}
