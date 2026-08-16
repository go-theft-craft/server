# M11.1 Framework Shape Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `server` from an application into an importable framework: a public `server` package constructed with functional options, the `Store` and `Observer` seams declared, and `cmd/server` replaced by an `examples/` module holding three programs that compose the pieces differently.

**Architecture:** `internal/server` becomes the public `server` package and `internal/server/config` becomes the public `config` package, so an outside module can construct and run a server. `New` takes functional options instead of three positional arguments. The server depends on interfaces it declares itself, never on the concrete `storage` type, and the application wires the implementations together. Network counters come from `minecraft-protocol`'s existing observation sink rather than from new instrumentation.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` as a released module, vendored dependencies.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/server`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- This repository is public. Do not name the private proxy project, its protocol, or its codename in any file or commit message. Refer to it by role only.
- **This milestone changes no behavior.** Every task is a restructure, an interface extraction, or an addition that is off by default. The byte-parity fixtures and the pinned Node interoperability lane must pass unchanged after every task, and they are the proof that nothing moved.
- **No exported name in the `server` package may mention a type from `internal/`.** Importing internal packages for implementation is fine and unavoidable: `internal` blocks another module from importing that package directly, not from using a public package built on top of it. What breaks an external caller is an exported signature naming a type they cannot construct or implement. That rule is what the `Store` seam and `FileStore` exist to satisfy, and Task 1 Step 6 checks it directly.
- The main module vendors its dependencies (`-mod vendor`). The `examples/` module does not; it uses a `replace` for its parent and the module cache for the rest.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task lint` and `devbox run -- task test` before every commit.

## Dependencies

**M6.1, the server play-state migration.** Not a technical dependency: nothing here needs the generated play packets. It is a sequencing one. M6.1 rewrites `internal/server/player` and the play handlers, and M11.1 moves the package those files live in, so running them concurrently means resolving the same conflicts twice. Land M6.1 first.

The design this plan implements is
[the server framework design](../specs/2026-08-16-server-framework-design.md),
2026-08-16. This plan covers **M11.1 only**: Decision 1 in full, and the parts
of Decision 5 that do not need the world model. M11.2 through M11.7 get their
own designs and plans.

## Two narrowings this plan makes

Both are places where the honest small thing differs from the design's eventual
shape. They are stated here so a reviewer can reject them cheaply:

1. **The `Store` seam covers world persistence only.** `Server` also saves
   players, and `storage.SavePlayer` takes `*player.Player`, an internal type.
   Putting it in a public interface would mean no external implementer could
   satisfy it. Player persistence therefore keeps its current concrete path for
   now and M11.3 folds it into `WorldStore` once the player model has a public
   shape.
2. **`Store` keeps `SaveWorldAnvil`, a method that names a format.** That is
   exactly what Decision 4 replaces. Extracting the interface that exists is
   honest; inventing the M11.3 interface here would be designing that milestone
   without its research.

## File Structure

**Created:**

| File | Responsibility |
| --- | --- |
| `server/server.go` | `Server`, `New`, `Start`, moved from `internal/server` |
| `server/options.go` | `Option` and every `With*` constructor |
| `server/options_test.go` | Option application, precedence, and validation |
| `server/store.go` | The `Store` seam and `FileStore`, the default implementation |
| `server/observer.go` | The `Observer` seam, `Sample`, and the no-op default |
| `server/observer_test.go` | Sample delivery, the no-op path, and non-blocking delivery |
| `server/metrics.go` | Process CPU and memory sampling, and the network observation sink |
| `server/metrics_test.go` | Counter correctness and the sink's byte accounting |
| `config/config.go` | Moved from `internal/server/config` |
| `examples/go.mod` | The examples module, with a `replace` for its parent |
| `examples/vanilla/main.go` | Today's `cmd/server`, rewritten against options |
| `examples/minimal/main.go` | Accepts a login into an empty world, no storage |
| `examples/flat/main.go` | Superflat, in-memory, no storage |
| `examples/examples_test.go` | Each example builds and its server reaches listening |

**Modified:**

| File | Change |
| --- | --- |
| `internal/server/*.go` | Moved to `server/`; package clause and imports updated |
| `internal/server/config/*.go` | Moved to `config/` |
| `internal/server/storage/storage.go` | Import path for config; no other change |
| `internal/server/conn/*.go` | Import path for config; no other change |
| `interop/node_client_test.go` | Constructs the server through options |
| `Taskfile.yml` | `fmt` paths, `build` target, and a new `test:examples` lane |
| `README.md` | Framework usage, the examples, and how to run them |
| `MASTER_PLAN.md` (in `headless-minecraft`) | M11.1 marked complete |

**Deleted:**

| File | Reason |
| --- | --- |
| `cmd/server/main.go` | Becomes `examples/vanilla/main.go` |

---

## Stage A — Make the core importable

### Task 1: Move the server and config packages out of `internal`

A framework has to be importable from another module. Nothing under
`internal/` is, so this move is the precondition for every task after it.

This is one task rather than two because a public `server` package whose `New`
takes an `*internal/server/config.Config` is not usable from outside the module,
so moving only one of them delivers nothing a reviewer could accept.

**Files:**
- Move: `internal/server/*.go` → `server/*.go` (not the subdirectories)
- Move: `internal/server/config/*.go` → `config/*.go`
- Modify: `internal/server/conn/*.go`, `internal/server/storage/*.go`, `internal/server/player/*.go` — config import path only
- Modify: `cmd/server/main.go`, `interop/node_client_test.go` — import paths
- Modify: `Taskfile.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: `server.Server`, `server.New(cfg *config.Config, log *slog.Logger, store *storage.Storage) (*Server, error)`, `(*Server).Start(ctx context.Context) error`, `config.Config`, `config.DefaultConfig() *Config`, `config.Merge(cfg, fromFile *Config, explicitFlags map[string]bool)`, `config.GeneratorDefault`, `config.GeneratorFlat`. Signatures are unchanged from today; Task 2 replaces `New`.

- [ ] **Step 1: Confirm the baseline is green before moving anything**

```bash
devbox run -- task test
devbox run -- task test:interop
```

Expected: PASS. A move that starts from a red tree cannot be verified, and this
milestone's whole claim is that nothing changed.

- [ ] **Step 2: Move the packages**

```bash
git mv internal/server/config config
git mv internal/server/server.go server/server.go
```

`git mv` for every remaining `*.go` file directly under `internal/server`,
including its test files. Leave `internal/server/conn`, `internal/server/player`,
and `internal/server/storage` where they are: they are implementation detail and
nothing outside the module needs them.

The package clause in the moved files is already `package server` and already
`package config`, so neither changes.

- [ ] **Step 3: Update every import path**

Five packages import `internal/server/config`: `server`, `conn`, `player`,
`storage`, and `cmd/server`. Two import `internal/server`: `cmd/server` and
`interop`.

```bash
grep -rln "internal/server/config" --include='*.go' . | grep -v vendor
grep -rln "server/internal/server\"" --include='*.go' . | grep -v vendor
```

Rewrite `github.com/go-theft-craft/server/internal/server/config` to
`github.com/go-theft-craft/server/config`, and
`github.com/go-theft-craft/server/internal/server` to
`github.com/go-theft-craft/server/server`.

- [ ] **Step 4: Update the Taskfile paths**

`fmt` runs `gci` over a fixed directory list that no longer covers the moved
code:

```yaml
      - gci write -s standard -s default -s "prefix(github.com/openserbia)" -s "prefix({{.PACKAGE_NAME}})" --skip-generated internal cmd server config pkg
```

- [ ] **Step 5: Run the full gate**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:interop
```

Expected: PASS, identical to Step 1. If the interoperability lane fails, the
move changed behavior, which it must not.

- [ ] **Step 6: Prove the package is importable from outside the module**

```bash
cd /tmp && mkdir -p importcheck && cd importcheck
cat > go.mod <<'EOF'
module importcheck

go 1.26.6

require github.com/go-theft-craft/server v0.0.0

replace github.com/go-theft-craft/server => /home/ocharnyshevich/pet.projects/go-theft-craft/server
EOF
cat > main.go <<'EOF'
package main

import (
	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/server"
)

var (
	_ = config.DefaultConfig
	_ = server.New
)

func main() {}
EOF
go build ./...
```

Expected: builds. This is the property the move exists to create, and it is
worth checking directly rather than inferring from the in-module build. Delete
`/tmp/importcheck` afterwards.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: move the server and config packages out of internal"
```

### Task 2: Construct the server with functional options

`New(cfg, log, store)` is three positional arguments where two are optional and
one is a mutable struct the caller fills in field by field. Options make the
optional parts optional, let the seams in Tasks 3 and 4 be supplied the same
way, and give every later sub-milestone one place to add a knob.

**Files:**
- Create: `server/options.go`, `server/options_test.go`
- Modify: `server/server.go`
- Modify: `interop/node_client_test.go`, `cmd/server/main.go`

**Interfaces:**
- Consumes: `config.Config`, `config.DefaultConfig` from Task 1.
- Produces: `server.Option`, `server.New(opts ...Option) (*Server, error)`, and `WithSettings(*config.Config)`, `WithLogger(*slog.Logger)`, `WithGenerator(gen.Generator)`, `WithPort(int)`, `WithSeed(int64)`, `WithMOTD(string)`, `WithOnlineMode(bool)`, `WithMaxPlayers(int)`, `WithViewDistance(int)`, `WithWorldRadius(int)`, `WithCompressionThreshold(int)`, `WithPrivateKey(*rsa.PrivateKey)`. `ErrInvalidOption`.

- [ ] **Step 1: Write the failing test**

`server/options_test.go`:

```go
package server_test

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"log/slog"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

func TestNewAppliesDefaultsWhenGivenNoOptions(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := srv.Settings()
	want := config.DefaultConfig()

	if got.Port != want.Port {
		t.Errorf("port is %d, want %d", got.Port, want.Port)
	}
	if got.MOTD != want.MOTD {
		t.Errorf("MOTD is %q, want %q", got.MOTD, want.MOTD)
	}
}

func TestOptionsApplyInOrderAndTheLastOneWins(t *testing.T) {
	srv, err := server.New(server.WithPort(1), server.WithPort(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := srv.Settings().Port; got != 2 {
		t.Errorf("port is %d, want 2 from the later option", got)
	}
}

func TestWithSettingsIsOverriddenByLaterOptions(t *testing.T) {
	// A caller loading a config file then applying flags is the vanilla
	// example's exact shape, so the ordering has to work that way round.
	settings := config.DefaultConfig()
	settings.Port = 100

	srv, err := server.New(
		server.WithSettings(settings),
		server.WithPort(200),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := srv.Settings().Port; got != 200 {
		t.Errorf("port is %d, want 200", got)
	}
}

func TestWithSettingsCopiesRatherThanAliases(t *testing.T) {
	// The caller keeps its own struct. Mutating it after New must not
	// reach inside the server.
	settings := config.DefaultConfig()
	settings.Port = 100

	srv, err := server.New(server.WithSettings(settings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	settings.Port = 999

	if got := srv.Settings().Port; got != 100 {
		t.Errorf("port is %d, want 100; New aliased the caller's settings", got)
	}
}

func TestAnInvalidOptionIsRejectedBeforeAnyWork(t *testing.T) {
	_, err := server.New(server.WithPort(-1))
	if !errors.Is(err, server.ErrInvalidOption) {
		t.Fatalf("New returned %v, want ErrInvalidOption", err)
	}
}

func TestWithGeneratorReplacesTheOneSettingsWouldHaveChosen(t *testing.T) {
	custom := gen.NewFlatGenerator(7)

	srv, err := server.New(
		server.WithSeed(1),
		server.WithGenerator(custom),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.Generator() != custom {
		t.Error("New built its own generator instead of using the supplied one")
	}
}

func TestWithLoggerIsUsedAndNilLoggerIsRejected(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	srv, err := server.New(server.WithLogger(log))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Logger() != log {
		t.Error("New did not use the supplied logger")
	}

	if _, err := server.New(server.WithLogger(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Errorf("New accepted a nil logger, returned %v", err)
	}
}

func TestWithPrivateKeyIsCarriedIntoSettings(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv, err := server.New(server.WithPrivateKey(key))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.Settings().PrivateKey != key {
		t.Error("New did not carry the private key into settings")
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test
```

Expected: FAIL, `server.Option` and the `With*` constructors are undefined.

- [ ] **Step 3: Implement the options**

`server/options.go`:

```go
package server

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// ErrInvalidOption reports an option rejected before any network, disk, or
// key-generation work happened.
var ErrInvalidOption = errors.New("invalid server option")

// Option configures a server. Options apply in the order given, so a caller
// that loads a configuration file and then applies command-line flags passes
// WithSettings first and the flags after it.
type Option func(*builder) error

// builder accumulates option results. It exists so New can validate the whole
// set before constructing anything, rather than half-building a server and
// discovering the port is negative.
type builder struct {
	settings  *config.Config
	log       *slog.Logger
	generator gen.Generator
}

// WithSettings replaces the whole settings struct. The value is copied, so the
// caller may keep mutating its own.
func WithSettings(settings *config.Config) Option {
	return func(b *builder) error {
		if settings == nil {
			return fmt.Errorf("%w: nil settings", ErrInvalidOption)
		}
		copied := *settings
		b.settings = &copied

		return nil
	}
}

// WithLogger sets the logger. A server with no logger would silently drop the
// operational record, so nil is an error rather than a default.
func WithLogger(log *slog.Logger) Option {
	return func(b *builder) error {
		if log == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidOption)
		}
		b.log = log

		return nil
	}
}

// WithGenerator supplies a world generator, replacing the one the generator
// type in settings would have selected.
func WithGenerator(g gen.Generator) Option {
	return func(b *builder) error {
		if g == nil {
			return fmt.Errorf("%w: nil generator", ErrInvalidOption)
		}
		b.generator = g

		return nil
	}
}

// WithPort sets the listening port.
func WithPort(port int) Option {
	return func(b *builder) error {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%w: port %d outside 1-65535", ErrInvalidOption, port)
		}
		b.settings.Port = port

		return nil
	}
}

// WithSeed sets the world generation seed.
func WithSeed(seed int64) Option {
	return func(b *builder) error {
		b.settings.Seed = seed

		return nil
	}
}

// WithMOTD sets the description shown in the server list.
func WithMOTD(motd string) Option {
	return func(b *builder) error {
		b.settings.MOTD = motd

		return nil
	}
}

// WithOnlineMode enables or disables Mojang authentication.
func WithOnlineMode(online bool) Option {
	return func(b *builder) error {
		b.settings.OnlineMode = online

		return nil
	}
}

// WithMaxPlayers sets the player count shown in the server list.
func WithMaxPlayers(maximum int) Option {
	return func(b *builder) error {
		if maximum < 0 {
			return fmt.Errorf("%w: max players %d is negative", ErrInvalidOption, maximum)
		}
		b.settings.MaxPlayers = maximum

		return nil
	}
}

// WithViewDistance sets the entity view distance in chunks.
func WithViewDistance(chunks int) Option {
	return func(b *builder) error {
		if chunks < 1 {
			return fmt.Errorf("%w: view distance %d is below 1", ErrInvalidOption, chunks)
		}
		b.settings.ViewDistance = chunks

		return nil
	}
}

// WithWorldRadius sets the world boundary in chunks. Zero means unbounded.
func WithWorldRadius(chunks int) Option {
	return func(b *builder) error {
		if chunks < 0 {
			return fmt.Errorf("%w: world radius %d is negative", ErrInvalidOption, chunks)
		}
		b.settings.WorldRadius = chunks

		return nil
	}
}

// WithCompressionThreshold sets the packet size at or above which the server
// compresses. A negative value disables compression.
func WithCompressionThreshold(threshold int) Option {
	return func(b *builder) error {
		b.settings.CompressionThreshold = threshold

		return nil
	}
}

// WithPrivateKey supplies the RSA keypair the login acceptor uses. Generating
// one costs real time, so the application owns when that happens.
func WithPrivateKey(key *rsa.PrivateKey) Option {
	return func(b *builder) error {
		if key == nil {
			return fmt.Errorf("%w: nil private key", ErrInvalidOption)
		}
		b.settings.PrivateKey = key

		return nil
	}
}
```

- [ ] **Step 4: Rewrite `New` and add the accessors**

In `server/server.go`, replace the existing `New` with:

```go
// New builds a server from options. It validates everything before doing any
// work, so an invalid port is reported without a socket ever being opened.
func New(opts ...Option) (*Server, error) {
	b := &builder{
		settings: config.DefaultConfig(),
		log:      slog.New(slog.DiscardHandler),
	}

	for i, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: option %d is nil", ErrInvalidOption, i)
		}
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	generator := b.generator
	if generator == nil {
		switch b.settings.GeneratorType {
		case config.GeneratorFlat:
			generator = gen.NewFlatGenerator(b.settings.Seed)
		default:
			generator = gen.NewDefaultGenerator(b.settings.Seed)
		}
	}

	gameData, err := v1_8.Data()
	if err != nil {
		return nil, fmt.Errorf("load java 1.8 game data: %w", err)
	}

	return &Server{
		cfg:       b.settings,
		log:       b.log,
		world:     world.NewWorld(generator),
		players:   player.NewManager(b.settings.ViewDistance),
		gameData:  gameData,
		generator: generator,
	}, nil
}

// Settings returns a copy of the effective settings. It is a copy because the
// server keeps using its own, and a caller that mutated the returned value
// would change behavior from outside with nothing to say it had.
func (s *Server) Settings() config.Config { return *s.cfg }

// Logger returns the logger the server was built with.
func (s *Server) Logger() *slog.Logger { return s.log }

// Generator returns the world generator the server was built with.
func (s *Server) Generator() gen.Generator { return s.generator }
```

Add `generator gen.Generator` to the `Server` struct. The `storage` field stays
for now and Task 3 replaces it; `New` no longer sets it, so `Start` sees a nil
store and skips persistence, which is the behavior `interop` already relies on.

- [ ] **Step 5: Update the two callers**

`cmd/server/main.go` replaces `server.New(cfg, log, store)` with option calls.
Because the file already merges flags over file config into one `*config.Config`,
the smallest correct change is:

```go
	srv, err := server.New(
		server.WithSettings(cfg),
		server.WithLogger(log),
		server.WithStore(store),
	)
```

`WithStore` does not exist until Task 3. Until then, drop it here and let the
example run without persistence; Task 3 restores it in the same file. Note this
in the commit message so the gap is deliberate rather than a regression nobody
noticed.

`interop/node_client_test.go` line 87 becomes:

```go
	instance, err := server.New(
		server.WithSettings(settings),
		server.WithLogger(slog.New(slog.DiscardHandler)),
	)
```

- [ ] **Step 6: Run and verify it passes**

```bash
devbox run -- task test
devbox run -- task test:interop
```

Expected: PASS, including the eight new option tests.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(server): construct the server with functional options

Persistence is temporarily disconnected: WithStore arrives with the
Store seam in the next commit, and cmd/server runs without saving
until it does."
```

---

## Stage B — Declare the seams

### Task 3: The `Store` seam

`Server` holds a `*storage.Storage`, a concrete type in an internal package.
That is the single thing stopping an outside module from supplying its own
persistence, and Decision 4 in the design turns it into `WorldStore` in M11.3.
M11.1 extracts the interface that exists today.

**Files:**
- Create: `server/store.go`, `server/store_test.go`
- Modify: `server/server.go`, `server/options.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `server.Option` from Task 2, `world.World` from `pkg/world`.
- Produces: `server.Store` interface, `server.WithStore(Store) Option`, `server.FileStore(dir string, log *slog.Logger) (Store, error)`.

- [ ] **Step 1: Write the failing test**

`server/store_test.go`:

```go
package server_test

import (
	"errors"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// recordingStore records which methods a server called, so a test can assert
// on the persistence contract without touching a disk.
type recordingStore struct {
	loadedWorld     bool
	loadedOverrides bool
	savedWorld      bool
	hasSaved        bool
	failLoad        error
}

func (s *recordingStore) HasSavedWorld() bool { return s.hasSaved }

func (s *recordingStore) LoadWorld(*world.World) error {
	s.loadedWorld = true

	return s.failLoad
}

func (s *recordingStore) SaveWorld(*world.World) error {
	s.savedWorld = true

	return nil
}

func (s *recordingStore) LoadBlockOverrides(*world.World) error {
	s.loadedOverrides = true

	return nil
}

func (s *recordingStore) SaveBlockOverrides(*world.World) error { return nil }

func (s *recordingStore) SaveWorldAnvil(*world.World) error { return nil }

func TestAServerWithNoStoreIsValid(t *testing.T) {
	// The interoperability lane and the minimal example both run without
	// persistence, so no store is a supported configuration rather than an
	// oversight.
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Store() != nil {
		t.Error("New invented a store")
	}
}

func TestWithStoreRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithStore(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Error("WithStore accepted nil; use no option at all to run without persistence")
	}
}

func TestAnExternalTypeSatisfiesTheStoreSeam(t *testing.T) {
	// This is the property the milestone exists to create: a type declared
	// outside the server package, implementing only exported types, is a
	// valid store.
	var store server.Store = &recordingStore{}

	srv, err := server.New(server.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Store() != store {
		t.Error("New did not use the supplied store")
	}
}

func TestFileStoreSatisfiesTheSeam(t *testing.T) {
	store, err := server.FileStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	if store == nil {
		t.Fatal("FileStore returned a nil store and no error")
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test
```

Expected: FAIL, `server.Store`, `server.WithStore`, `server.Store()`, and
`server.FileStore` are undefined.

- [ ] **Step 3: Implement**

`server/store.go`:

```go
package server

import (
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// Store is world persistence. The server depends on this rather than on a
// concrete type, so an application can supply its own.
//
// It covers the world only. Player persistence still runs through the
// concrete path inside this package, because storage.SavePlayer takes an
// internal type and no external implementer could satisfy a method naming it.
// M11.3 folds player data in when the player model has a public shape.
//
// SaveWorldAnvil names a format, which a seam should not. It is here because
// it is what the server calls today, and inventing the version-neutral
// WorldStore now would be designing M11.3 without its research.
type Store interface {
	HasSavedWorld() bool
	LoadWorld(w *world.World) error
	SaveWorld(w *world.World) error
	LoadBlockOverrides(w *world.World) error
	SaveBlockOverrides(w *world.World) error
	SaveWorldAnvil(w *world.World) error
}

// WithStore supplies persistence. Omit the option entirely to run without it;
// a nil store is an error rather than a silent no-op, because "I passed a
// store and nothing was saved" is the harder failure to diagnose.
func WithStore(store Store) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil store, omit WithStore to run without persistence", ErrInvalidOption)
		}
		b.store = store

		return nil
	}
}

// FileStore returns the framework's default store, which keeps config,
// world, and player data as files under dir.
//
// It exists so an application in another module can use the default without
// importing an internal package. A nil logger is replaced with a discarding
// one.
func FileStore(dir string, log *slog.Logger) (Store, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	store, err := storage.New(dir, log)
	if err != nil {
		return nil, fmt.Errorf("create file store: %w", err)
	}

	return store, nil
}
```

Add `store Store` to `builder`, add `store Store` to `Server` replacing the
`*storage.Storage` field, set it in `New` from `b.store`, and add:

```go
// Store returns the store the server was built with, or nil if it runs
// without persistence.
func (s *Server) Store() Store { return s.store }
```

`Start` already guards every persistence call with `if s.storage != nil`;
rename the field references and the guards keep working unchanged.

The one call `Store` does not cover is `s.storage.SavePlayer(p)`. Keep it on a
separate unexported field, discovered from whatever store was supplied:

```go
// playerSaver is the half of persistence that Store cannot express yet,
// because storage.SavePlayer takes an internal type. It is unexported, so it
// names an internal type legally: no external caller has to satisfy it, and a
// store that does not implement it simply does not persist players. M11.3
// removes it by giving player data a public shape.
type playerSaver interface {
	SavePlayer(p *player.Player) error
}
```

Add `playerStore playerSaver` to `Server`, and in `New`, after setting
`s.store`:

```go
	if ps, ok := b.store.(playerSaver); ok {
		srv.playerStore = ps
	}
```

Guard the call site with `if s.playerStore != nil`, the same shape as the
existing store guards. `FileStore` returns the concrete storage type behind the
`Store` interface, so the assertion succeeds for the default and fails
harmlessly for an external store.

- [ ] **Step 4: Restore persistence in `cmd/server`**

Add `server.WithStore(store)` back to the option list, and build `store` with
`server.FileStore(dataDir, log)`. The config load and save still use the
concrete storage type, because `LoadConfig` and `SaveConfig` are the
application's business rather than the server's. Import
`internal/server/storage` directly for those two calls; `cmd/server` is in the
same module, so it may.

- [ ] **Step 5: Run and verify it passes**

```bash
devbox run -- task test
devbox run -- task test:interop
```

Expected: PASS, including the four new store tests.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(server): declare the Store seam and restore persistence"
```

### Task 4: The `Observer` seam and plain resource counters

Decision 5 in the design puts per-player, per-feature, and per-chunk
attribution in M11.6, because those numbers depend on a world model that M11.2
replaces. CPU, memory, and network do not depend on it and land here.

Network bytes come from `minecraft-protocol`'s existing observation sink rather
than from new counting code. `newStream` already takes
`...protocol.StreamOption`, and `protocol.WithObservationSink` is one.

**Files:**
- Create: `server/observer.go`, `server/observer_test.go`, `server/metrics.go`, `server/metrics_test.go`
- Modify: `server/options.go`, `server/server.go`, `internal/server/conn/stream.go`

**Interfaces:**
- Consumes: `server.Option` from Task 2.
- Produces: `server.Observer` interface, `server.Sample`, `server.SampleKind` with `SampleCPU`, `SampleMemory`, `SampleNetworkIn`, `SampleNetworkOut`, `server.WithObserver(Observer) Option`, `server.NopObserver`.

- [ ] **Step 1: Write the failing test**

`server/observer_test.go`:

```go
package server_test

import (
	"sync"
	"testing"
	"time"

	"github.com/go-theft-craft/server/server"
)

// collectingObserver stores every sample it is given.
type collectingObserver struct {
	mu      sync.Mutex
	samples []server.Sample
}

func (o *collectingObserver) Observe(s server.Sample) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.samples = append(o.samples, s)
}

func (o *collectingObserver) count(kind server.SampleKind) int {
	o.mu.Lock()
	defer o.mu.Unlock()

	n := 0
	for _, s := range o.samples {
		if s.Kind == kind {
			n++
		}
	}

	return n
}

// blockingObserver never returns, which is how a badly written observer
// stalls a server that calls it on a hot path.
type blockingObserver struct{ release chan struct{} }

func (o *blockingObserver) Observe(server.Sample) { <-o.release }

func TestAServerWithNoObserverUsesTheNopAndDoesNotPanic(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A nil observer here would panic on the first sample, which would
	// only show up under load.
	srv.Observe(server.Sample{Kind: server.SampleCPU, Value: 1})
}

func TestWithObserverReceivesSamples(t *testing.T) {
	obs := &collectingObserver{}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv.Observe(server.Sample{Kind: server.SampleNetworkIn, Value: 42})

	if got := obs.count(server.SampleNetworkIn); got != 1 {
		t.Errorf("observer saw %d network-in samples, want 1", got)
	}
}

func TestWithObserverRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithObserver(nil)); err == nil {
		t.Error("WithObserver accepted nil; omit the option to run without one")
	}
}

func TestASlowObserverDoesNotStallTheCaller(t *testing.T) {
	// Delivery to an observer must never block the caller, because the
	// network sink runs on the stream's own goroutine and blocking there
	// applies backpressure to the whole connection.
	obs := &blockingObserver{release: make(chan struct{})}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			srv.Observe(server.Sample{Kind: server.SampleCPU, Value: float64(i)})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Observe blocked on a slow observer")
	}
	close(obs.release)
}
```

`server/metrics_test.go`:

```go
package server_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/server/server"
)

func TestTheNetworkSinkCountsRawFrameBytesInBothDirections(t *testing.T) {
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	inbound := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionServerbound,
		OriginalLen: 100,
	}
	outbound := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionClientbound,
		OriginalLen: 250,
	}

	if err := sink.Observe(t.Context(), inbound); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if err := sink.Observe(t.Context(), outbound); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if got := obs.count(server.SampleNetworkIn); got != 1 {
		t.Errorf("%d network-in samples, want 1", got)
	}
	if got := obs.count(server.SampleNetworkOut); got != 1 {
		t.Errorf("%d network-out samples, want 1", got)
	}
}

func TestTheNetworkSinkIgnoresEveryStageButRawFrame(t *testing.T) {
	// A packet record describes the same bytes a raw record already
	// counted, so counting both would double every connection's traffic.
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	for _, stage := range []protocol.ObservationStage{
		protocol.ObservationPacket,
		protocol.ObservationRejected,
		protocol.ObservationSecret,
	} {
		record := protocol.Observation{
			Stage:       stage,
			Direction:   protocol.DirectionServerbound,
			OriginalLen: 100,
		}
		if err := sink.Observe(t.Context(), record); err != nil {
			t.Fatalf("Observe %s: %v", stage, err)
		}
	}

	if got := len(obs.samples); got != 0 {
		t.Errorf("sink emitted %d samples for non-frame stages, want 0", got)
	}
}

func TestTheNetworkSinkUsesOriginalLenSoARedactedRecordStillCounts(t *testing.T) {
	obs := &collectingObserver{}
	sink := server.NetworkSink(obs)

	record := protocol.Observation{
		Stage:       protocol.ObservationRawFrame,
		Direction:   protocol.DirectionServerbound,
		OriginalLen: 512,
		Redacted:    true,
	}
	if err := sink.Observe(t.Context(), record); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()

	if len(obs.samples) != 1 {
		t.Fatalf("%d samples, want 1", len(obs.samples))
	}
	if got := obs.samples[0].Value; got != 512 {
		t.Errorf("counted %v bytes, want 512 from OriginalLen", got)
	}
}

func TestResourceSamplesReportPlausibleValues(t *testing.T) {
	obs := &collectingObserver{}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.SampleResources()

	if got := obs.count(server.SampleMemory); got != 1 {
		t.Errorf("%d memory samples, want 1", got)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()

	for _, s := range obs.samples {
		if s.Kind == server.SampleMemory && s.Value <= 0 {
			t.Errorf("memory sample is %v, want a positive byte count", s.Value)
		}
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test
```

Expected: FAIL, the observer and metrics identifiers are undefined.

- [ ] **Step 3: Implement the observer**

`server/observer.go`:

```go
package server

import (
	"fmt"
	"time"
)

// SampleKind names what a sample measures.
type SampleKind string

const (
	// SampleCPU is process CPU time consumed, in seconds.
	SampleCPU SampleKind = "cpu"
	// SampleMemory is heap bytes currently allocated.
	SampleMemory SampleKind = "memory"
	// SampleNetworkIn is bytes read from a connection, counted at the frame.
	SampleNetworkIn SampleKind = "network_in"
	// SampleNetworkOut is bytes written to a connection, counted at the frame.
	SampleNetworkOut SampleKind = "network_out"
)

// Sample is one measurement.
//
// Labels carries dimensions a later milestone attributes by: a player, a
// feature, a chunk. M11.1 emits none, and the field exists now so M11.6 adds
// attribution without changing this type and every implementation of Observer
// with it.
type Sample struct {
	Kind   SampleKind
	Value  float64
	At     time.Duration
	Labels map[string]string
}

// Observer receives samples.
//
// An implementation must not block: samples are emitted from the tick loop and
// from stream goroutines, and a slow observer that applied backpressure would
// slow the server it was only supposed to be watching. Buffer internally and
// drop rather than wait.
type Observer interface {
	Observe(s Sample)
}

// NopObserver discards every sample. It is the default, so nothing has to
// nil-check an observer on a hot path.
type NopObserver struct{}

// Observe discards the sample.
func (NopObserver) Observe(Sample) {}

// WithObserver supplies an observer. Omit the option to run without one; a nil
// observer is an error because "I passed an observer and got no samples" is
// harder to diagnose than a rejected option.
func WithObserver(obs Observer) Option {
	return func(b *builder) error {
		if obs == nil {
			return fmt.Errorf("%w: nil observer, omit WithObserver to run without one", ErrInvalidOption)
		}
		b.observer = obs

		return nil
	}
}

// Observe forwards a sample to the configured observer.
func (s *Server) Observe(sample Sample) { s.observer.Observe(sample) }
```

Add `observer Observer` to `builder` and to `Server`, and default it in `New`:

```go
	b := &builder{
		settings: config.DefaultConfig(),
		log:      slog.New(slog.DiscardHandler),
		observer: NopObserver{},
	}
```

- [ ] **Step 4: Implement the metrics**

`server/metrics.go`:

```go
package server

import (
	"context"
	"runtime"
	"syscall"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// networkSink turns stream observations into network samples.
//
// It counts ObservationRawFrame only. A packet record describes bytes a raw
// record already counted, so counting both would double every connection's
// traffic, and the rejected and secret stages describe no wire bytes at all.
type networkSink struct{ observer Observer }

// NetworkSink adapts an Observer to minecraft-protocol's observation sink, so
// network accounting reuses the observation points M1 already publishes rather
// than adding a second counting path that could disagree with them.
func NetworkSink(obs Observer) protocol.ObservationSink {
	if obs == nil {
		obs = NopObserver{}
	}

	return &networkSink{observer: obs}
}

// Observe records one frame's bytes.
//
// It always returns nil. Observation delivery is lossless and a sink that
// errors would fail the stream, and a metrics sink must never be able to drop
// a connection.
func (s *networkSink) Observe(_ context.Context, record protocol.Observation) error {
	if record.Stage != protocol.ObservationRawFrame {
		return nil
	}

	kind := SampleNetworkOut
	if record.Direction == protocol.DirectionServerbound {
		kind = SampleNetworkIn
	}

	// OriginalLen rather than len(Bytes): a redacted record drops its
	// payload but still reports the size it withheld.
	s.observer.Observe(Sample{
		Kind:  kind,
		Value: float64(record.OriginalLen),
		At:    record.Elapsed,
	})

	return nil
}

// SampleResources emits one CPU and one memory sample for the process.
//
// Both are process-wide rather than per-player. Attributing them to a player
// or a feature is M11.6, and it needs the world model M11.2 replaces.
func (s *Server) SampleResources() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	s.Observe(Sample{Kind: SampleMemory, Value: float64(stats.HeapAlloc)})

	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		// CPU accounting is best-effort. A platform without getrusage
		// still reports memory, and a metrics gap is not a server fault.
		return
	}

	seconds := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
		float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6

	s.Observe(Sample{Kind: SampleCPU, Value: seconds})
}

// resourceSampleInterval is how often the tick loop samples process
// resources. Ten seconds is often enough to see a trend and rare enough that
// ReadMemStats, which stops the world briefly, is not itself the load.
const resourceSampleInterval = 10 * time.Second
```

Call `SampleResources` from the existing tick loop on a
`time.NewTicker(resourceSampleInterval)`, guarded so it does nothing when the
observer is `NopObserver{}`.

- [ ] **Step 5: Wire the sink into connections**

`internal/server/conn/stream.go`'s `newStream` already accepts
`...protocol.StreamOption`. Find its caller in `connection.go` and pass
`protocol.WithObservationSink(sink)` when the server has a real observer. The
server hands the sink down the same way it hands down its logger today.

Do not install a sink when the observer is the no-op: observation delivery has
a cost per frame, and a server that was never asked for metrics should not pay
it.

- [ ] **Step 6: Run and verify it passes**

```bash
devbox run -- task test
devbox run -- task test:interop
```

Expected: PASS, including the four observer tests and the four metrics tests.
The interoperability lane matters here: a sink that errored or blocked would
show up as a failed or hung connection rather than as a failed unit test.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(server): add the Observer seam and process and network counters"
```

---

## Stage C — The examples module

### Task 5: Three examples in their own module

`cmd/server` is the only demonstration this repository has, and it composes
every piece at once, which shows nothing about whether the pieces come apart.
Three examples that compose different subsets do.

`examples/` is its own Go module so the library keeps the dependency list it
has. That is the repository convention recorded in `MASTER_PLAN.md`.

**Files:**
- Create: `examples/go.mod`, `examples/vanilla/main.go`, `examples/minimal/main.go`, `examples/flat/main.go`
- Delete: `cmd/server/main.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Consumes: everything the `server` package produced in Tasks 1 through 4.
- Produces: three runnable programs. No Go API.

- [ ] **Step 1: Create the module**

`examples/go.mod`:

```
module github.com/go-theft-craft/server/examples

go 1.26.6

require github.com/go-theft-craft/server v0.0.0

replace github.com/go-theft-craft/server => ../
```

The `replace` is deliberate. An example module has to build against the working
tree rather than a released version, or it could not demonstrate an unreleased
change. It is the one place in this repository where a `replace` is correct.

This module does not vendor. The parent vendors because it ships; examples do
not ship, and vendoring a `replace`d parent duplicates the whole tree on disk
for no benefit.

- [ ] **Step 2: Write `examples/minimal`**

The smallest thing that is still a server: a login into an empty world, no
storage, no generator choice, no flags.

```go
// Command minimal runs the smallest server this framework can build: it
// accepts logins into a generated world and persists nothing.
//
// Everything it does not do is the point. There is no storage, no
// configuration file, and no observer, and each of those is one option away.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/server"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv, err := server.New(
		server.WithLogger(log),
		server.WithMOTD("minimal example"),
		server.WithWorldRadius(4),
	)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Write `examples/flat`**

Superflat and in-memory, and it supplies its own generator rather than naming
one in settings, which is the seam `WithGenerator` exists for.

```go
// Command flat runs a superflat, in-memory server with a generator supplied
// directly rather than selected by name in settings.
//
// This is the example that shows a seam being replaced: nothing here asks the
// framework to pick a generator, and a custom generator would be substituted
// exactly the same way.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

const seed = 1

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv, err := server.New(
		server.WithLogger(log),
		server.WithGenerator(gen.NewFlatGenerator(seed)),
		server.WithMOTD("flat example"),
		server.WithWorldRadius(8),
	)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Write `examples/vanilla`**

This is today's `cmd/server/main.go`, moved and rewritten against options.
Every flag, the config file merge, the RSA key generation, and the storage
wiring stay exactly as they are; only the construction changes.

Two things it cannot do from another module, and how it handles them:

- `config.LoadConfig` and `SaveConfig` live on the concrete storage type, which
  is internal. The example uses `server.FileStore` for the server's store and
  keeps its configuration file handling in its own code, reading and writing
  `config.Config` as JSON directly. That is nine lines and it avoids exporting
  a storage type this milestone has no other reason to export.
- The RSA key is generated in the example rather than by the framework, which is
  where it already happens today.

Copy the flag block verbatim from `cmd/server/main.go`, then:

```go
	store, err := server.FileStore(dataDir, log)
	if err != nil {
		log.Error("create store", "error", err)
		os.Exit(1)
	}

	srv, err := server.New(
		server.WithSettings(cfg),
		server.WithLogger(log),
		server.WithStore(store),
		server.WithPrivateKey(key),
	)
```

- [ ] **Step 5: Delete `cmd/server` and update the Taskfile**

```bash
git rm -r cmd/server
```

`build` currently builds `./cmd/server`, which no longer exists. Point it at the
examples module:

```yaml
  build:
    desc: Build the example servers
    deps: [ deps, cleanup ]
    cmds:
      - cmd: cd examples && go build -trimpath -o ../{{.BUILD_PATH}}/vanilla ./vanilla
        platforms: [linux/amd64,linux/arm64]
      - cmd: cd examples && go build -trimpath -o ../{{.BUILD_PATH}}/minimal ./minimal
        platforms: [linux/amd64,linux/arm64]
      - cmd: cd examples && go build -trimpath -o ../{{.BUILD_PATH}}/flat ./flat
        platforms: [linux/amd64,linux/arm64]
```

The `server` task, which runs the server, points at `examples/vanilla`.

Drop `cmd` from the `fmt` target's directory list and add `examples`. Note that
`gci` and `gofumpt` run from the repository root and reach into the nested
module fine, because both work on files rather than on packages.

- [ ] **Step 6: Verify each example builds and runs**

```bash
devbox run -- task build
./build/minimal &
sleep 2
kill %1
```

Expected: all three build; `minimal` logs `server started` and exits cleanly on
signal.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(examples): add the examples module and retire cmd/server"
```

### Task 6: Make the examples the test surface

An example that only demonstrates rots. This task makes CI run them, which is
what the repository convention asks for and what keeps Task 5 honest six months
from now.

**Files:**
- Create: `examples/examples_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Consumes: the three example programs from Task 5.
- Produces: `task test:examples`.

- [ ] **Step 1: Write the failing test**

`examples/examples_test.go`:

```go
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

			// Each example binds the default port, so they cannot run at
			// once on a fixed one. vanilla takes a flag; the others are
			// started one at a time by the port argument below.
			run := exec.CommandContext(ctx, binary, portFlag(name)...)
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

// portFlag gives each example a distinct port so the subtests can run in
// parallel. Only vanilla accepts flags today; the others need one added.
func portFlag(name string) []string {
	switch name {
	case "vanilla":
		return []string{"-port", "25701", "-data-dir", "/tmp/gtc-example-vanilla"}
	case "flat":
		return []string{"-port", "25702"}
	default:
		return []string{"-port", "25703"}
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
cd examples && go test ./...
```

Expected: FAIL. `minimal` and `flat` accept no flags, so they bind the default
port and two of the three subtests collide.

- [ ] **Step 3: Give `minimal` and `flat` a port flag**

Add a single `flag.Int` for the port to each, defaulting to `25565`, and pass it
through `server.WithPort`. Nothing else about either example changes; an example
that cannot be told where to listen cannot be tested alongside its siblings.

- [ ] **Step 4: Run and verify it passes**

```bash
cd examples && go test ./...
```

Expected: PASS, three subtests.

- [ ] **Step 5: Add the CI lane**

The nested module is invisible to `go test ./...` from the root, so it needs its
own target:

```yaml
  test:examples:
    desc: Build and start each example in the nested examples module
    deps: [ deps ]
    cmds:
      - cd examples && go mod tidy
      - cd examples && go test -count=1 ./...
```

Add `test:examples` to the `default` task alongside `test`, so it runs without
anyone remembering to ask for it. That is the whole point.

- [ ] **Step 6: Run the full gate**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
devbox run -- task test:interop
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "test(examples): build and start each example in CI"
```

---

## Stage D — Gate

### Task 7: Documentation and the milestone record

**Files:**
- Modify: `README.md`
- Modify: `../headless-minecraft/MASTER_PLAN.md`

- [ ] **Step 1: Rewrite the README's usage section**

It documents `cmd/server`, which no longer exists. Replace it with:

- what the framework is, in two sentences: composable pieces, and a harness the
  protocol work depends on;
- the minimal program, copied from `examples/minimal`, which is short enough to
  inline;
- a table of the three examples and what each one demonstrates;
- the seams that exist today, `Store`, `Observer`, and `gen.Generator`, and a
  note that `WorldStore` and the command set arrive in M11.3 and M11.7;
- how to run an example: `devbox run -- task build` then `./build/vanilla`.

Do not document `Sample.Labels` as usable. It is present for M11.6 and nothing
populates it yet, and a README that promises attribution this milestone does not
deliver is worse than one that stays quiet about it.

- [ ] **Step 2: Record the milestone**

In `../headless-minecraft/MASTER_PLAN.md`, tick M11.1 in the M11 section and
record, specifically:

- whether the observation sink measurably cost anything on the interoperability
  lane, because if it does then Task 4's "only install a sink when an observer
  exists" guard is load-bearing rather than a precaution;
- whether the two narrowings held, or whether `Store` needed player persistence
  sooner than M11.3;
- anything the external-import check in Task 1 Step 6 turned up, since that is
  the first time this repository has been consumed as a library.

- [ ] **Step 3: Run the full gate one last time**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
devbox run -- task test:interop
```

Expected: PASS, and the interoperability lane byte-identical to the baseline
captured in Task 1 Step 1.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: record M11.1 and document the framework surface"
```

---

## Self-review notes

- **Every task is a restructure and none changes behavior**, which is why the
  interoperability lane runs at the end of all seven rather than only at the
  gate. It is the only check that a move preserved the wire.
- **Task 2 knowingly leaves persistence disconnected for one commit.** The
  alternative was folding the `Store` seam into the options task, which would
  have made a single commit that moved packages, changed a constructor, and
  extracted an interface. The gap is one commit long, it is stated in the commit
  message, and Task 3 closes it.
- **Task 4's CPU sampling uses `syscall.Getrusage`, which is Unix-only.** The
  repository builds for `linux/amd64` and `linux/arm64` in its own Taskfile, so
  this costs nothing today. If a Windows build is ever wanted, the CPU half
  needs a build-tagged sibling and the memory half already works everywhere.
- **The two narrowings in the header are the parts most likely to be wrong.**
  `Store` keeping `SaveWorldAnvil` puts a format name in a public interface that
  M11.3 will have to remove, which is a breaking change to a seam this milestone
  just published. Publishing it unversioned and breaking it in M11.3 is
  acceptable only because nothing outside this repository consumes it yet, and
  that stops being true the moment someone does.
- **Task 6 changes two examples to accept a port flag purely so they can be
  tested.** That is test pressure shaping the demonstration, which is usually a
  smell. It is defensible here because a server you cannot tell where to listen
  is a worse example anyway.
- **Task 5 Step 4 says "copy the flag block verbatim" rather than reproducing
  it.** That is the one place this plan points at existing code instead of
  showing it. Twenty lines of `flag.IntVar` calls are moving unchanged, the
  source file is named exactly, and retyping them here would create a second
  copy that could drift from the real one between now and execution.
