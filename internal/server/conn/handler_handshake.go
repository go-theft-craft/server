package conn

import (
	"fmt"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
)

func (c *Connection) handleHandshake(packetID int32, data []byte) error {
	if packetID != 0x00 {
		return fmt.Errorf("expected handshake packet 0x00, got 0x%02X", packetID)
	}

	var hs pkt.SetProtocol
	if err := mcnet.Unmarshal(data, &hs); err != nil {
		return fmt.Errorf("unmarshal handshake: %w", err)
	}

	c.log.Info("handshake received",
		"protocol", hs.ProtocolVersion,
		"server", hs.ServerHost,
		"port", hs.ServerPort,
		"nextState", hs.NextState,
	)

	switch hs.NextState {
	case 1:
		c.state = StateStatus
	case 2:
		if hs.ProtocolVersion != pkt.ProtocolVersion {
			c.log.Warn("unsupported protocol version", "version", hs.ProtocolVersion)
		}
		c.state = StateLogin
	default:
		return fmt.Errorf("invalid next state: %d", hs.NextState)
	}

	return nil
}
