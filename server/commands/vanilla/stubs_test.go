package vanilla_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/server"
	"github.com/go-theft-craft/server/server/commands/vanilla"
)

// The stub list.
//
// What it is for is visibility: a player typing /gamerule should be told the
// command exists and this server has not written it, not that they misspelled
// something. These tests are mostly about that distinction holding.

func TestEveryPinnedNameResolves(t *testing.T) {
	set := vanilla.Stubs()

	names := vanilla.Names()
	if len(names) < 50 {
		t.Fatalf("the pinned list holds %d names; 1.8 has about sixty", len(names))
	}

	for _, name := range names {
		if _, ok := set.Lookup(name); !ok {
			t.Errorf("/%s is in the pinned list and not in the set", name)
		}
	}
	// A spot check against the file rather than against this test's memory:
	// these are commands nobody would think to add and everybody would notice
	// missing.
	for _, name := range []string{"gamerule", "give", "ban", "whitelist", "worldborder", "tellraw"} {
		if !slices.Contains(names, name) {
			t.Errorf("/%s is not in the pinned list", name)
		}
	}
}

func TestAliasesResolveToTheSameCommand(t *testing.T) {
	set := vanilla.Stubs()

	tell, ok := set.Lookup("tell")
	if !ok {
		t.Fatal("/tell does not resolve")
	}
	for _, alias := range []string{"msg", "w"} {
		got, ok := set.Lookup(alias)
		if !ok {
			t.Errorf("/%s does not resolve", alias)

			continue
		}
		if got != tell {
			t.Errorf("/%s resolves to /%s, want /tell", alias, got.Name)
		}
	}

	// One command, however many names: All() is deduplicated by identity, so
	// /help does not print /tell three times.
	count := 0
	for _, cmd := range set.All() {
		if cmd == tell {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/tell appears %d times in All(), want once", count)
	}
}

func TestEveryStubReportsItselfUnimplemented(t *testing.T) {
	for _, cmd := range vanilla.Stubs().All() {
		if cmd.Implemented() {
			t.Errorf("/%s is a stub and reports itself implemented", cmd.Name)
		}
		// The description carries the vanilla usage line, which is the most
		// useful thing that can honestly be said about a command nobody has
		// written.
		if !strings.Contains(cmd.Description, "vanilla usage:") {
			t.Errorf("/%s does not say what vanilla takes: %q", cmd.Name, cmd.Description)
		}
	}
}

func TestNoStubShadowsAnImplementedCommand(t *testing.T) {
	// The order that matters: stubs first, implementations second, so a name
	// this server does implement resolves to the implementation.
	merged := server.Merge(vanilla.Stubs(), server.BuiltinCommands())

	for _, cmd := range server.BuiltinCommands().All() {
		got, ok := merged.Lookup(cmd.Name)
		if !ok {
			t.Errorf("/%s was lost in the merge", cmd.Name)

			continue
		}
		if !got.Implemented() {
			t.Errorf("/%s resolves to a stub after merging; the stub shadowed the implementation", cmd.Name)
		}
	}
}

func TestMissingIsWhatTheServerStillOwes(t *testing.T) {
	merged := server.Merge(vanilla.Stubs(), server.BuiltinCommands())
	missing := vanilla.Missing(merged)

	// The built-ins that share a vanilla name are not missing.
	for _, name := range []string{"help", "tp", "gamemode", "time", "say", "me", "kill", "seed"} {
		if slices.Contains(missing, name) {
			t.Errorf("/%s is implemented and reported missing", name)
		}
	}
	for _, name := range []string{"gamerule", "give", "ban"} {
		if !slices.Contains(missing, name) {
			t.Errorf("/%s is not implemented and not reported missing", name)
		}
	}
	if got := vanilla.Describe(merged); !strings.Contains(got, "vanilla 1.8 commands implemented") {
		t.Errorf("Describe said %q", got)
	}
}

func TestAStubRepliesUnimplementedRatherThanUnknown(t *testing.T) {
	srv, err := server.New(server.WithCommands(
		server.Merge(vanilla.Stubs(), server.BuiltinCommands()),
	))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caller := &recordingCaller{}
	srv.Dispatch(context.Background(), caller, "/gamerule keepInventory true")

	if len(caller.replies) == 0 {
		t.Fatal("a stub said nothing")
	}
	got := caller.replies[len(caller.replies)-1].Text
	if !strings.Contains(got, "not implemented") {
		t.Errorf("/gamerule replied %q, want that it is not implemented", got)
	}
	if strings.Contains(got, "Unknown") {
		t.Error("a stub reads as unknown; the whole point is that the two differ")
	}
}

func TestAStubSuggestsNoArgumentValues(t *testing.T) {
	// A stub that completed argument values would lead a player through a
	// command that does not run.
	srv, err := server.New(server.WithCommands(vanilla.Stubs()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := srv.Complete(context.Background(), &recordingCaller{}, "/gamerule "); len(got) != 0 {
		t.Errorf("a stub suggested %v", got)
	}
	// Its name is still suggested: the command exists, and hiding it is what
	// the stub list exists to stop.
	if got := srv.Complete(context.Background(), &recordingCaller{}, "/gamer"); len(got) != 1 {
		t.Errorf("completing /gamer gave %v, want /gamerule", got)
	}
}

// recordingCaller is the smallest Caller a stub can be run against.
type recordingCaller struct{ replies []server.Message }

func (c *recordingCaller) Name() string                       { return "Tester" }
func (c *recordingCaller) UUID() string                       { return "uuid-tester" }
func (c *recordingCaller) Position() server.Position          { return server.Position{} }
func (c *recordingCaller) Permission() server.PermissionLevel { return server.PermissionOwner }
func (c *recordingCaller) Reply(m server.Message)             { c.replies = append(c.replies, m) }
func (c *recordingCaller) Broadcast(server.Message)           {}
func (c *recordingCaller) Teleport(float64, float64, float64) {}
func (c *recordingCaller) Kill()                              {}

func (c *recordingCaller) SetGameMode(string) (string, bool) { return "", false }
