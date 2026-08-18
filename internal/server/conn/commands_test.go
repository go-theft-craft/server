package conn

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
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

	w := newTestWorld(t)

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
		loadedChunks:   make(map[world.ChunkPos]struct{}),
		keepAliveAcked: true,
		cursorSlot:     player.EmptySlot,
		craftingOutput: player.EmptySlot,
		craftingGrid:   emptyCraftingGrid(),
		states:         newBlockStates(w, gameData),
	}

	return c, sp, m, written
}

// The command behaviour tests moved to server/dispatch_test.go when commands
// became values. They run against a fake Caller there, which is what lets them
// assert the reply a player actually sees rather than "some packet was
// written" — the most any of them could check from here.
//
// What is left is the part that is genuinely this connection's: deciding that a
// line is a command at all, and handing it to whatever the server wired in.

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

func TestHandleCommandHandsTheLineToTheDispatcher(t *testing.T) {
	c, _, _ := newTestConn(t, "Alice")

	var got string
	c.SetCommands(func(_ *Connection, line string) bool {
		got = line

		return true
	}, nil)

	if !c.handleCommand("/tp 1 2 3") {
		t.Error("a slash-prefixed line was not treated as a command")
	}
	if got != "/tp 1 2 3" {
		t.Errorf("the dispatcher was handed %q, want the whole line", got)
	}

	// Chat is not a command and must never reach the dispatcher, or every
	// message a player types would be parsed as one.
	got = ""
	if c.handleCommand("hello") {
		t.Error("a chat line was treated as a command")
	}
	if got != "" {
		t.Errorf("chat reached the dispatcher as %q", got)
	}
}

func TestACommandWithNoDispatcherIsRefusedRatherThanBroadcast(t *testing.T) {
	// A connection with no server behind it. Answering "not available" is the
	// only safe answer: returning false would let the line fall through to
	// chat, and everyone would see the player's attempted command.
	c, _, _ := newTestConn(t, "Alice")

	if !c.handleCommand("/anything") {
		t.Error("a command was passed through to chat when no dispatcher was set")
	}
}

func TestTabCompleteWithNoCompleterAnswersNothing(t *testing.T) {
	c, _, _ := newTestConn(t, "Alice")

	if err := c.handleTabComplete(&v1_8.PlayServerboundTabComplete{Text: "/t"}); err != nil {
		t.Fatalf("handleTabComplete: %v", err)
	}
}
