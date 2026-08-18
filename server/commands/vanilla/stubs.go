// Package vanilla is every command name Java Edition 1.8 answers to, each one
// saying it is not implemented.
//
// It exists so that the list of what a server still owes is visible rather than
// implicit. Without it, a player typing /gamerule on this server is told the
// command is unknown, which is wrong twice: the command exists in the game, and
// the reason it does nothing here is that nobody has written it, not that it
// was misspelled. A server builder gets the same answer from Missing().
//
// The names come from testdata/commands-1.8.txt, which is derived from the 1.8
// client language file rather than typed from memory. Its header says how, and
// which parts of it have no upstream source.
//
// Signatures are honest rather than precise. Where the vanilla usage line makes
// an argument's shape obvious, the parameter says so; where it does not, the
// stub takes the rest of the line. A signature that guesses is worse than one
// that admits it is a placeholder, because a guess drives suggestions and a
// player follows them.
package vanilla

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/go-theft-craft/server/server"
)

//go:embed testdata/commands-1.8.txt
var pinned string

// aliases are the names 1.8 accepts that the language file does not list.
//
// The language file has one usage key per command, so /msg and /w never appear
// beside /tell even though the game takes all three. These are hand-listed and
// this comment is the only thing marking them as such: they are the half of the
// list with no upstream source, and the next person checking it should start
// here.
var aliases = map[string][]string{
	"tell": {"msg", "w"},
}

// entry is one line of the pinned file.
type entry struct {
	name  string
	usage string
}

func parsePinned() []entry {
	var out []entry
	for _, line := range strings.Split(pinned, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, usage, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		out = append(out, entry{name: name, usage: usage})
	}

	return out
}

// Stubs returns every vanilla 1.8 command name, each unimplemented.
//
// Merge it *before* the implementations, so that a name this server does
// implement resolves to the implementation:
//
//	server.WithCommands(server.Merge(vanilla.Stubs(), server.BuiltinCommands()))
func Stubs() server.Set {
	entries := parsePinned()
	commands := make([]server.Command, 0, len(entries))

	for _, e := range entries {
		commands = append(commands, server.Command{
			Name:    e.name,
			Aliases: aliasesFor(e.name),
			// The vanilla usage line is the description, because it is the
			// most useful thing that can honestly be said about a command
			// nobody has written: it says what the command would take.
			Description: "not implemented — vanilla usage: " + e.usage,
			Permission:  server.PermissionGameplay,
			Signature:   placeholder(),
			// No Run. An unimplemented command is one with nothing to run, and
			// the dispatcher says so; a Run that returned ErrNotImplemented
			// would be a second way to say the same thing.
		})
	}

	set, err := server.NewSet(commands...)
	if err != nil {
		panic("vanilla: " + err.Error())
	}

	return set
}

// aliasesFor returns a command's aliases, minus any that repeat its own name.
func aliasesFor(name string) []string {
	var out []string
	for _, alias := range aliases[name] {
		if alias == name {
			continue
		}
		out = append(out, alias)
	}

	return out
}

// placeholder is the signature of a command whose arguments nobody has
// declared: everything, as one greedy parameter.
//
// It suggests nothing, which is the point. A stub that completed argument
// values would be leading a player through a command that does not run.
func placeholder() server.Signature {
	return server.Signature{Overloads: []server.Overload{{Params: []server.Param{
		{Name: "arguments", Type: server.ParamMessage, Optional: true, NoSuggest: true},
	}}}}
}

// Names is every pinned vanilla command name, in file order.
func Names() []string {
	entries := parsePinned()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.name)
	}

	return out
}

// Missing is every vanilla name that set does not implement.
//
// It is the question this package exists to answer: what does this server still
// owe somebody expecting a vanilla one.
func Missing(set server.Set) []string {
	var out []string
	for _, name := range Names() {
		cmd, ok := set.Lookup(name)
		if !ok || !cmd.Implemented() {
			out = append(out, name)
		}
	}

	return out
}

// Usage is the vanilla usage line for a pinned name.
func Usage(name string) (string, bool) {
	for _, e := range parsePinned() {
		if e.name == name {
			return e.usage, true
		}
	}

	return "", false
}

// Describe is a one-line summary of how much of vanilla this set covers.
func Describe(set server.Set) string {
	total := len(Names())
	missing := len(Missing(set))

	return fmt.Sprintf("%d of %d vanilla 1.8 commands implemented", total-missing, total)
}
