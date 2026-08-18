package server_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/server"
)

// Parsing, overload resolution, and the errors.
//
// The errors are the interesting half. A parser that accepts the right lines
// and says "usage: /tp" for the wrong ones has moved the problem rather than
// solved it, and the player is still guessing.

// parseFor is the parse a dispatch would do, exposed for a test that is about
// parsing rather than about running.
func parseFor(t *testing.T, cmd server.Command, line string) (server.Args, error) {
	t.Helper()

	set, err := server.NewSet(cmd)
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}

	return server.ParseLine(set, line)
}

func word(name string) server.Param  { return server.Param{Name: name, Type: server.ParamWord} }
func intp(name string) server.Param  { return server.Param{Name: name, Type: server.ParamInt} }
func coord(name string) server.Param { return server.Param{Name: name, Type: server.ParamCoordinates} }

func one(params ...server.Param) server.Signature {
	return server.Signature{Overloads: []server.Overload{{Params: params}}}
}

func TestAWordParamParses(t *testing.T) {
	cmd := server.Command{Name: "echo", Signature: one(word("what"))}

	args, err := parseFor(t, cmd, "/echo hello")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := args.String("what"); got != "hello" {
		t.Errorf("what = %q, want hello", got)
	}
}

func TestAnIntParamRejectsNonNumbers(t *testing.T) {
	cmd := server.Command{Name: "page", Signature: one(intp("n"))}

	if _, err := parseFor(t, cmd, "/page 3"); err != nil {
		t.Fatalf("a whole number was rejected: %v", err)
	}

	_, err := parseFor(t, cmd, "/page three")
	if err == nil {
		t.Fatal("\"three\" was accepted as a whole number")
	}
	if !strings.Contains(err.Error(), "three") {
		t.Errorf("the error does not name what was wrong: %v", err)
	}
}

func TestChoicesRejectAValueOutsideThem(t *testing.T) {
	cmd := server.Command{Name: "mode", Signature: one(server.Param{
		Name: "mode", Type: server.ParamWord, Choices: []string{"survival", "creative"},
	})}

	if _, err := parseFor(t, cmd, "/mode CREATIVE"); err != nil {
		t.Fatalf("a choice was rejected for its case: %v", err)
	}

	_, err := parseFor(t, cmd, "/mode spectacular")
	if err == nil {
		t.Fatal("a value outside the choices was accepted")
	}
	// The error lists them, because a player who typed the wrong one wants to
	// know the right ones, not that they were wrong.
	if !strings.Contains(err.Error(), "survival") || !strings.Contains(err.Error(), "creative") {
		t.Errorf("the error does not list the choices: %v", err)
	}
}

func TestOverloadResolutionPicksByArity(t *testing.T) {
	cmd := server.Command{Name: "tp", Signature: server.Signature{Overloads: []server.Overload{
		{Params: []server.Param{{Name: "player", Type: server.ParamPlayer}}},
		{Params: []server.Param{coord("x"), coord("y"), coord("z")}},
	}}}

	args, err := parseFor(t, cmd, "/tp Alice")
	if err != nil {
		t.Fatalf("one argument: %v", err)
	}
	if args.Overload() != 0 || args.String("player") != "Alice" {
		t.Errorf("one argument resolved to overload %d, player %q", args.Overload(), args.String("player"))
	}

	args, err = parseFor(t, cmd, "/tp 1 2 3")
	if err != nil {
		t.Fatalf("three arguments: %v", err)
	}
	if args.Overload() != 1 {
		t.Errorf("three arguments resolved to overload %d, want 1", args.Overload())
	}
	if x, ok := args.Float("x"); !ok || x != 1 {
		t.Errorf("x = %v, %v; want 1", x, ok)
	}
}

func TestAnAmbiguousArityErrorNamesBothShapes(t *testing.T) {
	// The /tp case. Two arguments match neither <player> nor <x> <y> <z>, and
	// "usage: /tp" would leave the player to guess which one they were closer
	// to. The error has to say what the two shapes are.
	cmd := server.Command{Name: "tp", Signature: server.Signature{Overloads: []server.Overload{
		{Params: []server.Param{{Name: "player", Type: server.ParamPlayer}}},
		{Params: []server.Param{coord("x"), coord("y"), coord("z")}},
	}}}

	_, err := parseFor(t, cmd, "/tp 1 2")
	if err == nil {
		t.Fatal("two arguments matched a command that takes one or three")
	}
	for _, shape := range []string{"/tp <player>", "/tp <x> <y> <z>"} {
		if !strings.Contains(err.Error(), shape) {
			t.Errorf("the error does not name %q: %v", shape, err)
		}
	}
}

func TestOptionalTrailingParamsResolve(t *testing.T) {
	cmd := server.Command{Name: "help", Signature: one(
		server.Param{Name: "command", Type: server.ParamWord, Optional: true},
	)}

	args, err := parseFor(t, cmd, "/help")
	if err != nil {
		t.Fatalf("omitting an optional parameter: %v", err)
	}
	if args.Has("command") {
		t.Error("an optional parameter that was not given reads as given")
	}

	args, err = parseFor(t, cmd, "/help tp")
	if err != nil {
		t.Fatalf("supplying an optional parameter: %v", err)
	}
	if !args.Has("command") || args.String("command") != "tp" {
		t.Errorf("command = %q, %v; want tp", args.String("command"), args.Has("command"))
	}
}

func TestAMessageParamTakesTheRestOfTheLine(t *testing.T) {
	cmd := server.Command{Name: "say", Signature: one(
		server.Param{Name: "message", Type: server.ParamMessage},
	)}

	args, err := parseFor(t, cmd, "/say hello   there  world")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Runs of whitespace collapse, which is what the old implementation did:
	// it joined strings.Fields with a single space.
	if got := args.String("message"); got != "hello there world" {
		t.Errorf("message = %q, want %q", got, "hello there world")
	}
}

func TestAGreedyParamMustBeLast(t *testing.T) {
	// Rejected at construction rather than at the first call: a signature with
	// a greedy parameter in the middle produces a command that parses
	// plausibly and completes nonsense, and the moment to find that out is the
	// moment somebody wrote it.
	_, err := server.NewSet(server.Command{Name: "bad", Signature: one(
		server.Param{Name: "message", Type: server.ParamMessage},
		word("trailing"),
	)})
	if err == nil {
		t.Fatal("a signature with a greedy parameter in the middle was accepted")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("the error does not name the offending parameter: %v", err)
	}
}

func TestARequiredParamAfterAnOptionalOneIsRejected(t *testing.T) {
	_, err := server.NewSet(server.Command{Name: "bad", Signature: one(
		server.Param{Name: "first", Type: server.ParamWord, Optional: true},
		word("second"),
	)})
	if err == nil {
		t.Fatal("a required parameter after an optional one was accepted")
	}
}

func TestTwoCommandsCannotAnswerToTheSameName(t *testing.T) {
	_, err := server.NewSet(
		server.Command{Name: "tp"},
		server.Command{Name: "teleport", Aliases: []string{"tp"}},
	)
	if err == nil {
		t.Fatal("two commands answering to /tp were accepted")
	}
}

func TestUsageIsDerivedFromTheSignature(t *testing.T) {
	// Derived rather than stored, so a usage line cannot drift from the
	// arguments the parser actually accepts.
	cmd := server.Command{Name: "tp", Signature: server.Signature{Overloads: []server.Overload{
		{Params: []server.Param{{Name: "player", Type: server.ParamPlayer}}},
		{Params: []server.Param{coord("x"), coord("y"), coord("z")}},
	}}}

	if want := "/tp <player> | /tp <x> <y> <z>"; cmd.Usage() != want {
		t.Errorf("usage is %q, want %q", cmd.Usage(), want)
	}

	choices := server.Command{Name: "gamemode", Signature: one(server.Param{
		Name: "mode", Type: server.ParamWord, Choices: []string{"survival", "creative"},
	})}
	if want := "/gamemode <survival|creative>"; choices.Usage() != want {
		t.Errorf("usage is %q, want %q", choices.Usage(), want)
	}

	optional := server.Command{Name: "help", Signature: one(
		server.Param{Name: "command", Type: server.ParamWord, Optional: true},
	)}
	if want := "/help [command]"; optional.Usage() != want {
		t.Errorf("usage is %q, want %q", optional.Usage(), want)
	}
}

func TestANotImplementedCommandSaysSo(t *testing.T) {
	set, err := server.NewSet(server.Command{Name: "ban"})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	cmd, ok := set.Lookup("ban")
	if !ok {
		t.Fatal("the command did not resolve")
	}
	if cmd.Implemented() {
		t.Error("a command with no Run reports itself implemented")
	}
	if !errors.Is(server.ErrNotImplemented, server.ErrNotImplemented) {
		t.Error("ErrNotImplemented is not itself")
	}
}

func TestMergePrefersTheLaterSet(t *testing.T) {
	stubs, err := server.NewSet(server.Command{Name: "kill"}, server.Command{Name: "ban"})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	real, err := server.NewSet(server.Command{
		Name: "kill",
		Run:  func(_ context.Context, _ server.Caller, _ server.Args) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}

	merged := server.Merge(stubs, real)

	kill, ok := merged.Lookup("kill")
	if !ok {
		t.Fatal("/kill did not resolve after merging")
	}
	if !kill.Implemented() {
		t.Error("the stub won over the implementation; the later set should replace the earlier")
	}
	if _, ok := merged.Lookup("ban"); !ok {
		t.Error("/ban was lost in the merge")
	}
	// Two names, two commands: the replaced stub does not linger in All().
	if got := len(merged.All()); got != 2 {
		t.Errorf("the merged set holds %d commands, want 2", got)
	}
}
