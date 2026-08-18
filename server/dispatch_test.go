package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/server"
)

// Commands, run with no connection.
//
// This is what moving them out of the connection package bought. Every one of
// the ten used to need a socket, a stream, a player manager, and a world to
// exercise, so the tests that existed asserted "some packet was written"; these
// assert the line a player actually reads.

// fakeCaller records everything a command did to whoever ran it.
type fakeCaller struct {
	name       string
	uuid       string
	pos        server.Position
	permission server.PermissionLevel

	replies    []server.Message
	broadcasts []server.Message
	teleports  [][3]float64
	gameMode   string
	killed     bool
	// modes is what SetGameMode accepts; anything else is refused, which is
	// what the connection's own resolver does.
	modes map[string]string
}

func newFakeCaller(name string) *fakeCaller {
	return &fakeCaller{
		name: name,
		uuid: "uuid-" + name,
		pos:  server.Position{X: 10.7, Y: 65.2, Z: -3.4},
		modes: map[string]string{
			"survival": "survival", "s": "survival", "0": "survival",
			"creative": "creative", "c": "creative", "1": "creative",
			"adventure": "adventure", "a": "adventure", "2": "adventure",
			"spectator": "spectator", "sp": "spectator", "3": "spectator",
		},
	}
}

func (f *fakeCaller) Name() string                       { return f.name }
func (f *fakeCaller) UUID() string                       { return f.uuid }
func (f *fakeCaller) Position() server.Position          { return f.pos }
func (f *fakeCaller) Permission() server.PermissionLevel { return f.permission }
func (f *fakeCaller) Reply(m server.Message)             { f.replies = append(f.replies, m) }
func (f *fakeCaller) Broadcast(m server.Message)         { f.broadcasts = append(f.broadcasts, m) }
func (f *fakeCaller) Teleport(x, y, z float64) {
	f.teleports = append(f.teleports, [3]float64{x, y, z})
}
func (f *fakeCaller) Kill() { f.killed = true }

func (f *fakeCaller) SetGameMode(name string) (string, bool) {
	resolved, ok := f.modes[strings.ToLower(name)]
	if ok {
		f.gameMode = resolved
	}

	return resolved, ok
}

// lastReply is the last thing the caller was told, or "" if nothing.
func (f *fakeCaller) lastReply() string {
	if len(f.replies) == 0 {
		return ""
	}

	return f.replies[len(f.replies)-1].Text
}

// fakeServices is the server half a command talks to.
type fakeServices struct {
	seed      int64
	players   []string
	positions map[string]server.Position
	set       server.Set
	timeSet   int64
	saved     bool
	saveErr   error
}

func newFakeServices(set server.Set) *fakeServices {
	return &fakeServices{
		seed:    1234,
		players: []string{"Alice", "Bob"},
		positions: map[string]server.Position{
			"Bob": {X: 50, Y: 20, Z: 50},
		},
		set: set,
	}
}

func (s *fakeServices) Seed() int64             { return s.seed }
func (s *fakeServices) OnlinePlayers() []string { return s.players }

func (s *fakeServices) PlayerPosition(name string) (server.Position, bool) {
	pos, ok := s.positions[name]

	return pos, ok
}

func (s *fakeServices) SetTimeOfDay(ticks int64) { s.timeSet = ticks }

func (s *fakeServices) Save(context.Context) error {
	s.saved = true

	return s.saveErr
}

func (s *fakeServices) Commands() server.Set { return s.set }

// dispatchOn runs a line against a bare server carrying the built-ins, with
// services faked. It is the whole harness: no socket, no world, no player.
func dispatchOn(t *testing.T, caller server.Caller, svc server.Services, line string) *server.Server {
	t.Helper()

	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !srv.Dispatch(server.WithServices(context.Background(), svc), caller, line) {
		t.Fatalf("%q was not treated as a command", line)
	}

	return srv
}

func TestDispatchNeedsNoConnection(t *testing.T) {
	caller := newFakeCaller("Alice")
	svc := newFakeServices(server.BuiltinCommands())

	dispatchOn(t, caller, svc, "/seed")

	if !strings.Contains(caller.lastReply(), "1234") {
		t.Errorf("/seed replied %q, want the seed in it", caller.lastReply())
	}
}

func TestEveryBuiltinRunsAgainstAFakeCaller(t *testing.T) {
	set := server.BuiltinCommands()

	for _, tc := range []struct {
		line    string
		check   func(t *testing.T, c *fakeCaller, s *fakeServices)
		nothing bool
	}{
		{line: "/help", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.replies) < 2 {
				t.Errorf("/help said %d lines, want the header and one per command", len(c.replies))
			}
		}},
		{line: "/help tp", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if !strings.Contains(c.lastReply(), "/tp <player>") {
				t.Errorf("/help tp replied %q, want the usage", c.lastReply())
			}
		}},
		{line: "/list", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if !strings.Contains(c.lastReply(), "Alice") || !strings.Contains(c.lastReply(), "Bob") {
				t.Errorf("/list replied %q, want both players", c.lastReply())
			}
		}},
		{line: "/tp 100 10 100", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.teleports) != 1 || c.teleports[0] != [3]float64{100, 10, 100} {
				t.Errorf("/tp teleported to %v, want 100 10 100", c.teleports)
			}
		}},
		{line: "/tp Bob", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.teleports) != 1 || c.teleports[0] != [3]float64{50, 20, 50} {
				t.Errorf("/tp Bob teleported to %v, want Bob's position", c.teleports)
			}
		}},
		{line: "/tp Nobody", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.teleports) != 0 {
				t.Error("/tp to an offline player teleported anyway")
			}
			if !strings.Contains(c.lastReply(), "not found") {
				t.Errorf("/tp Nobody replied %q, want a not-found", c.lastReply())
			}
		}},
		{line: "/tp 1 2", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			// The ambiguous-arity case: the reply has to name both shapes.
			for _, shape := range []string{"/tp <player>", "/tp <x> <y> <z>"} {
				if !strings.Contains(c.lastReply(), shape) {
					t.Errorf("/tp 1 2 replied %q, want %q named", c.lastReply(), shape)
				}
			}
		}},
		{line: "/gamemode creative", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if c.gameMode != "creative" {
				t.Errorf("/gamemode creative set %q", c.gameMode)
			}
		}},
		{line: "/gamemode sp", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			// The short forms still work, because they always did.
			if c.gameMode != "spectator" {
				t.Errorf("/gamemode sp set %q, want spectator", c.gameMode)
			}
		}},
		{line: "/gamemode wrong", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if c.gameMode != "" {
				t.Errorf("/gamemode wrong set %q", c.gameMode)
			}
			if !strings.Contains(c.lastReply(), "survival") {
				t.Errorf("/gamemode wrong replied %q, want the choices listed", c.lastReply())
			}
		}},
		{line: "/time set night", check: func(t *testing.T, _ *fakeCaller, s *fakeServices) {
			if s.timeSet != 13000 {
				t.Errorf("/time set night set %d, want 13000", s.timeSet)
			}
		}},
		{line: "/time set 4200", check: func(t *testing.T, _ *fakeCaller, s *fakeServices) {
			if s.timeSet != 4200 {
				t.Errorf("/time set 4200 set %d", s.timeSet)
			}
		}},
		{line: "/time set nope", check: func(t *testing.T, _ *fakeCaller, s *fakeServices) {
			if s.timeSet != 0 {
				t.Errorf("/time set nope set %d, want nothing", s.timeSet)
			}
		}},
		{line: "/say hello there", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.broadcasts) != 1 {
				t.Fatalf("/say broadcast %d times", len(c.broadcasts))
			}
			if !strings.Contains(c.broadcasts[0].Text, "[Server] hello there") {
				t.Errorf("/say broadcast %q", c.broadcasts[0].Text)
			}
		}},
		{line: "/me waves", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if len(c.broadcasts) != 1 {
				t.Fatalf("/me broadcast %d times", len(c.broadcasts))
			}
			// Still a translated component, which is why Message carries one.
			if c.broadcasts[0].Translate != "chat.type.emote" {
				t.Errorf("/me broadcast %+v, want the emote translation", c.broadcasts[0])
			}
			if len(c.broadcasts[0].With) != 2 || c.broadcasts[0].With[0] != "Alice" {
				t.Errorf("/me arguments are %v, want the name and the action", c.broadcasts[0].With)
			}
		}},
		{line: "/kill", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if !c.killed {
				t.Error("/kill did not kill")
			}
		}},
		{line: "/seed", check: func(t *testing.T, c *fakeCaller, _ *fakeServices) {
			if !strings.Contains(c.lastReply(), "1234") {
				t.Errorf("/seed replied %q", c.lastReply())
			}
		}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			caller := newFakeCaller("Alice")
			svc := newFakeServices(set)
			dispatchOn(t, caller, svc, tc.line)
			tc.check(t, caller, svc)
		})
	}
}

func TestAnUnknownCommandRepliesUnknown(t *testing.T) {
	caller := newFakeCaller("Alice")
	dispatchOn(t, caller, newFakeServices(server.BuiltinCommands()), "/nosuchcmd")

	if !strings.Contains(caller.lastReply(), "Unknown command") {
		t.Errorf("an unknown command replied %q", caller.lastReply())
	}
}

func TestAnUnimplementedCommandRepliesUnimplemented(t *testing.T) {
	// The distinction the design argues for: unknown means a typo,
	// unimplemented means a to-do, and a server builder needs to tell them
	// apart — so does a player, who should stop retyping a command that exists.
	stubs, err := server.NewSet(server.Command{Name: "ban", Description: "Ban a player"})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	srv, err := server.New(server.WithCommands(stubs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caller := newFakeCaller("Alice")
	srv.Dispatch(context.Background(), caller, "/ban Bob")

	if !strings.Contains(caller.lastReply(), "not implemented") {
		t.Errorf("a stub replied %q, want that it is not implemented", caller.lastReply())
	}
	if strings.Contains(caller.lastReply(), "Unknown") {
		t.Error("a stub reads as an unknown command; the two have to be distinguishable")
	}
}

func TestAChatLineIsNotACommand(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caller := newFakeCaller("Alice")

	if srv.Dispatch(context.Background(), caller, "hello everyone") {
		t.Error("a chat line was treated as a command")
	}
	if len(caller.replies) != 0 {
		t.Errorf("a chat line produced %d replies", len(caller.replies))
	}
}

func TestSaveRunsThroughServices(t *testing.T) {
	caller := newFakeCaller("Alice")
	svc := newFakeServices(server.BuiltinCommands())

	dispatchOn(t, caller, svc, "/save")

	// The save itself runs off this goroutine, as it always did, so what this
	// asserts is that the player was told it started.
	if !strings.Contains(caller.replies[0].Text, "Saving") {
		t.Errorf("/save replied %q", caller.replies[0].Text)
	}
}
