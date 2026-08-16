package conn

import (
	"context"
	"fmt"
	"net"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"
)

// newStream builds the managed stream one connection runs on.
//
// The stream owns framing, compression, and — once the login installs it —
// encryption. It is started by Handle rather than here, so construction stays
// free of I/O and of goroutines.
func newStream(
	conn net.Conn,
	limits protocol.Limits,
	options ...protocol.StreamOption,
) (*protocol.Stream, error) {
	session, err := v1_8.Protocol().NewSession(protocol.RoleServer, limits)
	if err != nil {
		return nil, fmt.Errorf("create protocol session: %w", err)
	}

	stream, err := protocol.NewStream(session, protocol.Transport{
		Reader:    conn,
		Writer:    conn,
		Interrupt: conn.Close,
	}, options...)
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

// send builds and writes one clientbound packet as a generated value.
//
// The session encodes the value and, where the packet implies one, proposes
// the state or pipeline transition that goes with it. A raw payload gives the
// session nothing to inspect and so proposes nothing — which is why M3 left
// play on raw payloads and mirrored the state locally. This is the call sites
// migrate onto (Stage C), one area at a time.
func (c *Connection) send(value packetValue) error {
	return c.writePacket(protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        value.PacketID(),
		Value:     value,
	})
}

// packetValue is what both packet families have in common.
//
// The root protocol package declares no shared interface — protocol.Packet's
// Value field is typed any, and the only PacketValue interface lives in the
// wire/java subpackage — so send takes this local one.
type packetValue interface {
	PacketID() int32
}

// readPacket waits for the next packet the client sent.
func (c *Connection) readPacket(ctx context.Context) (protocol.Packet, error) {
	return c.stream.Read(ctx)
}

// writePacket sends one clientbound packet.
//
// It hands the stream a decoded value rather than a raw payload, so the
// session encodes it and can inspect it. That is what lets the session
// propose a state transition: M3 left play on raw payloads and mirrored the
// state locally as a result, and this is where that mirror stops being
// necessary. The stream serializes writes through its write pump, so this
// takes no lock of its own.
func (c *Connection) writePacket(p protocol.Packet) error {
	if p.Value == nil && p.Payload == nil {
		return fmt.Errorf("write packet 0x%02X: neither value nor payload", p.ID)
	}

	return c.stream.Write(c.ctx, p)
}

// writeMarshalled marshals a not-yet-migrated pc_1_8 packet struct through the
// shared reflect codec and writes it as a raw payload.
//
// It is the transitional path for the call sites that still hand a pc_1_8
// struct rather than a generated value. Stage C retypes each of them onto
// send(&v1_8.…), and Task 8 deletes this together with the pc_1_8 package it
// marshals. The bytes it produces are identical to what writePacket produced
// before this task, so the byte-parity fixtures are unaffected while the
// migration is in flight.
func (c *Connection) writeMarshalled(p java.PacketValue) error {
	payload, err := java.Marshal(p, c.limits)
	if err != nil {
		return fmt.Errorf("marshal packet 0x%02X: %w", p.PacketID(), err)
	}

	return c.writePacket(protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        p.PacketID(),
		Payload:   payload,
	})
}
