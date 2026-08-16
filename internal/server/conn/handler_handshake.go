package conn

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/protocolinfo"
)

// handshakeNextLogin is the next-state value that routes a handshake into
// login. The session validates the field itself; the connection only reads it
// to log an unsupported protocol version before login begins.
const handshakeNextLogin int32 = 2

// handleHandshake reads the one packet the handshake state allows.
//
// The session has already moved itself by the time this runs: the handshake
// packet proposes its own transition and the stream commits it before
// delivering the packet. The connection reads that move back from the session
// (syncState) rather than re-deriving it from the next-state field.
//
// An unsupported next state does not reach here. The session refuses to
// propose a transition it cannot make, and the stream fails on that instead of
// delivering a packet the connection would have to reject itself.
func (c *Connection) handleHandshake(packet protocol.Packet) error {
	handshake, ok := packet.Value.(*v1_8.HandshakingServerboundSetProtocol)
	if !ok {
		return fmt.Errorf("expected a handshake, received packet 0x%02X (%T)", packet.ID, packet.Value)
	}

	c.log.Info(
		"handshake received",
		"protocol", handshake.ProtocolVersion,
		"server", handshake.ServerHost,
		"port", handshake.ServerPort,
		"nextState", handshake.NextState,
	)

	if handshake.NextState == handshakeNextLogin &&
		handshake.ProtocolVersion != protocolinfo.ProtocolVersion {
		c.log.Warn("unsupported protocol version", "version", handshake.ProtocolVersion)
	}

	return c.syncState(c.ctx)
}
