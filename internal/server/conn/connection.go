package conn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// windowKind names what the open window is. The player's own inventory is
// always open underneath and is the zero value.
type windowKind uint8

const (
	windowPlayer windowKind = iota
	windowTable
	windowChest
)

// Connection manages a single client connection through the protocol state machine.
type Connection struct {
	conn    net.Conn
	stream  *protocol.Stream
	limits  protocol.Limits
	cfg     *config.Config
	log     *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	world   *world.World
	storage PlayerStore

	// disconnectMu is held while a disconnect writes its reason. The stream
	// runtime was started with c.ctx, so the read loop's teardown cancelling
	// that context aborts a write still in flight — and the write in flight is
	// the one carrying the kick message. Both sides take this, so whichever
	// runs second finds the connection already ended rather than half-ended.
	disconnectMu sync.Mutex

	// state caches the session's protocol state so the write path can stamp a
	// packet without a Snapshot round-trip. It is refreshed from the session
	// (syncState) at each transition and read through streamState; c.mu guards
	// it because broadcasts read it from other players' goroutines.
	mu    sync.Mutex
	state protocol.State

	// Player management
	players *player.Manager
	self    *player.Player

	// Chunk tracking (only accessed from Handle goroutine, no mutex needed)
	loadedChunks map[world.ChunkPos]struct{}

	// KeepAlive tracking
	lastKeepAliveID   int32
	lastKeepAliveSent time.Time
	keepAliveAcked    bool

	// Inventory state (only accessed from Handle goroutine)
	cursorSlot player.Slot
	// craftingGrid is row-major and sized for the largest grid a window can
	// show. The open window decides how much of it is in play: the player's own
	// window uses the first four cells as a 2x2, a crafting table all nine as a
	// 3x3.
	craftingGrid   [9]player.Slot
	craftingOutput player.Slot
	// windowID is the window the player has open, or 0 for their own
	// inventory, which is always open and never allocated an ID.
	windowID int8
	// windowKind says what that window is. A crafting table and a chest both
	// carry a non-zero ID and their slot numbers mean different things, so the
	// ID alone cannot decide the layout.
	windowKind windowKind
	// chestPositions is the open chest: one position, or two in window order
	// for a double chest. chestItems is a working copy of their contents, 27
	// slots per position. The world owns the stored copy; this one is written
	// back on every click and when the window closes.
	chestPositions []world.BlockPos
	chestItems     []world.ItemStack
	// nextWindowID is where the next allocation starts. Window IDs cycle
	// through 1-100; 0 is reserved for the player's own inventory.
	nextWindowID int8

	// Drag state for mode 5 (paint/drag click)
	dragMode   int8
	dragSlots  []int16
	dragActive bool

	// Health and death state (only accessed from Handle goroutine).
	// lastDamage is when the player was last hurt, which is what gives them
	// the brief immunity every damage source shares.
	health     float32
	lastDamage time.Time
	dead       bool

	// Game data registries (blocks, materials, recipes, etc.)
	gameData *data.Set

	// states is the connection's block state vocabulary: the registry the
	// world interns into and the adapter that turns a handle into what a
	// client is told. See blockstate.go.
	states blockStates

	// index is the item index, or nil when item identity is off, which is the
	// default. Every click path writes through it rather than telling it
	// afterwards; see identity.go.
	index world.ItemIndex

	// blocks is the sparse table of identified blocks and blockRec is where a
	// placement or a break is written. Both are nil unless the server was
	// built with item identity, and both tolerate being nil.
	blocks   *storage.BlockIdentity
	blockRec BlockRecorder

	// measure is where this connection reports how long a piece of work took,
	// or nil when nobody is watching. metricsPlayer is the label it carries,
	// stored once at login rather than read out of the player on every sample:
	// a join sends 625 chunks and each one is a span.
	measure       Measure
	count         Count
	metricsPlayer string

	// dispatch and complete are the command path, or nil on a connection
	// built without a server behind it. See commands.go.
	dispatch Dispatcher
	complete Completer

	// SaveAll triggers a server-wide save (set by Server).
	SaveAll func()
}

// PlayerStore is the per-connection half of persistence: a player's saved
// state restored at join and written back at disconnect.
//
// It names only the runtime player. The public PlayerData shape lives in the
// server package, which sits above both this package and whatever store an
// application supplied, and the conversion between the two lives there with
// it. A server built without persistence passes nothing, and the nil guards at
// both call sites hold.
type PlayerStore interface {
	// LoadPlayer restores saved state into p and reports whether there was
	// any. A player who has never logged in is false with no error.
	LoadPlayer(p *player.Player) (bool, error)
	SavePlayer(p *player.Player) error
}

// NewConnection creates a new Connection from a raw TCP connection.
//
// The stream options are the server's, not the connection's: an observed
// server installs an observation sink through them, and a server nobody asked
// for metrics passes none and pays nothing per frame.
//
// It returns an error because the connection now owns a managed stream, and a
// stream that cannot be built is a connection that cannot be served.
func NewConnection(ctx context.Context, conn net.Conn, cfg *config.Config, log *slog.Logger, w *world.World, players *player.Manager, store PlayerStore, gd *data.Set, streamOptions ...protocol.StreamOption) (*Connection, error) {
	ctx, cancel := context.WithCancel(ctx)

	limits, err := protocol.NewLimits()
	if err != nil {
		cancel()

		return nil, fmt.Errorf("build protocol limits: %w", err)
	}

	legacyPing, err := newLegacyPingHook(cfg, players)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("build legacy ping hook: %w", err)
	}

	options := append([]protocol.StreamOption{protocol.WithPreFrameHook(legacyPing)}, streamOptions...)

	stream, err := newStream(conn, limits, options...)
	if err != nil {
		cancel()

		return nil, err
	}

	return &Connection{
		conn:           conn,
		stream:         stream,
		limits:         limits,
		cfg:            cfg,
		log:            log.With("addr", conn.RemoteAddr().String()),
		ctx:            ctx,
		cancel:         cancel,
		state:          v1_8.StateHandshaking,
		world:          w,
		storage:        store,
		players:        players,
		loadedChunks:   make(map[world.ChunkPos]struct{}),
		keepAliveAcked: true,
		cursorSlot:     player.EmptySlot,
		craftingOutput: player.EmptySlot,
		craftingGrid:   emptyCraftingGrid(),
		gameData:       gd,
		states:         newBlockStates(w, gd),
	}, nil
}

// SetItemIndex puts this connection's click paths on the index's write path.
//
// It is set before Handle starts, the same way SaveAll is, because the
// handlers read it without a lock. A server built without item identity never
// calls it and every helper in identity.go stays the arithmetic it always was.
func (c *Connection) SetItemIndex(index world.ItemIndex) { c.index = index }

// BlockRecorder is told what happened to a block that has an identity.
//
// It is an interface here rather than the server's *Recorder because a record
// is a public type of the server package and this one sits below it. The two
// methods are the two events block identity has: an item became a block, and a
// block became items again.
type BlockRecorder interface {
	RecordBlockPlace(pos world.BlockPos, block string, id world.ItemID, from []world.ItemID, by world.Actor)
	RecordBlockBreak(pos world.BlockPos, block string, id world.ItemID, drops []world.ItemID, by world.Actor)
}

// SetBlockIdentity puts this connection on the block identity write path. Like
// SetItemIndex it is set before Handle starts and read without a lock.
func (c *Connection) SetBlockIdentity(blocks *storage.BlockIdentity, rec BlockRecorder) {
	c.blocks, c.blockRec = blocks, rec
}

// Measure is where a connection reports how long a piece of work took.
//
// The player is a parameter rather than baked into the closure because a
// connection exists before anyone has logged in on it, and the server that
// supplies this has no name to bake in at that point.
type Measure func(feature, player string, pos world.ChunkPos) func()

// Count is where a connection reports an event too frequent to time: a block
// write, an inventory click. The server accumulates them and emits one sample
// per tick rather than one per event.
type Count func(feature, player string, n float64)

// SetMeasure gives the connection somewhere to report. Set before Handle
// starts, like SetItemIndex, and read without a lock.
func (c *Connection) SetMeasure(m Measure, count Count) { c.measure, c.count = m, count }

// counted records n of something, or nothing at all.
func (c *Connection) counted(feature string, n float64) {
	if c.count == nil {
		return
	}

	c.count(feature, c.metricsPlayer, n)
}

// span starts a measurement, or does nothing.
//
// It returns nil rather than a no-op closure so that an unobserved server pays
// one branch here and one at the end, and no indirect call at all inside the
// loop that sends a join's chunks.
func (c *Connection) span(feature string, pos world.ChunkPos) func() {
	if c.measure == nil {
		return nil
	}

	return c.measure(feature, c.metricsPlayer, pos)
}

// endSpan closes a span that may be nil.
func endSpan(span func()) {
	if span != nil {
		span()
	}
}

// Handle runs the connection lifecycle. It reads packets and dispatches
// them to the appropriate state handler until the connection closes.
func (c *Connection) Handle() {
	defer func() {
		if c.self != nil {
			if c.storage != nil {
				if err := c.storage.SavePlayer(c.self); err != nil {
					c.log.Error("save player on disconnect", "error", err)
				}
			}
			c.players.Remove(c.self)
		}
		c.disconnectMu.Lock()
		c.cancel()
		c.conn.Close()
		c.disconnectMu.Unlock()
		c.log.Info("connection closed")
	}()

	c.log.Info("connection accepted")

	if err := c.stream.Start(c.ctx); err != nil {
		c.log.Error("start stream", "error", err)

		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if err := c.handleNextPacket(); err != nil {
			if c.ctx.Err() != nil {
				return
			}
			if isNormalDisconnect(err) {
				c.log.Info("client disconnected", "state", c.streamState())

				return
			}

			c.log.Error("handling packet", "state", c.streamState(), "error", err)

			return
		}
	}
}

func (c *Connection) handleNextPacket() error {
	state := c.streamState()

	// Login is a phase, not a packet. The acceptor owns inbound delivery for
	// its whole duration, so the read loop hands over rather than dispatching.
	if state == v1_8.StateLogin {
		return c.runLogin()
	}

	packet, err := c.readPacket(c.ctx)
	if err != nil {
		return err
	}

	// Every state now reads the value the session decoded rather than
	// re-decoding the raw payload.
	switch state {
	case v1_8.StateHandshaking:
		return c.handleHandshake(packet)
	case v1_8.StateStatus:
		return c.handleStatus(packet)
	case v1_8.StatePlay:
		return c.handlePlay(packet)
	default:
		return fmt.Errorf("unknown state: %q", state)
	}
}

// isNormalDisconnect reports whether an error is a client going away rather
// than a fault worth logging as one.
//
// The stream wraps the transport's EOF rather than returning it, so an
// equality test against io.EOF stopped matching when the connection moved onto
// it, and every player who quit was logged as an error. A stream that is
// closed or closing is the same event seen from the write side: the peer left
// while the server was mid-send.
func isNormalDisconnect(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe) ||
		// A client that vanishes without a FIN — killed, crashed, or with its
		// network dropped — is still a client leaving, not a server fault.
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, protocol.ErrStreamClosed) ||
		errors.Is(err, protocol.ErrStreamClosing)
}

// disconnectTimeout bounds how long a graceful disconnect waits for the
// client to accept the packet that explains it.
const disconnectTimeout = 5 * time.Second

// disconnect tells the client why the connection is ending, then ends it.
//
// Stream.Shutdown sends the disconnect packet the current state calls for —
// login or play — drains the writes already accepted, and only then interrupts
// the transport. The socket used to close with nothing on it, which a client
// can only report as a lost connection.
func (c *Connection) disconnect(reason string) {
	c.log.Info("disconnecting", "reason", reason)

	c.disconnectMu.Lock()
	defer c.disconnectMu.Unlock()

	// Not c.ctx: a server-wide shutdown cancels it before this runs, and a
	// cancelled context would abort the very write that carries the reason.
	// The bound is what keeps a stalled client from holding the shutdown.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), disconnectTimeout)
	defer cancel()

	if err := c.stream.Shutdown(ctx, reason); err != nil {
		c.log.Debug("graceful shutdown failed", "error", err)
	}

	c.cancel()
}

// Disconnect ends this connection from outside its own goroutine, which is
// what a shutting-down server does to every player still connected.
func (c *Connection) Disconnect(reason string) { c.disconnect(reason) }
