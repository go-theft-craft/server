package conn

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

// Tab-complete, from the connection's side.
//
// What used to be here was a switch statement with a case per command and per
// argument index — the only record anywhere of what this server suggests. It is
// gone: suggestions now come from the same Signature the parser reads, so
// execution and completion cannot disagree about what a command takes. The
// whole table it produced was pinned as a test first (completion_table_test.go)
// and the same table runs against the new path in the server package, because a
// rewrite that quietly dropped a suggestion would look exactly like one that
// kept them all.

// handleTabComplete processes a TabComplete (0x14) packet and sends completions
// back. The looked-at block (value.Block) is decoded by the session but unused:
// coordinates complete to the block the player stands in, not the one they are
// looking at, because there is no ray cast here and the standing position is
// more useful for typing a destination.
func (c *Connection) handleTabComplete(value *v1_8.PlayServerboundTabComplete) error {
	var matches []string
	if c.complete != nil {
		matches = c.complete(c, value.Text)
	}

	return c.sendTabCompleteResponse(matches)
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
