package conn

import (
	"encoding/json"
	"fmt"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
)

type statusResponse struct {
	Version     statusVersion `json:"version"`
	Players     statusPlayers `json:"players"`
	Description statusDesc    `json:"description"`
}

type statusVersion struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type statusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}

type statusDesc struct {
	Text string `json:"text"`
}

func (c *Connection) handleStatus(packetID int32, data []byte) error {
	switch packetID {
	case 0x00: // Status Request
		resp := statusResponse{
			Version: statusVersion{
				Name:     "1.8.9",
				Protocol: 47,
			},
			Players: statusPlayers{
				Max:    c.cfg.MaxPlayers,
				Online: 0,
			},
			Description: statusDesc{
				Text: c.cfg.MOTD,
			},
		}

		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("marshal status response: %w", err)
		}

		return c.writePacket(&packet.StatusResponse{
			JSONResponse: string(jsonBytes),
		})

	case 0x01: // Ping
		var ping packet.StatusPing
		if err := mcnet.Unmarshal(data, &ping); err != nil {
			return fmt.Errorf("unmarshal ping: %w", err)
		}

		return c.writePacket(&packet.StatusPong{
			Payload: ping.Payload,
		})

	default:
		return fmt.Errorf("unexpected status packet 0x%02X", packetID)
	}
}
