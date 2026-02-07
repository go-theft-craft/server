package conn

import (
	"crypto/md5"
	"fmt"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
)

func (c *Connection) handleLogin(packetID int32, data []byte) error {
	if packetID != 0x00 {
		return fmt.Errorf("expected LoginStart (0x00), got 0x%02X", packetID)
	}

	var login packet.LoginStart
	if err := mcnet.Unmarshal(data, &login); err != nil {
		return fmt.Errorf("unmarshal login start: %w", err)
	}

	c.log.Info("login start", "username", login.Name)

	if c.cfg.OnlineMode {
		return c.handleOnlineLogin(login.Name)
	}

	return c.handleOfflineLogin(login.Name)
}

func (c *Connection) handleOfflineLogin(username string) error {
	uuid := offlineUUID(username)
	uuidStr := formatUUID(uuid)

	c.log.Info("offline login success", "username", username, "uuid", uuidStr)

	if err := c.writePacket(&packet.LoginSuccess{
		UUID:     uuidStr,
		Username: username,
	}); err != nil {
		return fmt.Errorf("write login success: %w", err)
	}

	c.state = StatePlay
	return c.startPlay(username, uuidStr)
}

func (c *Connection) handleOnlineLogin(username string) error {
	// Online mode requires crypto — send disconnect for now if crypto is not yet initialized.
	reason := `{"text":"Online mode is not yet supported."}`
	if err := c.writePacket(&packet.LoginDisconnect{Reason: reason}); err != nil {
		return fmt.Errorf("write disconnect: %w", err)
	}
	c.disconnect("online mode not yet implemented")
	return nil
}

// offlineUUID generates UUID v3 from "OfflinePlayer:<username>" using the MD5 namespace.
func offlineUUID(username string) [16]byte {
	h := md5.Sum([]byte("OfflinePlayer:" + username))
	// Set version to 3 (MD5)
	h[6] = (h[6] & 0x0f) | 0x30
	// Set variant to RFC 4122
	h[8] = (h[8] & 0x3f) | 0x80
	return h
}

// formatUUID formats a 16-byte UUID as a hyphenated string.
func formatUUID(uuid [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
