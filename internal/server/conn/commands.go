package conn

import (
	"fmt"
	"strings"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/player"
)

// The command path, from the connection's side.
//
// There used to be a package-level slice of commands built in init(), a linear
// scan, and ten handlers that each took the whole connection and a slice of
// unparsed words. All of it moved into the server package, where a command is a
// value with a declared signature and can be run against a fake caller.
//
// What is left here is the two halves that genuinely belong to a connection:
// turning a chat line into a dispatch, and doing the things a command asks of
// the player who ran it. The Dispatcher and Completer are function seams
// because this package sits below the one that publishes server.Command, so it
// cannot name the types a command is written in.

// Dispatcher runs a command line for this connection and reports whether the
// line was a command.
type Dispatcher func(c *Connection, line string) bool

// Completer returns tab-complete matches for a line.
type Completer func(c *Connection, line string) []string

// SetCommands puts this connection on the server's command path. Set before
// Handle starts, like SetItemIndex, and read without a lock.
func (c *Connection) SetCommands(d Dispatcher, comp Completer) {
	c.dispatch, c.complete = d, comp
}

// handleCommand intercepts /-prefixed messages and dispatches them.
// It reports whether the message was a command, even an unknown one.
func (c *Connection) handleCommand(msg string) bool {
	if !strings.HasPrefix(msg, "/") {
		return false
	}
	if c.dispatch == nil {
		// A connection built without a server behind it. Saying so beats
		// letting the line through to chat, where it would be broadcast.
		c.sendErrorMsg("Commands are not available.")

		return true
	}

	return c.dispatch(c, msg)
}

// Username is the name of the player on this connection, or "" before login.
func (c *Connection) Username() string {
	if c.self == nil {
		return ""
	}

	return c.self.Username
}

// PlayerUUID is the UUID of the player on this connection, or "" before login.
func (c *Connection) PlayerUUID() string { return c.playerID() }

// PlayerPosition is where the player on this connection is standing.
func (c *Connection) PlayerPosition() (x, y, z float64, yaw, pitch float32) {
	if c.self == nil {
		return 0, 0, 0, 0, 0
	}
	pos := c.self.GetPosition()

	return pos.X, pos.Y, pos.Z, pos.Yaw, pos.Pitch
}

// SendMessage says something to this player and nobody else.
func (c *Connection) SendMessage(text, color string) { c.sendSystemMsg(text, color) }

// SendTranslated says something the client renders from its own language file,
// which is how /me is drawn.
func (c *Connection) SendTranslated(key string, with []string) {
	_ = c.send(&v1_8.PlayClientboundChat{Message: translatedJSON(key, with), Position: 1})
}

// BroadcastMessage says something to everyone.
func (c *Connection) BroadcastMessage(text, color string) {
	c.players.Broadcast(&v1_8.PlayClientboundChat{
		Message:  fmt.Sprintf(`{"text":%s,"color":%s}`, escapeJSON(text), escapeJSON(color)),
		Position: 0,
	})
}

// BroadcastTranslated is BroadcastMessage for a translated component.
func (c *Connection) BroadcastTranslated(key string, with []string) {
	c.players.Broadcast(&v1_8.PlayClientboundChat{Message: translatedJSON(key, with), Position: 0})
}

func translatedJSON(key string, with []string) string {
	parts := make([]string, len(with))
	for i, w := range with {
		parts[i] = escapeJSON(w)
	}

	return fmt.Sprintf(`{"translate":%s,"with":[%s]}`,
		escapeJSON(key), strings.Join(parts, ","))
}

// OnlineNames is every connected player's name, which /list reads.
func (c *Connection) OnlineNames() []string {
	var names []string
	c.players.ForEach(func(p *player.Player) { names = append(names, p.Username) })

	return names
}

// KillPlayer kills the player on this connection.
func (c *Connection) KillPlayer() {
	c.health = 0
	_ = c.send(&v1_8.PlayClientboundUpdateHealth{Health: 0, Food: 0, FoodSaturation: 0})
	c.die("death.attack.generic")
}

// SetGameModeByName resolves a mode name and applies it, reporting the
// canonical name and whether the name was one.
//
// Resolving lives here rather than in the command because what "sp" means is a
// property of this server's protocol layer, not of /gamemode: the short forms
// and the numbers are what a 1.8 player types and what vanilla accepts.
func (c *Connection) SetGameModeByName(name string) (string, bool) {
	var mode uint8
	var abilities int8

	switch strings.ToLower(name) {
	case "survival", "s", "0":
		mode, abilities, name = packet.GameModeSurvival, 0, "survival"
	case "creative", "c", "1":
		mode = packet.GameModeCreative
		abilities = packet.AbilityInvulnerable | packet.AbilityAllowFlight | packet.AbilityCreativeMode
		name = "creative"
	case "adventure", "a", "2":
		mode, abilities, name = packet.GameModeAdventure, 0, "adventure"
	case "spectator", "sp", "3":
		mode = packet.GameModeSpectator
		abilities = packet.AbilityInvulnerable | packet.AbilityAllowFlight
		name = "spectator"
	default:
		return "", false
	}

	_ = c.send(&v1_8.PlayClientboundGameStateChange{
		Reason:   3, // Change game mode
		GameMode: float32(mode),
	})
	c.self.SetGameMode(mode)
	_ = c.send(&v1_8.PlayClientboundAbilities{
		Flags:        abilities,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	})
	// Broadcast the change so the tab list updates for everyone.
	c.players.BroadcastGameMode(c.self)

	return name, true
}

// sendSystemMsg sends a chat message (position=1, system) to this connection only.
func (c *Connection) sendSystemMsg(text, color string) {
	_ = c.send(&v1_8.PlayClientboundChat{
		Message:  fmt.Sprintf(`{"text":%s,"color":%s}`, escapeJSON(text), escapeJSON(color)),
		Position: 1,
	})
}

// sendErrorMsg sends a red system message.
func (c *Connection) sendErrorMsg(text string) {
	c.sendSystemMsg(text, "red")
}

// TeleportSelf moves the connection's player to the given coordinates,
// broadcasting the teleport to trackers and updating tracking.
func (c *Connection) TeleportSelf(x, y, z float64) { c.teleportSelf(x, y, z) }

func (c *Connection) teleportSelf(x, y, z float64) {
	pos := c.self.GetPosition()
	c.setPositionAndUpdateChunks(x, y, z, pos.Yaw, pos.Pitch, false)

	_ = c.send(&v1_8.PlayClientboundPosition{
		X:     x,
		Y:     y,
		Z:     z,
		Yaw:   pos.Yaw,
		Pitch: pos.Pitch,
		Flags: packet.PositionAbsolute,
	})

	c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityTeleport{
		EntityID: c.self.EntityID,
		X:        player.FixedPoint(x),
		Y:        player.FixedPoint(y),
		Z:        player.FixedPoint(z),
		Yaw:      player.DegreesToAngle(pos.Yaw),
		Pitch:    player.DegreesToAngle(pos.Pitch),
		OnGround: false,
	}, c.self.EntityID)

	c.players.UpdateTracking(c.self)
}
