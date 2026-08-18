package server

import (
	"context"
	"math"
	"strconv"
	"strings"
)

// Completion, derived from the same signatures execution parses against.
//
// This replaces a switch statement with a case per command and per argument
// index. The switch was the only record of what this server suggests, which is
// why the whole table was pinned as a test before it was deleted: a rewrite
// that quietly dropped a suggestion would have looked exactly like a rewrite
// that kept them all.
//
// Two behaviors from that switch are deliberate and survive. Both have their
// reasons here, because a reason that is deleted has to be rediscovered:
//
//   - Coordinates complete to the block the caller is standing in, not to the
//     block they are looking at. Vanilla completes to the latter; this server
//     has no ray cast, and the standing position is the more useful of the two
//     for typing a destination near where you are.
//   - A candidate that does not extend what was typed is dropped rather than
//     sent empty, because an empty candidate takes a line in the client's
//     suggestion list.

// Complete returns the tab-complete matches for a partial line.
//
// A line with no leading slash is chat, and chat completes player names, which
// is what the client uses for @-less mentions.
func (s *Server) Complete(ctx context.Context, caller Caller, line string) []string {
	svc := s.servicesFor(ctx)

	if !strings.HasPrefix(line, "/") {
		return matchNames(lastWord(line), svc.OnlinePlayers())
	}

	return s.completeCommand(caller, svc, line)
}

func (s *Server) completeCommand(caller Caller, svc Services, line string) []string {
	set := s.Commands()
	fields := strings.Fields(strings.TrimPrefix(line, "/"))
	trailingSpace := strings.HasSuffix(line, " ")

	// Still typing the command name.
	if len(fields) <= 1 && !trailingSpace {
		var partial string
		if len(fields) == 1 {
			partial = strings.ToLower(fields[0])
		}

		var matches []string
		for _, cmd := range set.All() {
			if !s.authorize(caller, cmd) {
				continue
			}
			if strings.HasPrefix(cmd.Name, partial) {
				matches = append(matches, "/"+cmd.Name)
			}
		}

		return matches
	}

	cmd, ok := set.Lookup(fields[0])
	// A caller who may not run it is not told it exists: completing /ban for
	// somebody without permission tells them the server has one.
	if !ok || !s.authorize(caller, cmd) {
		return nil
	}

	// argIndex counts from 1 for the first argument, the way the old switch
	// did, so the table pinned against it still reads the same.
	argIndex := len(fields) - 1
	partial := ""
	if !trailingSpace {
		partial = fields[len(fields)-1]
	} else {
		argIndex = len(fields)
	}

	return candidates(cmd, argIndex, partial, caller, svc)
}

// candidates is what may follow at one argument position, across every
// overload that has a parameter there.
func candidates(cmd *Command, argIndex int, partial string, caller Caller, svc Services) []string {
	var out []string
	seen := map[string]bool{}

	for _, o := range cmd.Signature.Overloads {
		p, ok := paramAt(o, argIndex)
		if !ok {
			continue
		}
		for _, candidate := range paramCandidates(p, argIndex, partial, caller, svc) {
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			out = append(out, candidate)
		}
	}

	return out
}

// paramAt is the parameter at a one-based argument position, if the overload
// has one. A greedy last parameter answers for every position from its own
// onwards, because it is still being typed however many words in the caller is.
func paramAt(o Overload, argIndex int) (Param, bool) {
	if argIndex < 1 {
		return Param{}, false
	}
	if argIndex <= len(o.Params) {
		return o.Params[argIndex-1], true
	}
	if o.greedy() {
		return o.Params[len(o.Params)-1], true
	}

	return Param{}, false
}

func paramCandidates(p Param, argIndex int, partial string, caller Caller, svc Services) []string {
	if p.NoSuggest {
		return nil
	}
	if len(p.Choices) > 0 {
		return filter(partial, p.Choices)
	}

	switch p.Type {
	case ParamCommand:
		// From the set itself, which is what lets /help complete command names
		// with no special case anywhere.
		var names []string
		for _, cmd := range svc.Commands().All() {
			names = append(names, cmd.Name)
		}

		return filter(partial, names)

	case ParamPlayer:
		return matchNames(partial, svc.OnlinePlayers())

	case ParamCoordinates:
		// Which axis this is comes from where it sits in the overload, which is
		// what lets one rule serve /tp's y and z without naming either.
		pos := caller.Position()
		axis := []float64{pos.X, pos.Y, pos.Z}[(argIndex-1)%3]

		return wholeBlock(partial, axis)

	case ParamDuration:
		return filter(partial, durationNames)

	case ParamMessage:
		// Free text. Player names are the only useful thing to offer, and
		// offering them is what /say and /me did.
		return matchNames(partial, svc.OnlinePlayers())

	case ParamWord, ParamInt, ParamFloat:
		return nil
	}

	return nil
}

// wholeBlock suggests the block the caller stands in, and suggests nothing when
// that does not extend what they have already typed.
func wholeBlock(partial string, value float64) []string {
	suggestion := strconv.Itoa(int(math.Floor(value)))
	if partial != "" && !strings.HasPrefix(suggestion, partial) {
		return nil
	}

	return []string{suggestion}
}

func matchNames(partial string, names []string) []string {
	partial = strings.ToLower(partial)

	var matches []string
	for _, name := range names {
		if partial == "" || strings.HasPrefix(strings.ToLower(name), partial) {
			matches = append(matches, name)
		}
	}

	return matches
}

func filter(partial string, options []string) []string {
	partial = strings.ToLower(partial)

	var matches []string
	for _, opt := range options {
		if strings.HasPrefix(opt, partial) {
			matches = append(matches, opt)
		}
	}

	return matches
}

func lastWord(line string) string {
	if strings.HasSuffix(line, " ") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}

	return fields[len(fields)-1]
}
