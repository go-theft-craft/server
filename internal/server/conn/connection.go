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

	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// State represents the connection state.
type State int

const (
	StateHandshake State = iota
	StateStatus
	StateLogin
	StatePlay
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
	storage *storage.Storage

	mu    sync.Mutex
	state State

	// Player management
	players *player.Manager
	self    *player.Player

	// Chunk tracking (only accessed from Handle goroutine, no mutex needed)
	loadedChunks map[gen.ChunkPos]struct{}

	// KeepAlive tracking
	lastKeepAliveID   int32
	lastKeepAliveSent time.Time
	keepAliveAcked    bool

	// Inventory state (only accessed from Handle goroutine)
	cursorSlot     player.Slot
	craftingGrid   [4]player.Slot
	craftingOutput player.Slot

	// Drag state for mode 5 (paint/drag click)
	dragMode   int8
	dragSlots  []int16
	dragActive bool

	// Death state (only accessed from Handle goroutine)
	dead bool

	// Game data registries (blocks, materials, recipes, etc.)
	gameData *data.Set

	// SaveAll triggers a server-wide save (set by Server).
	SaveAll func()
}

// NewConnection creates a new Connection from a raw TCP connection.
//
// It returns an error because the connection now owns a managed stream, and a
// stream that cannot be built is a connection that cannot be served.
func NewConnection(ctx context.Context, conn net.Conn, cfg *config.Config, log *slog.Logger, w *world.World, players *player.Manager, store *storage.Storage, gd *data.Set) (*Connection, error) {
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

	stream, err := newStream(conn, limits, protocol.WithPreFrameHook(legacyPing))
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
		state:          StateHandshake,
		world:          w,
		storage:        store,
		players:        players,
		loadedChunks:   make(map[gen.ChunkPos]struct{}),
		keepAliveAcked: true,
		cursorSlot:     player.EmptySlot,
		craftingOutput: player.EmptySlot,
		craftingGrid:   [4]player.Slot{player.EmptySlot, player.EmptySlot, player.EmptySlot, player.EmptySlot},
		gameData:       gd,
	}, nil
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
		c.cancel()
		c.conn.Close()
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
				c.log.Info("client disconnected", "state", c.state)

				return
			}

			c.log.Error("handling packet", "state", c.state, "error", err)

			return
		}
	}
}

func (c *Connection) handleNextPacket() error {
	// Login is a phase, not a packet. The acceptor owns inbound delivery for
	// its whole duration, so the read loop hands over rather than dispatching.
	if c.state == StateLogin {
		return c.runLogin()
	}

	packet, err := c.readPacket(c.ctx)
	if err != nil {
		return err
	}

	// Handshake and status read the value the session decoded. Play still
	// takes the raw payload and decodes it into the local structs; M6 moves
	// those to generated types too.
	switch c.state {
	case StateHandshake:
		return c.handleHandshake(packet)
	case StateStatus:
		return c.handleStatus(packet)
	case StatePlay:
		return c.handlePlay(packet.ID, packet.Payload)
	default:
		return fmt.Errorf("unknown state: %d", c.state)
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
