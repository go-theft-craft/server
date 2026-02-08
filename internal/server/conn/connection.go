package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/gamedata"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
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
	rw      io.ReadWriter
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
	gameData *gamedata.GameData

	// SaveAll triggers a server-wide save (set by Server).
	SaveAll func()
}

// NewConnection creates a new Connection from a raw TCP connection.
func NewConnection(ctx context.Context, conn net.Conn, cfg *config.Config, log *slog.Logger, w *world.World, players *player.Manager, store *storage.Storage, gd *gamedata.GameData) *Connection {
	ctx, cancel := context.WithCancel(ctx)
	return &Connection{
		conn:           conn,
		rw:             conn,
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
		c.cancel()
		c.conn.Close()
		c.log.Info("connection closed")
	}()

	c.log.Info("connection accepted")

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
	packetID, data, err := mcnet.ReadRawPacket(c.rw)
	if err != nil {
		return err
	}

	switch c.state {
	case StateHandshake:
		return c.handleHandshake(packetID, data)
	case StateStatus:
		return c.handleStatus(packetID, data)
	case StateLogin:
		return c.handleLogin(packetID, data)
	case StatePlay:
		return c.handlePlay(packetID, data)
	default:
		return fmt.Errorf("unknown state: %d", c.state)
	}
}

// writePacket writes a packet to the connection under the write lock.
func (c *Connection) writePacket(p mcnet.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return mcnet.WritePacket(c.rw, p)
}

// disconnect sends a disconnect packet and closes the connection.
func (c *Connection) disconnect(reason string) {
	c.log.Info("disconnecting", "reason", reason)
	c.cancel()
}

// enableEncryption wraps the connection with AES/CFB8 encryption.
func (c *Connection) enableEncryption(sharedSecret []byte) error {
	enc, err := newEncryptedConn(c.conn, sharedSecret)
	if err != nil {
		return err
	}
	c.rw = enc
	return nil
}
