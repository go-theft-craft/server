package conn

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

func TestV47SatisfiesTheDialect(t *testing.T) {
	var _ Dialect = (*v47Dialect)(nil)
}

// readV47 reads a decoded protocol 47 packet the way the play handler does,
// through the dialect, and returns the action it becomes. A test that builds a
// generated packet and hands the result to a handler exercises the same
// translation a real client's packet goes through.
func readV47[T Action](value any) T {
	action, known := newV47Dialect().Read(protocol.Packet{Value: value})
	if !known {
		panic(fmt.Sprintf("the protocol 47 dialect does not know %T", value))
	}
	typed, ok := action.(T)
	if !ok {
		panic(fmt.Sprintf("%T read as %T", value, action))
	}

	return typed
}

// TestThePlayPathNamesNoVersion is the seam's own gate. A generated version
// package imported outside the dialect files is a version leaking back into the
// path the dialect exists to make neutral.
//
// The connection's own files that run before play are allowed it, each for a
// stated reason: they choose and drive the session a version's play path runs
// on, which is the job the handshake-boundary session swap takes over when a
// second version is served. Nothing on the list is a play handler.
func TestThePlayPathNamesNoVersion(t *testing.T) {
	allowed := map[string]map[string]string{
		".": {
			"dialect_v47.go":       "the protocol 47 dialect",
			"stream.go":            "builds the session every connection starts on",
			"connection.go":        "names the session's states to dispatch on them",
			"handler_handshake.go": "reads the handshake, before any version is chosen",
			"handler_status.go":    "answers the server list, which never reaches play",
		},
		filepath.Join("..", "player"): {
			"dialect_v47.go": "the protocol 47 spelling of the entity packets",
		},
	}

	for dir, allow := range allowed {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files in %s", dir)
		}

		fset := token.NewFileSet()
		for _, file := range files {
			base := filepath.Base(file)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			if _, ok := allow[base]; ok {
				continue
			}

			parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			for _, spec := range parsed.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("%s: import %s: %v", file, spec.Path.Value, err)
				}
				if strings.Contains(path, "/generated/java/") {
					t.Errorf("%s imports %s: a play path file names a version", file, path)
				}
			}
		}
	}
}
