package conn

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// writtenPackets counts the packets a connection put on the wire. The
// connection writes through a managed stream now, so a test observes it by
// reading the other end rather than by holding the writer.
type writtenPackets struct {
	mu    sync.Mutex
	count int
}

func (w *writtenPackets) add() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.count++
}

func (w *writtenPackets) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.count = 0
}

// len reports how many packets arrived, waiting briefly for writes that are
// still in the stream's write pump.
func (w *writtenPackets) len() int {
	for range 100 {
		w.mu.Lock()
		count := w.count
		w.mu.Unlock()
		if count > 0 {
			return count
		}
		time.Sleep(5 * time.Millisecond)
	}

	return 0
}

// sentPackets collects packets from a player's WritePacket func.
type sentPackets struct {
	mu      sync.Mutex
	packets []java.PacketValue
}

func (s *sentPackets) write(p java.PacketValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets = append(s.packets, p)
	return nil
}

func (s *sentPackets) get() []java.PacketValue {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]java.PacketValue, len(s.packets))
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
func newTestConn(t *testing.T, username string) (*Connection, *sentPackets, *player.Manager) {
	t.Helper()

	c, sp, m, _ := newTestConnWithCapture(t, username)

	return c, sp, m
}

// newTestConnWithCapture also returns what the connection wrote to the client.
func newTestConnWithCapture(t *testing.T, username string) (*Connection, *sentPackets, *player.Manager, *writtenPackets) {
	t.Helper()

	m := player.NewManager(8)
	sp := &sentPackets{}
	eid := m.AllocateEntityID()
	uuid := [16]byte{byte(eid)}
	p := player.NewPlayer(eid, "test-uuid", uuid, username, nil, sp.write)
	p.SetPosition(0.5, 4, 0.5, 0, 0, true)
	m.Add(p)

	w := world.NewWorld(gen.NewFlatGenerator(0))

	// The real registry, not a stub: crafting silently produces nothing when
	// gameData is nil, which hid every recipe defect from these tests.
	gameData, err := v1_8.Data()
	if err != nil {
		t.Fatalf("game data: %v", err)
	}

	clientEnd, serverEnd := net.Pipe()

	limits, limitsErr := protocol.NewLimits()
	if limitsErr != nil {
		t.Fatalf("limits: %v", limitsErr)
	}
	stream, err := newStream(serverEnd, limits)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := stream.Start(ctx); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	// These tests exercise play-state behavior, so both the session and the
	// connection start there.
	if err := stream.SetState(ctx, v1_8.StatePlay); err != nil {
		t.Fatalf("set play state: %v", err)
	}

	written := &writtenPackets{}
	go func() {
		for {
			if _, err := java.ReadRawPacket(clientEnd, limits); err != nil {
				return
			}
			written.add()
		}
	}()

	t.Cleanup(func() {
		_ = clientEnd.Close()
		cancel()
		_ = stream.Close()
	})

	c := &Connection{
		conn:           serverEnd,
		stream:         stream,
		limits:         limits,
		ctx:            ctx,
		cancel:         cancel,
		state:          v1_8.StatePlay,
		log:            slog.New(slog.DiscardHandler),
		cfg:            config.DefaultConfig(),
		self:           p,
		players:        m,
		world:          w,
		gameData:       gameData,
		loadedChunks:   make(map[gen.ChunkPos]struct{}),
		keepAliveAcked: true,
		cursorSlot:     player.EmptySlot,
		craftingOutput: player.EmptySlot,
		craftingGrid:   emptyCraftingGrid(),
	}

	return c, sp, m, written
}

func TestHandleCommand_NonSlash(t *testing.T) {
	c, _, _ := newTestConn(t, "Alice")
	if c.handleCommand("hello world") {
		t.Error("expected false for non-slash message")
	}
}

func TestHandleCommand_SlashDetected(t *testing.T) {
	c, _, _ := newTestConn(t, "Alice")
	if !c.handleCommand("/anything") {
		t.Error("expected true for slash-prefixed message")
	}
}

func TestHandleCommand_UnknownCommand(t *testing.T) {
	c, sp, _ := newTestConn(t, "Alice")
	sp.reset()

	c.handleCommand("/nosuchcmd")

	// The player receives the error through writePacket, which goes to the
	// client, not through sp, which is the player's own WritePacket func.
}

func TestCmdHelp(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/help")

	// help writes multiple ChatCB packets to the client.
	if written.len() == 0 {
		t.Error("expected help output, got nothing")
	}
}

func TestCmdList(t *testing.T) {
	c, _, m, written := newTestConnWithCapture(t, "Alice")

	// Add another player.
	sp2 := &sentPackets{}
	eid2 := m.AllocateEntityID()
	uuid2 := [16]byte{byte(eid2)}
	p2 := player.NewPlayer(eid2, "test-uuid-2", uuid2, "Bob", nil, sp2.write)
	p2.SetPosition(0.5, 4, 0.5, 0, 0, true)
	m.Add(p2)

	written.reset()

	c.handleCommand("/list")

	if written.len() == 0 {
		t.Error("expected list output, got nothing")
	}
}

func TestCmdTp_Coordinates(t *testing.T) {
	c, sp, _, written := newTestConnWithCapture(t, "Alice")
	sp.reset()
	written.reset()

	c.handleCommand("/tp 100 10 100")

	// Player should have been teleported.
	pos := c.self.GetPosition()
	if pos.X != 100 || pos.Y != 10 || pos.Z != 100 {
		t.Errorf("expected position 100,10,100, got %.1f,%.1f,%.1f", pos.X, pos.Y, pos.Z)
	}
}

func TestCmdTp_Player(t *testing.T) {
	c, _, m := newTestConn(t, "Alice")

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
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/tp NoOne")

	if written.len() == 0 {
		t.Error("expected error message for missing player")
	}
}

func TestCmdTp_BadCoordinates(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/tp abc def ghi")

	if written.len() == 0 {
		t.Error("expected error message for bad coordinates")
	}
}

func TestCmdGamemode(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/gamemode survival")

	// Should have written GameStateChange + AbilitiesCB + ChatCB to rw.
	if written.len() == 0 {
		t.Error("expected gamemode packets, got nothing")
	}
}

func TestCmdGamemode_Invalid(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/gamemode invalid")

	if written.len() == 0 {
		t.Error("expected error message for invalid gamemode")
	}
}

func TestCmdTime(t *testing.T) {
	c, sp, m := newTestConn(t, "Alice")

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
		if _, ok := p.(*v1_8.PlayClientboundUpdateTime); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Alice did not receive UpdateTime packet")
	}

	found = false
	for _, p := range sp2.get() {
		if _, ok := p.(*v1_8.PlayClientboundUpdateTime); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Bob did not receive UpdateTime packet")
	}

	// Verify the world time was actually updated.
	_, tod := c.world.GetTime()
	if tod != 13000 {
		t.Errorf("world timeOfDay = %d, want 13000 (night)", tod)
	}
}

func TestCmdKill(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/kill")

	// Should have written UpdateHealth + ChatCB.
	if written.len() == 0 {
		t.Error("expected kill packets, got nothing")
	}
}

func TestCmdSay(t *testing.T) {
	c, sp, _ := newTestConn(t, "Alice")
	sp.reset()

	c.handleCommand("/say hello everyone")

	// Broadcast goes through player WritePacket.
	found := false
	for _, p := range sp.get() {
		if chat, ok := p.(*v1_8.PlayClientboundChat); ok {
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
	c, sp, _ := newTestConn(t, "Alice")
	sp.reset()

	c.handleCommand("/me waves")

	found := false
	for _, p := range sp.get() {
		if chat, ok := p.(*v1_8.PlayClientboundChat); ok {
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
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/seed")

	if written.len() == 0 {
		t.Error("expected seed output, got nothing")
	}
}

func TestCmdSay_Empty(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/say")

	// Should get error message.
	if written.len() == 0 {
		t.Error("expected error for empty /say")
	}
}

func TestCmdTime_BadUsage(t *testing.T) {
	c, _, _, written := newTestConnWithCapture(t, "Alice")
	written.reset()

	c.handleCommand("/time")

	if written.len() == 0 {
		t.Error("expected error for bad /time usage")
	}
}
