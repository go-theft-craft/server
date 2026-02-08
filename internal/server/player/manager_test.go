package player

import (
	"sync"
	"testing"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// packetCollector records packets sent to a player.
type packetCollector struct {
	mu      sync.Mutex
	packets []mcnet.Packet
}

func (pc *packetCollector) writePacket(p mcnet.Packet) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.packets = append(pc.packets, p)
	return nil
}

func (pc *packetCollector) get() []mcnet.Packet {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	cp := make([]mcnet.Packet, len(pc.packets))
	copy(cp, pc.packets)
	return cp
}

func (pc *packetCollector) reset() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.packets = nil
}

func (pc *packetCollector) countByType(id int32) int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	n := 0
	for _, p := range pc.packets {
		if p.PacketID() == id {
			n++
		}
	}
	return n
}

func newTestPlayer(m *Manager, x, z float64) (*Player, *packetCollector) {
	pc := &packetCollector{}
	eid := m.AllocateEntityID()
	uuid := [16]byte{byte(eid)}
	p := NewPlayer(eid, "test-uuid", uuid, "player", nil, pc.writePacket)
	// Set position to desired chunk.
	p.SetPosition(x, 4, z, 0, 0, true)
	return p, pc
}

func TestAllocateEntityID(t *testing.T) {
	m := NewManager(8)
	id1 := m.AllocateEntityID()
	id2 := m.AllocateEntityID()
	id3 := m.AllocateEntityID()
	if id1 != 1 || id2 != 2 || id3 != 3 {
		t.Errorf("expected 1,2,3 got %d,%d,%d", id1, id2, id3)
	}
}

func TestAddRemovePlayer(t *testing.T) {
	m := NewManager(8)
	p1, _ := newTestPlayer(m, 0, 0)
	p2, _ := newTestPlayer(m, 0, 0)

	m.Add(p1)
	if m.PlayerCount() != 1 {
		t.Errorf("expected 1, got %d", m.PlayerCount())
	}

	m.Add(p2)
	if m.PlayerCount() != 2 {
		t.Errorf("expected 2, got %d", m.PlayerCount())
	}

	m.Remove(p1)
	if m.PlayerCount() != 1 {
		t.Errorf("expected 1, got %d", m.PlayerCount())
	}

	m.Remove(p2)
	if m.PlayerCount() != 0 {
		t.Errorf("expected 0, got %d", m.PlayerCount())
	}
}

func TestBroadcast(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, pc2 := newTestPlayer(m, 0, 0)

	m.Add(p1)
	m.Add(p2)

	pc1.reset()
	pc2.reset()

	m.Broadcast(&pkt.ChatCB{Message: `{"text":"hello"}`, Position: 0})

	if len(pc1.get()) != 1 {
		t.Errorf("p1 expected 1 packet, got %d", len(pc1.get()))
	}
	if len(pc2.get()) != 1 {
		t.Errorf("p2 expected 1 packet, got %d", len(pc2.get()))
	}
}

func TestBroadcastExcept(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, pc2 := newTestPlayer(m, 0, 0)

	m.Add(p1)
	m.Add(p2)

	pc1.reset()
	pc2.reset()

	m.BroadcastExcept(&pkt.ChatCB{Message: `{"text":"hello"}`, Position: 0}, p1.EntityID)

	if len(pc1.get()) != 0 {
		t.Errorf("p1 (excluded) expected 0 packets, got %d", len(pc1.get()))
	}
	if len(pc2.get()) != 1 {
		t.Errorf("p2 expected 1 packet, got %d", len(pc2.get()))
	}
}

func TestPlayerInfoOnAdd(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, pc2 := newTestPlayer(m, 0, 0)

	m.Add(p1)
	pc1.reset()

	m.Add(p2)

	// p1 should receive p2's PlayerInfo
	p1InfoCount := pc1.countByType(pkt.PlayerInfo{}.PacketID())
	if p1InfoCount < 1 {
		t.Errorf("p1 expected at least 1 PlayerInfo, got %d", p1InfoCount)
	}

	// p2 should receive p1's PlayerInfo
	p2InfoCount := pc2.countByType(pkt.PlayerInfo{}.PacketID())
	if p2InfoCount < 1 {
		t.Errorf("p2 expected at least 1 PlayerInfo, got %d", p2InfoCount)
	}
}

func TestSpawnSendsEquipmentPackets(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, _ := newTestPlayer(m, 0, 0)

	m.Add(p1)
	pc1.reset()

	m.Add(p2)

	// p1 should receive 5 EntityEquipment packets for p2 (held item + 4 armor slots).
	eqCount := pc1.countByType(pkt.EntityEquipment{}.PacketID())
	if eqCount != 5 {
		t.Errorf("expected 5 EntityEquipment packets, got %d", eqCount)
	}
}

func TestSpawnSendsEntityMetadata(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, _ := newTestPlayer(m, 0, 0)

	m.Add(p1)
	pc1.reset()

	m.Add(p2)

	// p1 should receive at least 1 EntityMetadata packet for p2.
	metaCount := pc1.countByType(pkt.EntityMetadata{}.PacketID())
	if metaCount < 1 {
		t.Errorf("expected at least 1 EntityMetadata packet, got %d", metaCount)
	}
}

func TestPlayerInfoOnRemove(t *testing.T) {
	m := NewManager(8)
	p1, pc1 := newTestPlayer(m, 0, 0)
	p2, _ := newTestPlayer(m, 0, 0)

	m.Add(p1)
	m.Add(p2)
	pc1.reset()

	m.Remove(p2)

	// p1 should receive PlayerInfo (remove) for p2.
	p1InfoCount := pc1.countByType(pkt.PlayerInfo{}.PacketID())
	if p1InfoCount < 1 {
		t.Errorf("p1 expected at least 1 PlayerInfo(remove), got %d", p1InfoCount)
	}
}
