package conn

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/login"

	"github.com/go-theft-craft/server/internal/server/player"
)

// runLogin drives the whole login and hands the connection over to play.
//
// The login state is a phase rather than a packet dispatch: login.Acceptor
// owns inbound delivery from login start through login success, running the
// key exchange, the session-server check, and the compression handshake in the
// order the protocol fixes. The connection resumes reading afterwards.
func (c *Connection) runLogin() error {
	options := []login.AcceptorOption{
		login.WithCompressionThreshold(c.cfg.CompressionThreshold),
	}

	// The verifier is what makes a login online. Without one the acceptor
	// derives the same offline identity the server derived before.
	var verifier *mojangVerifier
	if c.cfg.OnlineMode {
		verifier = &mojangVerifier{}
		options = append(options, login.WithVerifier(verifier))
	}

	acceptor, err := login.NewAcceptor(c.cfg.PrivateKey, options...)
	if err != nil {
		return fmt.Errorf("build login acceptor: %w", err)
	}

	profile, err := acceptor.Accept(c.ctx, c.stream)
	if err != nil {
		return fmt.Errorf("accept login: %w", err)
	}

	username := profile.Name.String()
	identity := profile.UUID.String()

	c.log.Info("login success", "username", username, "uuid", identity, "online", c.cfg.OnlineMode)

	// The acceptor already moved the session to play by writing login success;
	// read that move back before startPlay sends its first play packet.
	if err := c.syncState(c.ctx); err != nil {
		return fmt.Errorf("sync state after login: %w", err)
	}

	return c.startPlay(username, identity, c.skinProperties(verifier, username))
}

// skinProperties resolves the textures to show for a player.
//
// Online mode already has them: they arrived with the session-server response
// the verifier read. Offline mode looks the name up by itself, and a failure
// there is not a login failure — a player with no skin still joins.
func (c *Connection) skinProperties(verifier *mojangVerifier, username string) []player.SkinProperty {
	if verifier != nil {
		return verifier.properties
	}

	properties, err := fetchSkin(c.ctx, username)
	if err != nil {
		c.log.Debug("skin lookup failed", "username", username, "error", err)

		return nil
	}

	return skinProperties(properties)
}
