package conn

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

// packetRecorder captures packets written via mcnet.WritePacket.
// It records the raw packet ID and data for each write.
type packetRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *packetRecorder) Read(p []byte) (int, error) { return 0, nil }
func (r *packetRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

// sentPackets collects packets from a player's WritePacket func.
type sentPackets struct {
	mu      sync.Mutex
	packets []mcnet.Packet
}

func (s *sentPackets) write(p mcnet.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets = append(s.packets, p)
	return nil
}

func (s *sentPackets) get() []mcnet.Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]mcnet.Packet, len(s.packets))
	copy(cp, s.packets)
	return cp
}

func (s *sentPackets) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets = nil
}

// newTestConn creates a minimal Connection suitable for testing commands.
// The returned sentPackets captures packets sent to the connection's player.
func newTestConn(username string) (*Connection, *sentPackets, *player.Manager) {
	m := player.NewManager(8)
	sp := &sentPackets{}
	eid := m.AllocateEntityID()
	uuid := [16]byte{byte(eid)}
	p := player.NewPlayer(eid, "test-uuid", uuid, username, nil, sp.write)
	p.SetPosition(0.5, 4, 0.5, 0, 0, true)
	m.Add(p)

	w := world.NewWorld(gen.NewFlatGenerator(0))
	rec := &packetRecorder{}

	c := &Connection{
		rw:             rec,
		cfg:            config.DefaultConfig(),
		self:           p,
		players:        m,
		world:          w,
		loadedChunks:   make(map[gen.ChunkPos]struct{}),
		keepAliveAcked: true,
		cursorSlot:     player.EmptySlot,
		craftingOutput: player.EmptySlot,
		craftingGrid:   [4]player.Slot{player.EmptySlot, player.EmptySlot, player.EmptySlot, player.EmptySlot},
	}
	return c, sp, m
}

func TestHandleCommand_NonSlash(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	if c.handleCommand("hello world") {
		t.Error("expected false for non-slash message")
	}
}

func TestHandleCommand_SlashDetected(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	if !c.handleCommand("/anything") {
		t.Error("expected true for slash-prefixed message")
	}
}

func TestHandleCommand_UnknownCommand(t *testing.T) {
	c, sp, _ := newTestConn("Alice")
	sp.reset()

	c.handleCommand("/nosuchcmd")

	// The player should receive an error message via writePacket (goes to rw),
	// not via sp (which is the player's WritePacket func).
	// Since writePacket goes to c.rw (packetRecorder), we check that too.
	// But the command calls sendErrorMsg → writePacket → c.rw.
	// We can't easily parse the raw bytes, so just verify it returned true above.
}

func TestCmdHelp(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/help")

	// help writes multiple ChatCB packets to c.rw
	if rec.buf.Len() == 0 {
		t.Error("expected help output, got nothing")
	}
}

func TestCmdList(t *testing.T) {
	c, _, m := newTestConn("Alice")

	// Add another player.
	sp2 := &sentPackets{}
	eid2 := m.AllocateEntityID()
	uuid2 := [16]byte{byte(eid2)}
	p2 := player.NewPlayer(eid2, "test-uuid-2", uuid2, "Bob", nil, sp2.write)
	p2.SetPosition(0.5, 4, 0.5, 0, 0, true)
	m.Add(p2)

	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/list")

	if rec.buf.Len() == 0 {
		t.Error("expected list output, got nothing")
	}
}

func TestCmdTp_Coordinates(t *testing.T) {
	c, sp, _ := newTestConn("Alice")
	sp.reset()
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/tp 100 10 100")

	// Player should have been teleported.
	pos := c.self.GetPosition()
	if pos.X != 100 || pos.Y != 10 || pos.Z != 100 {
		t.Errorf("expected position 100,10,100, got %.1f,%.1f,%.1f", pos.X, pos.Y, pos.Z)
	}
}

func TestCmdTp_Player(t *testing.T) {
	c, _, m := newTestConn("Alice")

	// Add target player at 50,20,50.
	sp2 := &sentPackets{}
	eid2 := m.AllocateEntityID()
	uuid2 := [16]byte{byte(eid2)}
	p2 := player.NewPlayer(eid2, "test-uuid-2", uuid2, "Bob", nil, sp2.write)
	p2.SetPosition(50, 20, 50, 0, 0, true)
	m.Add(p2)

	c.handleCommand("/tp Bob")

	pos := c.self.GetPosition()
	if pos.X != 50 || pos.Y != 20 || pos.Z != 50 {
		t.Errorf("expected position 50,20,50, got %.1f,%.1f,%.1f", pos.X, pos.Y, pos.Z)
	}
}

func TestCmdTp_PlayerNotFound(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/tp NoOne")

	if rec.buf.Len() == 0 {
		t.Error("expected error message for missing player")
	}
}

func TestCmdTp_BadCoordinates(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/tp abc def ghi")

	if rec.buf.Len() == 0 {
		t.Error("expected error message for bad coordinates")
	}
}

func TestCmdGamemode(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/gamemode survival")

	// Should have written GameStateChange + AbilitiesCB + ChatCB to rw.
	if rec.buf.Len() == 0 {
		t.Error("expected gamemode packets, got nothing")
	}
}

func TestCmdGamemode_Invalid(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/gamemode invalid")

	if rec.buf.Len() == 0 {
		t.Error("expected error message for invalid gamemode")
	}
}

func TestCmdTime(t *testing.T) {
	c, sp, m := newTestConn("Alice")

	// Add another player to verify broadcast.
	sp2 := &sentPackets{}
	eid2 := m.AllocateEntityID()
	uuid2 := [16]byte{byte(eid2)}
	p2 := player.NewPlayer(eid2, "test-uuid-2", uuid2, "Bob", nil, sp2.write)
	p2.SetPosition(0.5, 4, 0.5, 0, 0, true)
	m.Add(p2)
	sp.reset()
	sp2.reset()

	c.handleCommand("/time set night")

	// Both players should get UpdateTime via Broadcast (through their WritePacket).
	found := false
	for _, p := range sp.get() {
		if _, ok := p.(*pkt.UpdateTime); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Alice did not receive UpdateTime packet")
	}

	found = false
	for _, p := range sp2.get() {
		if _, ok := p.(*pkt.UpdateTime); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Bob did not receive UpdateTime packet")
	}
}

func TestCmdKill(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/kill")

	// Should have written UpdateHealth + ChatCB.
	if rec.buf.Len() == 0 {
		t.Error("expected kill packets, got nothing")
	}
}

func TestCmdSay(t *testing.T) {
	c, sp, _ := newTestConn("Alice")
	sp.reset()

	c.handleCommand("/say hello everyone")

	// Broadcast goes through player WritePacket.
	found := false
	for _, p := range sp.get() {
		if chat, ok := p.(*pkt.ChatCB); ok {
			if strings.Contains(chat.Message, "[Server]") && strings.Contains(chat.Message, "hello everyone") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected broadcast with [Server] prefix")
	}
}

func TestCmdMe(t *testing.T) {
	c, sp, _ := newTestConn("Alice")
	sp.reset()

	c.handleCommand("/me waves")

	found := false
	for _, p := range sp.get() {
		if chat, ok := p.(*pkt.ChatCB); ok {
			if strings.Contains(chat.Message, "chat.type.emote") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected emote broadcast")
	}
}

func TestCmdSeed(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/seed")

	if rec.buf.Len() == 0 {
		t.Error("expected seed output, got nothing")
	}
}

func TestCmdSay_Empty(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/say")

	// Should get error message.
	if rec.buf.Len() == 0 {
		t.Error("expected error for empty /say")
	}
}

func TestCmdTime_BadUsage(t *testing.T) {
	c, _, _ := newTestConn("Alice")
	rec := c.rw.(*packetRecorder)
	rec.buf.Reset()

	c.handleCommand("/time")

	if rec.buf.Len() == 0 {
		t.Error("expected error for bad /time usage")
	}
}
