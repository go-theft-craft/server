package player

import (
	"sync"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// wireLimits bounds the values this package writes into packet payloads.
//
// The player package builds a few payloads by hand — entity metadata and the
// player-list entry — rather than through a packet struct, so it needs the
// same limits a connection uses. They are the defaults, and they are built
// once: the values are immutable and every connection shares them.
var wireLimits = sync.OnceValue(func() protocol.Limits {
	limits, err := protocol.NewLimits()
	if err != nil {
		// NewLimits with no options cannot fail; a failure here means the
		// defaults themselves are inconsistent, which is a build-time bug.
		panic("player: build default protocol limits: " + err.Error())
	}

	return limits
})
