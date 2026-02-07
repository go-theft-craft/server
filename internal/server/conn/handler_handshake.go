package conn

import (
	"fmt"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
)

func (c *Connection) handleHandshake(packetID int32, data []byte) error {
	if packetID != 0x00 {
		return fmt.Errorf("expected handshake packet 0x00, got 0x%02X", packetID)
	}

	var hs packet.Handshake
	if err := mcnet.Unmarshal(data, &hs); err != nil {
		return fmt.Errorf("unmarshal handshake: %w", err)
	}

	c.log.Info("handshake received",
		"protocol", hs.ProtocolVersion,
		"server", hs.ServerAddress,
		"port", hs.ServerPort,
		"nextState", hs.NextState,
	)

	switch hs.NextState {
	case 1:
		c.state = StateStatus
	case 2:
		if hs.ProtocolVersion != 47 {
			c.log.Warn("unsupported protocol version", "version", hs.ProtocolVersion)
		}
		c.state = StateLogin
	default:
		return fmt.Errorf("invalid next state: %d", hs.NextState)
	}

	return nil
}
