package conn

import (
	"context"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/protocolinfo"
)

// newLegacyPingHook answers the `FE 01` probe a 1.6 or older client sends
// before any handshake.
//
// The server used to read those two bytes as a frame length and fail, which
// showed the client an unreachable server. The hook runs once, before framing
// begins, and declines every connection that does not start with the probe
// without consuming a byte.
//
// It reports the same description, player counts, and version the modern
// status response does, so both lists agree.
func newLegacyPingHook(cfg *config.Config, players *player.Manager) (protocol.PreFrameHook, error) {
	return java.NewLegacyPingHook(func(context.Context, java.LegacyPing) (java.LegacyStatus, error) {
		return java.LegacyStatus{
			ProtocolVersion: protocolinfo.ProtocolVersion,
			Version:         protocolinfo.VersionName,
			MOTD:            cfg.MOTD,
			OnlinePlayers:   players.PlayerCount(),
			MaxPlayers:      cfg.MaxPlayers,
		}, nil
	})
}
