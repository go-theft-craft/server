package conn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
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
	conn   net.Conn
	cfg    *config.Config
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	state State
}

// NewConnection creates a new Connection from a raw TCP connection.
func NewConnection(ctx context.Context, conn net.Conn, cfg *config.Config, log *slog.Logger) *Connection {
	ctx, cancel := context.WithCancel(ctx)
	return &Connection{
		conn:   conn,
		cfg:    cfg,
		log:    log.With("addr", conn.RemoteAddr().String()),
		ctx:    ctx,
		cancel: cancel,
		state:  StateHandshake,
	}
}

// Handle runs the connection lifecycle. It reads packets and dispatches
// them to the appropriate state handler until the connection closes.
func (c *Connection) Handle() {
	defer func() {
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
	packetID, data, err := mcnet.ReadRawPacket(c.conn)
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
	return mcnet.WritePacket(c.conn, p)
}

// disconnect sends a disconnect packet and closes the connection.
func (c *Connection) disconnect(reason string) {
	c.log.Info("disconnecting", "reason", reason)
	c.cancel()
}
