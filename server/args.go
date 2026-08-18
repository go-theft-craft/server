package server

import (
	"fmt"
	"strconv"
	"strings"
)

// Parsed arguments.
//
// The dispatcher parses against the signature and hands the handler an Args, so
// nothing re-parses. That is the whole point: when every handler parsed its own
// words, every handler had its own idea of what a bad argument looked like, and
// /tp's idea and /time's idea were both written from scratch.

// Args is one command invocation's arguments, resolved against one overload.
type Args struct {
	overload int
	values   map[string]string
}

// Overload is which shape of the signature matched, indexed from zero. A
// command with two shapes reads it to tell them apart; one with a single shape
// never needs it.
func (a Args) Overload() int { return a.overload }

// Has reports whether an optional parameter was given.
func (a Args) Has(name string) bool {
	_, ok := a.values[name]

	return ok
}

// String is a parameter's raw text, or "" if it was not given.
func (a Args) String(name string) string { return a.values[name] }

// Int is a parameter parsed as a whole number. The parser has already rejected
// anything that is not one, so ok is false only for a parameter that was not
// given.
func (a Args) Int(name string) (int, bool) {
	raw, ok := a.values[name]
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(raw)

	return v, err == nil
}

// Float is a parameter parsed as a number.
func (a Args) Float(name string) (float64, bool) {
	raw, ok := a.values[name]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)

	return v, err == nil
}

// ParseError is a line that did not match the signature.
//
// It carries the command so the message can name every shape the command
// accepts rather than just the one the parser gave up on. "usage: /tp" tells a
// player nothing they did not already know.
type ParseError struct {
	Command *Command
	Reason  string
}

func (e *ParseError) Error() string { return e.Reason }

// Message is what the caller is told.
func (e *ParseError) Message() Message {
	if e.Command == nil {
		return Error(e.Reason)
	}

	return Error(e.Reason + " Usage: " + e.Command.Usage())
}

// split breaks a command line into its name and its words.
//
// Splitting on runs of whitespace is what the old implementation did and what
// the client's own suggestion behavior assumes; a quoted-string grammar is a
// different feature and 1.8 has no use for one.
func splitLine(line string) (name string, words []string) {
	fields := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(fields) == 0 {
		return "", nil
	}

	return strings.ToLower(fields[0]), fields[1:]
}

// parse resolves words against a command's signature.
func parse(cmd *Command, words []string) (Args, error) {
	overloads := cmd.Signature.Overloads
	if len(overloads) == 0 {
		if len(words) > 0 {
			return Args{}, &ParseError{Command: cmd, Reason: fmt.Sprintf("/%s takes no arguments.", cmd.Name)}
		}

		return Args{values: map[string]string{}}, nil
	}

	// Candidates are the overloads whose arity the line could satisfy. More
	// than one is not an error yet: two shapes of the same length are resolved
	// by which one actually parses.
	var candidates []int
	for i, o := range overloads {
		minimum, maximum := o.arity()
		if len(words) < minimum {
			continue
		}
		if !o.greedy() && len(words) > maximum {
			continue
		}
		candidates = append(candidates, i)
	}

	if len(candidates) == 0 {
		return Args{}, &ParseError{Command: cmd, Reason: arityError(cmd, len(words))}
	}

	var firstErr error
	for _, i := range candidates {
		args, err := parseOverload(cmd, i, overloads[i], words)
		if err == nil {
			return args, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	return Args{}, firstErr
}

// arityError names every shape the command accepts.
//
// This is the /tp case: two arguments match neither <player> nor <x> <y> <z>,
// and "usage: /tp" would leave the player to guess which of the two they were
// closer to.
func arityError(cmd *Command, given int) string {
	shapes := make([]string, 0, len(cmd.Signature.Overloads))
	for _, o := range cmd.Signature.Overloads {
		shapes = append(shapes, "/"+cmd.Name+o.usage())
	}

	word := "arguments"
	if given == 1 {
		word = "argument"
	}

	return fmt.Sprintf("/%s does not take %d %s. It takes %s.",
		cmd.Name, given, word, strings.Join(shapes, " or "))
}

func parseOverload(cmd *Command, index int, o Overload, words []string) (Args, error) {
	values := make(map[string]string, len(o.Params))

	for i, p := range o.Params {
		if i >= len(words) {
			// Only optional parameters can be missing; arity filtering above
			// guarantees the required ones are present.
			break
		}

		raw := words[i]
		if p.Type == ParamMessage {
			raw = strings.Join(words[i:], " ")
		}

		if err := checkParam(cmd, p, raw); err != nil {
			return Args{}, err
		}
		values[p.Name] = raw

		if p.Type == ParamMessage {
			break
		}
	}

	return Args{overload: index, values: values}, nil
}

func checkParam(cmd *Command, p Param, raw string) error {
	if len(p.Choices) > 0 {
		for _, choice := range p.Choices {
			if strings.EqualFold(choice, raw) {
				return nil
			}
		}

		return &ParseError{Command: cmd, Reason: fmt.Sprintf(
			"%q is not one of %s.", raw, strings.Join(p.Choices, ", "),
		)}
	}

	switch p.Type {
	case ParamInt:
		if _, err := strconv.Atoi(raw); err != nil {
			return &ParseError{Command: cmd, Reason: fmt.Sprintf("%q is not a whole number.", raw)}
		}
	case ParamFloat, ParamCoordinates:
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return &ParseError{Command: cmd, Reason: fmt.Sprintf("%q is not a number.", raw)}
		}
	case ParamDuration:
		if !isDuration(raw) {
			return &ParseError{Command: cmd, Reason: fmt.Sprintf(
				"%q is not a time. Use day, night, noon, midnight, or a number of ticks.", raw,
			)}
		}
	case ParamWord, ParamMessage, ParamPlayer:
		// Anything non-empty. A player name that is offline is the command's
		// to report, not the parser's: "no such player" is a better message
		// than "not a player".
	}

	return nil
}

// durationNames are the times of day /time accepts by name.
var durationNames = []string{"day", "night", "noon", "midnight"}

func isDuration(raw string) bool {
	for _, name := range durationNames {
		if strings.EqualFold(name, raw) {
			return true
		}
	}
	_, err := strconv.ParseInt(raw, 10, 64)

	return err == nil
}

// DurationTicks resolves a ParamDuration value to a time of day.
//
// It is exported because a command reads it and the mapping — which tick "noon"
// is — belongs beside the parser that accepts the word, not in the command.
func DurationTicks(raw string) (int64, bool) {
	switch strings.ToLower(raw) {
	case "day":
		return 1000, true
	case "noon":
		return 6000, true
	case "night":
		return 13000, true
	case "midnight":
		return 18000, true
	}

	v, err := strconv.ParseInt(raw, 10, 64)

	return v, err == nil
}

// ParseLine resolves a whole command line against a set.
//
// It is exported so a caller can validate a line without running it, and so a
// test about parsing does not have to go through a dispatch that also needs a
// caller and a server.
func ParseLine(set Set, line string) (Args, error) {
	name, words := splitLine(line)
	cmd, ok := set.Lookup(name)
	if !ok {
		return Args{}, &ParseError{Reason: fmt.Sprintf("Unknown command: /%s.", name)}
	}

	return parse(cmd, words)
}
