package player

import (
	"testing"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
)

func TestEntityTrackingOnJoin(t *testing.T) {
	m := NewManager(8)
	// Both players at chunk 0,0 → in range.
	p1, pc1 := newTestPlayer(m, 8, 8)
	p2, pc2 := newTestPlayer(m, 8, 8)

	m.Add(p1)
	m.Add(p2)

	// p1 should track p2 and vice versa.
	if !p1.IsTracking(p2.EntityID) {
		t.Error("p1 should be tracking p2")
	}
	if !p2.IsTracking(p1.EntityID) {
		t.Error("p2 should be tracking p1")
	}

	// Both should have received SpawnNamedEntity.
	if pc1.countByType(pkt.NamedEntitySpawn{}.PacketID()) < 1 {
		t.Error("p1 should have received NamedEntitySpawn for p2")
	}
	if pc2.countByType(pkt.NamedEntitySpawn{}.PacketID()) < 1 {
		t.Error("p2 should have received NamedEntitySpawn for p1")
	}
}

func TestEntityTrackingOutOfRange(t *testing.T) {
	m := NewManager(2) // 2-chunk view distance
	// p1 at chunk 0,0 and p2 at chunk 10,10 → out of range.
	p1, _ := newTestPlayer(m, 8, 8)
	p2, _ := newTestPlayer(m, 168, 168)

	m.Add(p1)
	m.Add(p2)

	if p1.IsTracking(p2.EntityID) {
		t.Error("p1 should NOT be tracking p2 (out of range)")
	}
	if p2.IsTracking(p1.EntityID) {
		t.Error("p2 should NOT be tracking p1 (out of range)")
	}
}

func TestEntityTrackingEnterRange(t *testing.T) {
	m := NewManager(2)
	p1, pc1 := newTestPlayer(m, 8, 8)
	p2, _ := newTestPlayer(m, 168, 168) // far away

	m.Add(p1)
	m.Add(p2)

	if p1.IsTracking(p2.EntityID) {
		t.Fatal("precondition: p1 should not track p2 yet")
	}

	pc1.reset()

	// Move p2 close to p1.
	p2.SetPosition(8, 4, 8, 0, 0, true)
	m.UpdateTracking(p2)

	if !p1.IsTracking(p2.EntityID) {
		t.Error("p1 should now be tracking p2 after p2 moved into range")
	}

	if pc1.countByType(pkt.NamedEntitySpawn{}.PacketID()) < 1 {
		t.Error("p1 should have received NamedEntitySpawn after p2 entered range")
	}
}

func TestEntityTrackingLeaveRange(t *testing.T) {
	m := NewManager(2)
	p1, pc1 := newTestPlayer(m, 8, 8)
	p2, _ := newTestPlayer(m, 8, 8) // same location

	m.Add(p1)
	m.Add(p2)

	if !p1.IsTracking(p2.EntityID) {
		t.Fatal("precondition: p1 should track p2")
	}

	pc1.reset()

	// Move p2 far away.
	p2.SetPosition(500, 4, 500, 0, 0, true)
	m.UpdateTracking(p2)

	if p1.IsTracking(p2.EntityID) {
		t.Error("p1 should no longer track p2 after p2 moved out of range")
	}

	if pc1.countByType(pkt.EntityDestroy{}.PacketID()) < 1 {
		t.Error("p1 should have received EntityDestroy after p2 left range")
	}
}
