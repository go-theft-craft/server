package conn

import (
	"context"
	"fmt"
	"net"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	mcnet "github.com/go-theft-craft/server/pkg/protocol"
)

// newStream builds the managed stream one connection runs on.
//
// The stream owns framing, compression, and — once the login installs it —
// encryption. It is started by Handle rather than here, so construction stays
// free of I/O and of goroutines.
func newStream(conn net.Conn, limits protocol.Limits) (*protocol.Stream, error) {
	session, err := v1_8.Protocol().NewSession(protocol.RoleServer, limits)
	if err != nil {
		return nil, fmt.Errorf("create protocol session: %w", err)
	}

	stream, err := protocol.NewStream(session, protocol.Transport{
		Reader:    conn,
		Writer:    conn,
		Interrupt: conn.Close,
	})
	if err != nil {
		return nil, fmt.Errorf("create protocol stream: %w", err)
	}

	return stream, nil
}

// streamState maps the connection's own state enum onto the protocol's.
//
// Both exist for now. The session moves itself when it decodes a handshake or
// writes a login success, and the handlers still move c.state alongside it;
// Task 6 deletes the local enum once nothing reads it.
func (c *Connection) streamState() protocol.State {
	switch c.state {
	case StateHandshake:
		return v1_8.StateHandshaking
	case StateStatus:
		return v1_8.StateStatus
	case StateLogin:
		return v1_8.StateLogin
	case StatePlay:
		return v1_8.StatePlay
	default:
		return v1_8.StateHandshaking
	}
}

// writeValue sends one clientbound packet as a generated value.
//
// The session encodes it and, where the packet implies one, proposes the state
// or pipeline transition that goes with it. writePacket cannot do that: it
// hands over a raw payload, which the session has nothing to inspect.
func (c *Connection) writeValue(value packetValue) error {
	return c.stream.Write(c.ctx, protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        value.PacketID(),
		Value:     value,
	})
}

// packetValue is what both packet families have in common.
type packetValue interface {
	PacketID() int32
}

// setState moves the connection and its session to the same state.
//
// The session proposes a transition of its own when it decodes or encodes a
// generated packet that implies one, but this connection still writes raw
// payloads, which carry no value for it to inspect. Driving both explicitly
// keeps them from diverging; Task 6 lets the handshake and login packets
// propose their own transitions again.
func (c *Connection) setState(next State) error {
	c.state = next

	if err := c.stream.SetState(c.ctx, c.streamState()); err != nil {
		return fmt.Errorf("set stream state: %w", err)
	}

	return nil
}

// readPacket waits for the next packet the client sent.
func (c *Connection) readPacket(ctx context.Context) (protocol.Packet, error) {
	return c.stream.Read(ctx)
}

// writePacket sends one clientbound packet.
//
// Its signature is unchanged, so its eighty call sites did not move. The body
// marshals the local struct through the shared reflect codec — which reads the
// same mc tags — and hands the stream a raw payload. The stream serializes
// writes through its write pump, so this no longer takes a lock of its own.
func (c *Connection) writePacket(p mcnet.Packet) error {
	payload, err := java.Marshal(p, c.limits)
	if err != nil {
		return fmt.Errorf("marshal packet 0x%02X: %w", p.PacketID(), err)
	}

	return c.stream.Write(c.ctx, protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        p.PacketID(),
		Payload:   payload,
	})
}
