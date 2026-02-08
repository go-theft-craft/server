package conn

import (
	"fmt"
	"strconv"
	"strings"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
)

type command struct {
	name    string
	usage   string
	desc    string
	handler func(c *Connection, args []string)
}

var commands []command

func init() {
	commands = []command{
		{name: "help", usage: "/help", desc: "Show available commands", handler: cmdHelp},
		{name: "list", usage: "/list", desc: "Show online players", handler: cmdList},
		{name: "tp", usage: "/tp <player> | /tp <x> <y> <z>", desc: "Teleport to a player or coordinates", handler: cmdTp},
		{name: "gamemode", usage: "/gamemode <survival|creative|adventure|spectator>", desc: "Change game mode", handler: cmdGamemode},
		{name: "time", usage: "/time set <day|night|noon|midnight|number>", desc: "Set world time", handler: cmdTime},
		{name: "say", usage: "/say <message>", desc: "Broadcast an announcement", handler: cmdSay},
		{name: "me", usage: "/me <action>", desc: "Send an action message", handler: cmdMe},
		{name: "kill", usage: "/kill", desc: "Kill yourself", handler: cmdKill},
		{name: "seed", usage: "/seed", desc: "Show world seed", handler: cmdSeed},
		{name: "save", usage: "/save", desc: "Save world and player data", handler: cmdSave},
	}
}

// handleCommand intercepts /-prefixed messages and dispatches them.
// Returns true if the message was a command (even if unknown).
func (c *Connection) handleCommand(msg string) bool {
	if !strings.HasPrefix(msg, "/") {
		return false
	}

	parts := strings.Fields(msg)
	if len(parts) == 0 {
		return true
	}

	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	args := parts[1:]

	for _, cmd := range commands {
		if cmd.name == name {
			cmd.handler(c, args)
			return true
		}
	}

	c.sendErrorMsg(fmt.Sprintf("Unknown command: /%s. Type /help for a list of commands.", name))
	return true
}

// sendSystemMsg sends a chat message (position=1, system) to this connection only.
func (c *Connection) sendSystemMsg(text, color string) {
	_ = c.writePacket(&pkt.ChatCB{
		Message:  fmt.Sprintf(`{"text":%s,"color":%s}`, escapeJSON(text), escapeJSON(color)),
		Position: 1,
	})
}

// sendErrorMsg sends a red system message.
func (c *Connection) sendErrorMsg(text string) {
	c.sendSystemMsg(text, "red")
}

// sendSuccessMsg sends a gold system message.
func (c *Connection) sendSuccessMsg(text string) {
	c.sendSystemMsg(text, "gold")
}

// teleportSelf moves the connection's player to the given coordinates,
// broadcasting the teleport to trackers and updating tracking.
func (c *Connection) teleportSelf(x, y, z float64) {
	pos := c.self.GetPosition()
	c.setPositionAndUpdateChunks(x, y, z, pos.Yaw, pos.Pitch, false)

	_ = c.writePacket(&pkt.PositionCB{
		X:     x,
		Y:     y,
		Z:     z,
		Yaw:   pos.Yaw,
		Pitch: pos.Pitch,
		Flags: 0x00,
	})

	c.players.BroadcastToTrackers(&pkt.EntityTeleport{
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

func cmdHelp(c *Connection, _ []string) {
	c.sendSystemMsg("--- Available Commands ---", "yellow")
	for _, cmd := range commands {
		c.sendSystemMsg(fmt.Sprintf("%s - %s", cmd.usage, cmd.desc), "yellow")
	}
}

func cmdList(c *Connection, _ []string) {
	var names []string
	c.players.ForEach(func(p *player.Player) {
		names = append(names, p.Username)
	})
	c.sendSuccessMsg(fmt.Sprintf("Online players (%d): %s", len(names), strings.Join(names, ", ")))
}

func cmdTp(c *Connection, args []string) {
	switch len(args) {
	case 1:
		target := c.players.GetByName(args[0])
		if target == nil {
			c.sendErrorMsg(fmt.Sprintf("Player %q not found.", args[0]))
			return
		}
		pos := target.GetPosition()
		c.teleportSelf(pos.X, pos.Y, pos.Z)
		c.sendSuccessMsg(fmt.Sprintf("Teleported to %s.", target.Username))

	case 3:
		x, errX := strconv.ParseFloat(args[0], 64)
		y, errY := strconv.ParseFloat(args[1], 64)
		z, errZ := strconv.ParseFloat(args[2], 64)
		if errX != nil || errY != nil || errZ != nil {
			c.sendErrorMsg("Usage: /tp <x> <y> <z> (numbers)")
			return
		}
		c.teleportSelf(x, y, z)
		c.sendSuccessMsg(fmt.Sprintf("Teleported to %.1f, %.1f, %.1f.", x, y, z))

	default:
		c.sendErrorMsg("Usage: /tp <player> or /tp <x> <y> <z>")
	}
}

func cmdGamemode(c *Connection, args []string) {
	if len(args) != 1 {
		c.sendErrorMsg("Usage: /gamemode <survival|creative|adventure|spectator>")
		return
	}

	var mode uint8
	var abilities int8
	modeName := strings.ToLower(args[0])

	switch modeName {
	case "survival", "s", "0":
		mode = packet.GameModeSurvival
		abilities = 0
		modeName = "survival"
	case "creative", "c", "1":
		mode = packet.GameModeCreative
		abilities = packet.AbilityInvulnerable | packet.AbilityAllowFlight | packet.AbilityCreativeMode
		modeName = "creative"
	case "adventure", "a", "2":
		mode = packet.GameModeAdventure
		abilities = 0
		modeName = "adventure"
	case "spectator", "sp", "3":
		mode = packet.GameModeSpectator
		abilities = packet.AbilityInvulnerable | packet.AbilityAllowFlight
		modeName = "spectator"
	default:
		c.sendErrorMsg("Unknown game mode. Use: survival, creative, adventure, spectator")
		return
	}

	_ = c.writePacket(&pkt.GameStateChange{
		Reason:   3, // Change game mode
		GameMode: float32(mode),
	})

	c.self.SetGameMode(mode)

	_ = c.writePacket(&pkt.AbilitiesCB{
		Flags:        abilities,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	})

	c.sendSuccessMsg(fmt.Sprintf("Game mode set to %s.", modeName))
}

func cmdTime(c *Connection, args []string) {
	if len(args) != 2 || strings.ToLower(args[0]) != "set" {
		c.sendErrorMsg("Usage: /time set <day|night|noon|midnight|number>")
		return
	}

	var ticks int64
	switch strings.ToLower(args[1]) {
	case "day":
		ticks = 1000
	case "noon":
		ticks = 6000
	case "night":
		ticks = 13000
	case "midnight":
		ticks = 18000
	default:
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			c.sendErrorMsg("Usage: /time set <day|night|noon|midnight|number>")
			return
		}
		ticks = v
	}

	c.world.SetTimeOfDay(ticks)
	age, _ := c.world.GetTime()
	c.players.Broadcast(&pkt.UpdateTime{
		Age:  age,
		Time: ticks,
	})
	c.sendSuccessMsg(fmt.Sprintf("Time set to %d.", ticks))
}

func cmdSay(c *Connection, args []string) {
	if len(args) == 0 {
		c.sendErrorMsg("Usage: /say <message>")
		return
	}
	msg := strings.Join(args, " ")
	chatJSON := fmt.Sprintf(
		`{"text":"[Server] %s","color":"light_purple"}`,
		strings.ReplaceAll(strings.ReplaceAll(msg, `\`, `\\`), `"`, `\"`),
	)
	c.players.Broadcast(&pkt.ChatCB{
		Message:  chatJSON,
		Position: 0,
	})
}

func cmdMe(c *Connection, args []string) {
	if len(args) == 0 {
		c.sendErrorMsg("Usage: /me <action>")
		return
	}
	action := strings.Join(args, " ")
	chatJSON := fmt.Sprintf(
		`{"translate":"chat.type.emote","with":[%s,%s]}`,
		escapeJSON(c.self.Username), escapeJSON(action),
	)
	c.players.Broadcast(&pkt.ChatCB{
		Message:  chatJSON,
		Position: 0,
	})
}

func cmdKill(c *Connection, _ []string) {
	c.dead = true
	_ = c.writePacket(&pkt.UpdateHealth{
		Health:         0,
		Food:           0,
		FoodSaturation: 0,
	})
	c.players.BroadcastToTrackers(&pkt.EntityStatus{
		EntityID:     c.self.EntityID,
		EntityStatus: 3, // death animation
	}, c.self.EntityID)
	c.sendSuccessMsg("You killed yourself.")
}

func cmdSeed(c *Connection, _ []string) {
	c.sendSuccessMsg(fmt.Sprintf("Seed: [%d]", c.cfg.Seed))
}

func cmdSave(c *Connection, _ []string) {
	if c.SaveAll == nil {
		c.sendErrorMsg("Save is not available.")
		return
	}
	c.sendSuccessMsg("Saving world and player data...")
	go func() {
		c.SaveAll()
		c.sendSuccessMsg("Save complete.")
	}()
}
