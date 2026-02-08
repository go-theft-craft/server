package conn

import (
	"encoding/json"
	"fmt"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
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
				Name:     pkt.VersionName,
				Protocol: int(pkt.ProtocolVersion),
			},
			Players: statusPlayers{
				Max:    c.cfg.MaxPlayers,
				Online: c.players.PlayerCount(),
			},
			Description: statusDesc{
				Text: c.cfg.MOTD,
			},
		}

		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("marshal status response: %w", err)
		}

		return c.writePacket(&pkt.ServerInfo{
			Response: string(jsonBytes),
		})

	case 0x01: // Ping
		var ping pkt.PingSB
		if err := mcnet.Unmarshal(data, &ping); err != nil {
			return fmt.Errorf("unmarshal ping: %w", err)
		}

		return c.writePacket(&pkt.PingCB{
			Time: ping.Time,
		})

	default:
		return fmt.Errorf("unexpected status packet 0x%02X", packetID)
	}
}
