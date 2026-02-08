package conn

import (
	"bytes"
	"strings"

	"github.com/go-theft-craft/server/internal/server/player"
	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// handleTabComplete processes a TabComplete (0x14) packet and sends completions back.
func (c *Connection) handleTabComplete(data []byte) error {
	r := bytes.NewReader(data)

	text, err := mcnet.ReadString(r)
	if err != nil {
		return err
	}

	hasPosition, err := mcnet.ReadBool(r)
	if err != nil {
		return err
	}
	if hasPosition {
		// Consume the looked-at block position (i64), we don't use it.
		if _, err := mcnet.ReadI64(r); err != nil {
			return err
		}
	}

	matches := computeCompletions(text, c.players)
	return c.sendTabCompleteResponse(matches)
}

// computeCompletions returns tab-completion matches for the given input text.
func computeCompletions(text string, players *player.Manager) []string {
	if strings.HasPrefix(text, "/") {
		return completeCommand(text, players)
	}
	// No "/" prefix: complete player names for chat mentions.
	parts := strings.Fields(text)
	var partial string
	if len(parts) > 0 && !strings.HasSuffix(text, " ") {
		partial = parts[len(parts)-1]
	}
	return matchPlayerNames(partial, players)
}

func completeCommand(text string, players *player.Manager) []string {
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
		if argIndex == 1 {
			return matchPlayerNames(argPartial, players)
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
	case "help", "list", "kill", "seed":
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

func (c *Connection) sendTabCompleteResponse(matches []string) error {
	var buf bytes.Buffer
	_, _ = mcnet.WriteVarInt(&buf, int32(len(matches)))
	for _, m := range matches {
		_, _ = mcnet.WriteString(&buf, m)
	}
	return c.writePacket(&pkt.TabCompleteCB{Data: buf.Bytes()})
}
