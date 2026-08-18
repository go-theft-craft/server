package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Commands as values.
//
// A command used to be a name, a usage string, and a function taking the whole
// connection and a slice of unparsed words. Three things followed from that,
// and all three are what this file exists to stop:
//
//   - A command could not be tested without a connection, so most were not.
//   - Every handler parsed its own arguments, so every handler had its own idea
//     of what a bad one looked like.
//   - Tab-complete knew the argument shapes in a switch statement of its own,
//     so execution and suggestion could disagree, and nothing would say so.
//
// A Signature is declared once and drives all three: the parser, the
// suggestions, and — for a protocol that has one — the command tree sent to the
// client. Adding an argument in one place and not the others stops being
// possible rather than stopping being likely.

// ParamType is what one argument is.
//
// Seven types cover the ten built-in commands. The list is open for extension
// and closed for improvisation: a command needing an eighth adds it here, where
// the brigadier mapping in server/commands/v775 will see it and refuse to build
// until it has an answer for it.
type ParamType string

// The parameter types.
const (
	// ParamWord is one word of free text.
	ParamWord ParamType = "word"
	// ParamMessage is the rest of the line. It is greedy by definition, so it
	// must be the last parameter of its overload; NewSet refuses a signature
	// where it is not.
	ParamMessage ParamType = "message"
	// ParamInt is a whole number.
	ParamInt ParamType = "int"
	// ParamFloat is a number.
	ParamFloat ParamType = "float"
	// ParamPlayer is the name of an online player. It completes to the online
	// list; it does not refuse a name that is offline, because a command that
	// wants to say "no such player" says it better than a parser can.
	ParamPlayer ParamType = "player"
	// ParamCoordinates is one axis. Three of them make a position.
	ParamCoordinates ParamType = "coordinates"
	// ParamDuration is a time of day: day, night, noon, midnight, or a tick
	// count.
	ParamDuration ParamType = "duration"
	// ParamCommand is the name of a command in this server's own set. It is
	// the eighth type, and the design named seven: /help's argument is a
	// command name, and completing it from Set.All() is what removed the
	// special case tab-complete used to carry for /help alone.
	ParamCommand ParamType = "command"
)

// Param is one argument of one overload.
type Param struct {
	// Name is what Args looks it up by, and what the usage line calls it.
	Name string
	Type ParamType
	// Choices is a fixed set of accepted values, and the set that is
	// suggested. A value outside Choices and Also is rejected.
	Choices []string
	// Also is accepted and never suggested.
	//
	// /gamemode is why it exists: it takes "sp" and "3" as well as
	// "spectator", because it always did and taking them away would change
	// what a player can type — but offering all twelve in a suggestion list
	// buries the four that are worth reading.
	Also []string
	// NoSuggest silences this parameter, leaving the position to whatever
	// another overload offers there.
	//
	// /tp is why it exists: its first argument is either a player name or an
	// x coordinate, and suggesting both puts the caller's own x in a list of
	// player names, where "10" reads like somebody called 10. The y and z
	// coordinates have no such competition and do suggest.
	NoSuggest bool
	// Optional parameters may be left off the end of the line. A required
	// parameter after an optional one is a signature error.
	Optional bool
}

// accepts reports whether a fixed-set parameter takes a value.
func (p Param) accepts(value string) bool {
	for _, choice := range append(append([]string(nil), p.Choices...), p.Also...) {
		if strings.EqualFold(choice, value) {
			return true
		}
	}

	return false
}

// fixed reports whether the parameter has a fixed set of values at all.
func (p Param) fixed() bool { return len(p.Choices) > 0 || len(p.Also) > 0 }

// Overload is one shape a command's arguments can take.
type Overload struct {
	Params []Param
}

// Signature is every shape a command accepts.
//
// A command with no arguments has one overload with no parameters, spelled out
// rather than left empty, so "this command takes nothing" and "nobody declared
// this command's arguments" are different states.
type Signature struct {
	Overloads []Overload
}

// PermissionLevel is how privileged a caller is.
//
// The numbers are vanilla's: 0 is anyone, 1 through 4 are the operator levels,
// where 4 is the console. Nothing in this server assigns them by itself — see
// AllowAll and WithAuthorizer.
type PermissionLevel int

// The vanilla permission levels.
const (
	PermissionEveryone PermissionLevel = 0
	PermissionModerate PermissionLevel = 1
	PermissionGameplay PermissionLevel = 2
	PermissionAdmin    PermissionLevel = 3
	PermissionOwner    PermissionLevel = 4
)

// ErrNotImplemented is what a stub returns.
//
// It is distinct from "unknown command" on purpose: unknown means a typo and
// unimplemented means a to-do, and somebody building a server on this needs to
// tell the two apart. See server/commands/vanilla.
var ErrNotImplemented = errors.New("server: command is not implemented")

// Command is one command, as a value an application can write and register.
type Command struct {
	Name    string
	Aliases []string
	// Description is the one line /help prints.
	Description string
	Permission  PermissionLevel
	Signature   Signature
	// Run does the work. A nil Run is a stub: it replies that the command is
	// not implemented, which is what vanilla.Stubs() is made of.
	Run func(ctx context.Context, caller Caller, args Args) error
}

// Implemented reports whether this command does anything.
func (c *Command) Implemented() bool { return c.Run != nil }

// Usage renders the signature as the line a player is shown.
//
// It is derived rather than stored, so a usage line cannot drift from the
// arguments the parser actually accepts — which is the specific way the old
// hand-written usage strings went wrong.
func (c *Command) Usage() string {
	if len(c.Signature.Overloads) == 0 {
		return "/" + c.Name
	}

	shapes := make([]string, 0, len(c.Signature.Overloads))
	for _, o := range c.Signature.Overloads {
		shapes = append(shapes, "/"+c.Name+o.usage())
	}

	return strings.Join(shapes, " | ")
}

func (o Overload) usage() string {
	var b strings.Builder
	for _, p := range o.Params {
		b.WriteByte(' ')
		open, close := "<", ">"
		if p.Optional {
			open, close = "[", "]"
		}
		b.WriteString(open)
		if len(p.Choices) > 0 {
			b.WriteString(strings.Join(p.Choices, "|"))
		} else {
			b.WriteString(p.Name)
		}
		b.WriteString(close)
	}

	return b.String()
}

// arity is how many parameters an overload takes, at least and at most.
func (o Overload) arity() (minimum, maximum int) {
	for _, p := range o.Params {
		maximum++
		if !p.Optional {
			minimum++
		}
	}

	return minimum, maximum
}

// greedy reports whether the overload ends in a parameter that takes the rest
// of the line, in which case there is no maximum arity.
func (o Overload) greedy() bool {
	return len(o.Params) > 0 && o.Params[len(o.Params)-1].Type == ParamMessage
}

// Set is a named collection of commands, with their aliases resolved.
//
// The zero value is empty and usable: Lookup finds nothing and All returns
// nothing, which is what a server built without commands has.
type Set struct {
	commands []*Command
	byName   map[string]*Command
}

// NewSet builds a set, validating every signature.
//
// Validation happens here rather than at the first call, because a signature
// with a greedy parameter in the middle produces a command that parses
// plausibly and completes nonsense, and the moment to find that out is the
// moment somebody wrote it.
func NewSet(commands ...Command) (Set, error) {
	s := Set{byName: make(map[string]*Command, len(commands))}

	for i := range commands {
		cmd := &commands[i]
		if err := validate(cmd); err != nil {
			return Set{}, err
		}
		for _, name := range append([]string{cmd.Name}, cmd.Aliases...) {
			key := strings.ToLower(name)
			if _, taken := s.byName[key]; taken {
				return Set{}, fmt.Errorf("server: two commands answer to %q", key)
			}
			s.byName[key] = cmd
		}
		s.commands = append(s.commands, cmd)
	}

	return s, nil
}

func validate(c *Command) error {
	if c.Name == "" {
		return errors.New("server: a command with no name")
	}

	for _, o := range c.Signature.Overloads {
		seenOptional := false
		for i, p := range o.Params {
			if p.Name == "" {
				return fmt.Errorf("server: /%s has an unnamed parameter", c.Name)
			}
			if p.Type == ParamMessage && i != len(o.Params)-1 {
				return fmt.Errorf("server: /%s takes %q as the rest of the line but has parameters after it",
					c.Name, p.Name)
			}
			if p.Optional {
				seenOptional = true
			} else if seenOptional {
				return fmt.Errorf("server: /%s has required parameter %q after an optional one",
					c.Name, p.Name)
			}
		}
	}

	return nil
}

// Lookup finds a command by name or alias, case-insensitively.
func (s Set) Lookup(name string) (*Command, bool) {
	cmd, ok := s.byName[strings.ToLower(strings.TrimPrefix(name, "/"))]

	return cmd, ok
}

// All is every command in the set, in registration order, deduplicated by
// identity: a command with three aliases appears once.
func (s Set) All() []*Command { return slices.Clone(s.commands) }

// Names is every name and alias the set answers to, sorted.
func (s Set) Names() []string {
	names := make([]string, 0, len(s.byName))
	for name := range s.byName {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// Merge combines sets, with a later set replacing an earlier one for any name
// they both answer to.
//
// That order is what makes vanilla.Stubs() useful: a server merges the stub
// list first and its own implementations second, so every vanilla name resolves
// and the implemented ones win. An error can only come from the combined set
// disagreeing with itself, which Merge resolves rather than reports.
func Merge(sets ...Set) Set {
	out := Set{byName: map[string]*Command{}}
	replaced := map[*Command]bool{}

	for _, s := range sets {
		for _, name := range s.Names() {
			if previous, taken := out.byName[name]; taken {
				replaced[previous] = true
			}
			out.byName[name] = s.byName[name]
		}
		out.commands = append(out.commands, s.commands...)
	}

	kept := out.commands[:0]
	for _, cmd := range out.commands {
		if replaced[cmd] {
			continue
		}
		kept = append(kept, cmd)
	}
	out.commands = kept

	return out
}
