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

// streamState returns the session's protocol state as the connection last
// observed it.
//
// It reads a cache rather than the session, because the write path calls it on
// every write — some of them broadcasts from other players' goroutines — and a
// Snapshot round-trip there would take a context and could fail. The cache is
// refreshed from the session by syncState at each transition, and the session's
// play state is terminal, so a write can never read a state the session has
// already moved past. c.mu makes the read memory-safe across goroutines.
func (c *Connection) streamState() protocol.State {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state
}

// syncState refreshes the cached protocol state from the session.
//
// M1 recorded that a running stream owns its session exclusively, so Snapshot
// is the only safe way to read the state the session moved itself to. This is
// called on the Handle goroutine at each transition — after the handshake is
// read, and after login success is written — where a context and an error are
// available. The session is the single source of truth; the connection no
// longer re-derives the transition it already made.
func (c *Connection) syncState(ctx context.Context) error {
	snapshot, err := c.stream.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read session state: %w", err)
	}

	c.mu.Lock()
	c.state = snapshot.State
	c.mu.Unlock()

	return nil
}

// send builds and writes one clientbound packet as a generated value.
//
// The session encodes the value and, where the packet implies one, proposes
// the state or pipeline transition that goes with it. A raw payload gives the
// session nothing to inspect and so proposes nothing. Every clientbound call
// site now sends a generated value through here.
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

// writePlayerPacket is the writer a Player is handed (see player.NewPlayer).
//
// Every clientbound packet a Player emits — its own and the manager's
// broadcasts that fan out through this same closure — is a generated v1_8
// value now, so this hands each straight to send, where the session encodes it
// and can inspect it for a state or pipeline transition.
func (c *Connection) writePlayerPacket(v java.PacketValue) error {
	return c.send(v)
}
