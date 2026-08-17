package conn

import (
	"testing"
	"time"

	"github.com/go-theft-craft/server/internal/server/packet"
)

// newDamageTestConn returns a survival-mode connection with full health. The
// default game mode is creative, which is immune, so a damage test that forgot
// this would pass whatever the geometry did.
func newDamageTestConn(t *testing.T) *Connection {
	t.Helper()

	c := newInventoryTestConn(t)
	c.self.SetGameMode(packet.GameModeSurvival)
	c.resetHealth()

	return c
}

// putCactus places one cactus and returns the position the player has to stand
// at to be pressed against its -X face.
func putCactus(c *Connection, x, y, z int) {
	c.world.SetBlock(x, y, z, int32(cactusBlockID)<<4)
}

func TestCactus_TouchingTheSideHurts(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 1, 4, 0)

	// The cactus box starts at x=1.0625, so a player whose box reaches 1.1
	// overlaps it.
	c.checkContactDamage(0.8, 4, 0.5)

	if got := c.health; got != maxHealth-cactusDamage {
		t.Errorf("health = %v, want %v", got, maxHealth-cactusDamage)
	}
}

// The case that matters in a real game, and the one the first implementation
// got wrong. A cactus's collision box is inset 0.0625, so the client walking
// into one stops with the player's box edge at exactly x = 1.0625 and no
// nearer. A check that asked for a strict overlap with that same inset box
// found none, and a player could stand against a cactus forever unhurt.
func TestCactus_HurtsAtTheDistanceTheClientStopsAt(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 1, 4, 0)

	// Box edge at 1.0625: pressed against the cactus's face, as far in as the
	// client will ever put the player.
	c.checkContactDamage(1.0625-playerHalfWidth, 4, 0.5)

	if got := c.health; got != maxHealth-cactusDamage {
		t.Errorf("health = %v, want %v — a player pressed against a cactus has to be hurt", got, maxHealth-cactusDamage)
	}
}

// A cactus is inset on X and Z, so a player standing in the middle of the
// neighbouring block is not touching it.
func TestCactus_StandingInTheNextBlockDoesNotHurt(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 1, 4, 0)

	c.checkContactDamage(0.5, 4, 0.5)

	if got := c.health; got != maxHealth {
		t.Errorf("health = %v, want %v — the player is not touching the cactus", got, maxHealth)
	}
}

// A cactus is shorter than a full block, which is why standing on one is safe.
func TestCactus_StandingOnTopDoesNotHurt(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 0, 3, 0)

	c.checkContactDamage(0.5, 4, 0.5)

	if got := c.health; got != maxHealth {
		t.Errorf("health = %v, want %v — standing on a cactus is not contact", got, maxHealth)
	}
}

// A client sends position twenty times a second. Without the immunity window,
// one cactus would take the whole bar in a second.
func TestCactus_ImmunityWindowLimitsTheRate(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 1, 4, 0)

	for range 10 {
		c.checkContactDamage(0.8, 4, 0.5)
	}

	if got := c.health; got != maxHealth-cactusDamage {
		t.Errorf("health = %v, want %v — ten checks inside the immunity window are one hit", got, maxHealth-cactusDamage)
	}
}

func TestCactus_HurtsAgainOnceImmunityExpires(t *testing.T) {
	c := newDamageTestConn(t)
	putCactus(c, 1, 4, 0)

	c.checkContactDamage(0.8, 4, 0.5)
	c.lastDamage = c.lastDamage.Add(-hurtImmunity)
	c.checkContactDamage(0.8, 4, 0.5)

	if got := c.health; got != maxHealth-2*cactusDamage {
		t.Errorf("health = %v, want %v", got, maxHealth-2*cactusDamage)
	}
}

func TestCactus_CreativeIsImmune(t *testing.T) {
	c := newDamageTestConn(t)
	c.self.SetGameMode(packet.GameModeCreative)
	putCactus(c, 1, 4, 0)

	c.checkContactDamage(0.8, 4, 0.5)

	if got := c.health; got != maxHealth {
		t.Errorf("health = %v, want %v — creative takes no damage", got, maxHealth)
	}
}

func TestDamage_EmptyingTheBarKills(t *testing.T) {
	c := newDamageTestConn(t)
	c.health = cactusDamage
	putCactus(c, 1, 4, 0)

	c.checkContactDamage(0.8, 4, 0.5)

	if c.health != 0 {
		t.Errorf("health = %v, want 0", c.health)
	}
	if !c.dead {
		t.Error("player is not dead with an empty health bar")
	}
}

// A dead player takes no further damage: the cactus is still there, and a
// death that repeated every heartbeat would spam the death message.
func TestDamage_TheDeadAreNotHurtAgain(t *testing.T) {
	c := newDamageTestConn(t)
	c.health = cactusDamage
	putCactus(c, 1, 4, 0)

	c.checkContactDamage(0.8, 4, 0.5)
	c.lastDamage = time.Time{}
	c.checkContactDamage(0.8, 4, 0.5)

	if got := c.health; got != 0 {
		t.Errorf("health = %v, want it to stay at 0", got)
	}
}

func TestDamage_RespawnRestoresTheBar(t *testing.T) {
	c := newDamageTestConn(t)
	c.health = 1
	c.dead = true

	if err := c.handleRespawn(); err != nil {
		t.Fatalf("handleRespawn: %v", err)
	}

	if got := c.health; got != maxHealth {
		t.Errorf("health = %v, want %v after respawn", got, maxHealth)
	}
	if c.dead {
		t.Error("player is still dead after respawning")
	}
}
