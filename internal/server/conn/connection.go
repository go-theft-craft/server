package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/data"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

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

	// Login state (online mode)
	loginUsername    string
	loginVerifyToken []byte

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

	stream, err := newStream(conn, limits)
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
			if err == io.EOF {
				return
			}
			c.log.Error("handling packet", "state", c.state, "error", err)
			return
		}
	}
}

func (c *Connection) handleNextPacket() error {
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
	case StateLogin:
		return c.handleLogin(packet.ID, packet.Payload)
	case StatePlay:
		return c.handlePlay(packet.ID, packet.Payload)
	default:
		return fmt.Errorf("unknown state: %d", c.state)
	}
}

// disconnect sends a disconnect packet and closes the connection.
func (c *Connection) disconnect(reason string) {
	c.log.Info("disconnecting", "reason", reason)
	c.cancel()
}

// enableEncryption installs AES/CFB8 on the stream's transport.
//
// The cipher covers the frame length prefix, which the session never sees, so
// it is applied to the stream rather than proposed by a packet.
func (c *Connection) enableEncryption(sharedSecret []byte) error {
	secret, err := java.SharedSecretFrom(sharedSecret)
	if err != nil {
		return fmt.Errorf("adopt session key: %w", err)
	}

	return c.stream.Control(c.ctx, java.EncryptionControl{Secret: secret})
}
