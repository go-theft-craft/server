package conn

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"fmt"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
	"github.com/go-theft-craft/server/internal/server/player"
)

func (c *Connection) handleLogin(packetID int32, data []byte) error {
	switch packetID {
	case 0x00: // LoginStart
		return c.handleLoginStart(data)
	case 0x01: // EncryptionResponse
		return c.handleEncryptionResponse(data)
	default:
		return fmt.Errorf("unexpected login packet 0x%02X", packetID)
	}
}

func (c *Connection) handleLoginStart(data []byte) error {
	var login pkt.LoginStart
	if err := mcnet.Unmarshal(data, &login); err != nil {
		return fmt.Errorf("unmarshal login start: %w", err)
	}

	c.log.Info("login start", "username", login.Username)

	if c.cfg.OnlineMode {
		return c.startOnlineLogin(login.Username)
	}

	return c.handleOfflineLogin(login.Username)
}

func (c *Connection) handleOfflineLogin(username string) error {
	uuid := offlineUUID(username)
	uuidStr := formatUUID(uuid)

	c.log.Info("offline login success", "username", username, "uuid", uuidStr)

	if err := c.writePacket(&pkt.Success{
		UUID:     uuidStr,
		Username: username,
	}); err != nil {
		return fmt.Errorf("write login success: %w", err)
	}

	var skinProps []player.SkinProperty
	if props, err := fetchSkinByUsername(c.ctx, username); err == nil && props != nil {
		skinProps = make([]player.SkinProperty, len(props))
		for i, p := range props {
			skinProps[i] = player.SkinProperty{Name: p.Name, Value: p.Value, Signature: p.Signature}
		}
	}

	c.state = StatePlay
	return c.startPlay(username, uuidStr, skinProps)
}

func (c *Connection) startOnlineLogin(username string) error {
	// Generate a random 4-byte verify token.
	verifyToken := make([]byte, 4)
	if _, err := rand.Read(verifyToken); err != nil {
		return fmt.Errorf("generate verify token: %w", err)
	}

	c.loginUsername = username
	c.loginVerifyToken = verifyToken

	// Send EncryptionRequest. ServerID is empty string per Minecraft 1.8.
	if err := c.writePacket(&pkt.EncryptionBeginCB{
		ServerID:    "",
		PublicKey:   c.cfg.PublicKeyDER,
		VerifyToken: verifyToken,
	}); err != nil {
		return fmt.Errorf("write encryption request: %w", err)
	}

	// Stay in StateLogin — next packet will be EncryptionResponse.
	return nil
}

func (c *Connection) handleEncryptionResponse(data []byte) error {
	var resp pkt.EncryptionBeginSB
	if err := mcnet.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("unmarshal encryption response: %w", err)
	}

	// Decrypt shared secret.
	sharedSecret, err := rsa.DecryptPKCS1v15(rand.Reader, c.cfg.PrivateKey, resp.SharedSecret)
	if err != nil {
		return fmt.Errorf("decrypt shared secret: %w", err)
	}

	// Decrypt and verify token.
	verifyToken, err := rsa.DecryptPKCS1v15(rand.Reader, c.cfg.PrivateKey, resp.VerifyToken)
	if err != nil {
		return fmt.Errorf("decrypt verify token: %w", err)
	}

	if len(verifyToken) != len(c.loginVerifyToken) {
		return fmt.Errorf("verify token length mismatch")
	}
	for i := range verifyToken {
		if verifyToken[i] != c.loginVerifyToken[i] {
			return fmt.Errorf("verify token mismatch")
		}
	}

	// Enable encryption. The EncryptionResponse was received unencrypted;
	// everything from here on (including LoginSuccess) is encrypted.
	if err := c.enableEncryption(sharedSecret); err != nil {
		return fmt.Errorf("enable encryption: %w", err)
	}

	// Verify with Mojang.
	serverHash := minecraftSHA1HexDigest("", sharedSecret, c.cfg.PublicKeyDER)
	profile, err := verifyWithMojang(c.ctx, c.loginUsername, serverHash)
	if err != nil {
		reason := `{"text":"Failed to verify with Mojang."}`
		_ = c.writePacket(&pkt.Disconnect{Reason: reason})
		c.disconnect("mojang auth failed")
		return fmt.Errorf("mojang verify: %w", err)
	}

	uuidStr := formatMojangUUID(profile.ID)

	c.log.Info("online login success", "username", profile.Name, "uuid", uuidStr)

	if err := c.writePacket(&pkt.Success{
		UUID:     uuidStr,
		Username: profile.Name,
	}); err != nil {
		return fmt.Errorf("write login success: %w", err)
	}

	skinProps := make([]player.SkinProperty, len(profile.Properties))
	for i, p := range profile.Properties {
		skinProps[i] = player.SkinProperty{
			Name:      p.Name,
			Value:     p.Value,
			Signature: p.Signature,
		}
	}

	c.state = StatePlay
	return c.startPlay(profile.Name, uuidStr, skinProps)
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
