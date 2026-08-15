package conn

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
)

// The two next-state values a handshake may carry.
const (
	handshakeNextStatus int32 = 1
	handshakeNextLogin  int32 = 2
)

// handleHandshake reads the one packet the handshake state allows.
//
// The session has already moved itself by the time this runs: the handshake
// packet proposes its own transition and the stream commits it before
// delivering the packet. This mirrors the result into the connection's own
// state, which M6 deletes.
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

	switch handshake.NextState {
	case handshakeNextStatus:
		c.state = StateStatus
	case handshakeNextLogin:
		if handshake.ProtocolVersion != pkt.ProtocolVersion {
			c.log.Warn("unsupported protocol version", "version", handshake.ProtocolVersion)
		}

		c.state = StateLogin
	default:
		return fmt.Errorf("invalid next state: %d", handshake.NextState)
	}

	return nil
}
