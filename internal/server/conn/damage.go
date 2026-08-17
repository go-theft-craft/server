package conn

import (
	"fmt"
	"math"
	"time"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/packet"
)

// Health and contact damage.
//
// The server had no health state at all before this: /kill sent a zeroed
// UpdateHealth and respawn sent a full one, and nothing in between could hurt
// a player. Health now lives on the connection, because it is per-session
// state the Handle goroutine owns, exactly like the inventory and the death
// flag beside it.
const (
	maxHealth = 20.0
	maxFood   = 20

	// cactusName is the only contact-damage block so far.
	cactusDamageBlock = cactusName

	// cactusDamage is a half heart, which is what a cactus deals per hit.
	cactusDamage = 1.0

	// hurtImmunity is the window a damaged entity cannot be hurt again in.
	// Vanilla grants 10 ticks, and without it a client sending position twenty
	// times a second would lose twenty hearts a second to one cactus.
	hurtImmunity = 500 * time.Millisecond
)

// Player box, in blocks.
const (
	playerHalfWidth = 0.3
	playerHeight    = 1.8

	// contactEpsilon is how far the player's box is contracted before asking
	// which blocks it is inside, so merely resting against a face is not
	// contact with the block behind it.
	//
	// This is the whole trick, and testing the cactus's own collision box
	// instead gets it wrong: a cactus is inset 0.0625 on X and Z, so the
	// client stops the player exactly at that inset face and a strict overlap
	// with it never happens — the player would stand against a cactus forever
	// and never be hurt. What actually makes vanilla's cactus dangerous is
	// that the player's box, stopped at the inset face, is 0.0625 deep inside
	// the cactus's block cell. Vanilla asks which cells the contracted box
	// touches, and so does this.
	contactEpsilon = 0.001
)

// entityStatusHurt and entityStatusDead are the EntityStatus codes that play
// the hurt flash and the death animation on watching clients.
const (
	entityStatusHurt = 2
	entityStatusDead = 3
)

// checkContactDamage hurts the player if their bounding box overlaps a block
// that damages on contact. It is called from the movement path and from the
// idle heartbeat, because a player standing still in a cactus sends no
// position update and would otherwise never be hurt.
func (c *Connection) checkContactDamage(x, y, z float64) {
	if c.self == nil || c.dead || !c.vulnerable() {
		return
	}

	minX := int(math.Floor(x - playerHalfWidth + contactEpsilon))
	maxX := int(math.Floor(x + playerHalfWidth - contactEpsilon))
	minY := int(math.Floor(y + contactEpsilon))
	maxY := int(math.Floor(y + playerHeight - contactEpsilon))
	minZ := int(math.Floor(z - playerHalfWidth + contactEpsilon))
	maxZ := int(math.Floor(z + playerHalfWidth - contactEpsilon))

	for bx := minX; bx <= maxX; bx++ {
		for by := minY; by <= maxY; by++ {
			for bz := minZ; bz <= maxZ; bz++ {
				if c.blockName(c.blockAt(bx, by, bz)) == cactusDamageBlock {
					c.applyDamage(cactusDamage, "death.attack.cactus")

					return
				}
			}
		}
	}
}

// vulnerable reports whether the player's game mode allows damage.
func (c *Connection) vulnerable() bool {
	mode := c.self.GetGameMode()

	return mode != packet.GameModeCreative && mode != packet.GameModeSpectator
}

// applyDamage removes health, tells the player and everyone watching, and
// kills the player when nothing is left. deathKey is the vanilla translation
// key describing the cause, so the death message a client renders is the one
// its own language file already has.
func (c *Connection) applyDamage(amount float32, deathKey string) {
	if c.dead || !c.vulnerable() {
		return
	}

	if time.Since(c.lastDamage) < hurtImmunity {
		return
	}
	c.lastDamage = time.Now()

	c.health -= amount
	if c.health < 0 {
		c.health = 0
	}

	_ = c.send(&v1_8.PlayClientboundUpdateHealth{
		Health:         c.health,
		Food:           maxFood,
		FoodSaturation: 5,
	})

	status := &v1_8.PlayClientboundEntityStatus{
		EntityID:     c.self.EntityID,
		EntityStatus: entityStatusHurt,
	}
	// The hurt flash is sent to the player as well as to everyone tracking
	// them: BroadcastToTrackers excludes the player themselves, and a client
	// that never receives its own status shows no damage tint.
	_ = c.send(status)
	c.players.BroadcastToTrackers(status, c.self.EntityID)

	if c.health > 0 {
		return
	}

	c.die(deathKey)
}

// die marks the player dead, plays the death animation to everyone tracking
// them, and announces the cause.
func (c *Connection) die(deathKey string) {
	c.dead = true

	c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityStatus{
		EntityID:     c.self.EntityID,
		EntityStatus: entityStatusDead,
	}, c.self.EntityID)

	c.players.Broadcast(&v1_8.PlayClientboundChat{
		Message: fmt.Sprintf(
			`{"translate":%s,"with":[%s]}`,
			escapeJSON(deathKey), escapeJSON(c.self.Username),
		),
		Position: 0,
	})
}

// resetHealth restores a full bar. Join and respawn both use it, so the field
// and the packet can never disagree about what the player has.
func (c *Connection) resetHealth() {
	c.health = maxHealth
	c.lastDamage = time.Time{}
}
