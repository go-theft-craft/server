package conn

import (
	"math"
	"strconv"
	"strings"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
)

// handleTabComplete processes a TabComplete (0x14) packet and sends completions
// back. The looked-at block (value.Block) is decoded by the session but unused.
func (c *Connection) handleTabComplete(value *v1_8.PlayServerboundTabComplete) error {
	matches := computeCompletions(value.Text, c.players, c.self.GetPosition())

	return c.sendTabCompleteResponse(matches)
}

// computeCompletions returns tab-completion matches for the given input text.
//
// The 1.8 client decides on its own what to do with them: one match is
// inserted, and several are drawn as the suggestion list above the chat box.
// That list is the only suggestion UI the protocol has — there is no way to
// push suggestions as the player types — so the way to make suggestions appear
// more often is to leave fewer argument positions with nothing to offer.
//
// pos is the player's own position, which is what coordinate arguments
// complete to.
func computeCompletions(text string, players *player.Manager, pos player.Position) []string {
	if strings.HasPrefix(text, "/") {
		return completeCommand(text, players, pos)
	}
	// No "/" prefix: complete player names for chat mentions.
	parts := strings.Fields(text)
	var partial string
	if len(parts) > 0 && !strings.HasSuffix(text, " ") {
		partial = parts[len(parts)-1]
	}
	return matchPlayerNames(partial, players)
}

func completeCommand(text string, players *player.Manager, pos player.Position) []string {
	parts := strings.Fields(text)
	// If text ends with space, we're completing the next argument.
	trailingSpace := strings.HasSuffix(text, " ")

	if len(parts) == 1 && !trailingSpace {
		// Completing the command name itself: "/par" → "/tp", etc.
		partial := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
		var matches []string
		for _, cmd := range commands {
			if strings.HasPrefix(cmd.name, partial) {
				matches = append(matches, "/"+cmd.name)
			}
		}
		return matches
	}

	// Completing arguments for a known command.
	if len(parts) == 0 {
		return nil
	}
	cmdName := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	var argPartial string
	if !trailingSpace && len(parts) > 1 {
		argPartial = parts[len(parts)-1]
	}
	argIndex := len(parts) - 1
	if trailingSpace {
		argIndex = len(parts)
	}

	switch cmdName {
	case "tp":
		// /tp takes either a player or three coordinates. The first argument
		// completes to a player name, as vanilla's does, and the rest to the
		// block the player is standing in — vanilla completes coordinates to
		// the block being looked at, which this server cannot see without a
		// ray cast, and the player's own position is the more useful of the
		// two for typing a destination near where they are.
		if argIndex == 1 {
			return matchPlayerNames(argPartial, players)
		}
		// In the coordinate form the first argument is x, so the second and
		// third — the only ones left to complete — are y and z.
		if argIndex == 2 {
			return coordinate(argPartial, pos.Y)
		}
		if argIndex == 3 {
			return coordinate(argPartial, pos.Z)
		}
	case "help":
		// /help takes a command name, so it completes the same list the bare
		// slash does, without the slash.
		if argIndex == 1 {
			names := make([]string, 0, len(commands))
			for _, cmd := range commands {
				names = append(names, cmd.name)
			}

			return filterStrings(argPartial, names)
		}
	case "gamemode":
		if argIndex == 1 {
			return filterStrings(argPartial, []string{"survival", "creative", "adventure", "spectator"})
		}
	case "time":
		if argIndex == 1 {
			return filterStrings(argPartial, []string{"set"})
		}
		if argIndex == 2 {
			return filterStrings(argPartial, []string{"day", "night", "noon", "midnight"})
		}
	case "list", "kill", "seed", "save":
		// No arguments to complete.
	case "say", "me":
		// Free-form text, complete player names.
		return matchPlayerNames(argPartial, players)
	}

	return nil
}

func matchPlayerNames(partial string, players *player.Manager) []string {
	partial = strings.ToLower(partial)
	var matches []string
	players.ForEach(func(p *player.Player) {
		if partial == "" || strings.HasPrefix(strings.ToLower(p.Username), partial) {
			matches = append(matches, p.Username)
		}
	})
	return matches
}

// coordinate suggests the whole block the player stands in, and suggests
// nothing when that does not extend what they have already typed.
func coordinate(partial string, value float64) []string {
	suggestion := strconv.Itoa(int(math.Floor(value)))
	if partial != "" && !strings.HasPrefix(suggestion, partial) {
		return nil
	}

	return []string{suggestion}
}

func filterStrings(partial string, options []string) []string {
	partial = strings.ToLower(partial)
	var matches []string
	for _, opt := range options {
		if strings.HasPrefix(opt, partial) {
			matches = append(matches, opt)
		}
	}
	return matches
}

// sendTabCompleteResponse sends the matches, dropping any empty one. A
// coordinate that does not extend what the player typed comes back empty, and
// an empty candidate would take a line in the client's suggestion list.
func (c *Connection) sendTabCompleteResponse(matches []string) error {
	kept := make([]string, 0, len(matches))
	for _, m := range matches {
		if m != "" {
			kept = append(kept, m)
		}
	}

	return c.send(&v1_8.PlayClientboundTabComplete{Matches: kept})
}
