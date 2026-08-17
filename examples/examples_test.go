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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const startTimeout = 30 * time.Second

func TestEachExampleBuildsAndStarts(t *testing.T) {
	for _, name := range []string{"minimal", "flat", "vanilla"} {
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
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "server started") {
					return
				}
			}

			t.Fatalf("%s never logged that it started", name)
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
	default:
		return []string{"-port", "25703"}
	}
}
