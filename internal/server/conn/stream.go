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

// writePlayerPacket is the writer a Player is handed (see player.NewPlayer).
//
// A Player's packets arrive from two places while the migration is in flight:
// manager.go and item_entity.go now build generated v1_8 values, whereas the
// still-pc_1_8 handlers (handler_play.go, inventory.go, commands.go) hand the
// manager's broadcast methods pc_1_8 structs. Those broadcasts fan out through
// this same closure, so it routes by value type: a generated value carries an
// Encode method and goes through send (the session encodes and can inspect it),
// a pc_1_8 struct has only mc tags and goes through the reflective shim.
//
// Sending a generated value through the reflective path would silently emit an
// empty body (a generated type carries no mc tags), and sending a pc_1_8 struct
// through send fails loudly (the session rejects an unregistered value), so the
// split has to be made here rather than left to the caller. Task 6 retypes the
// remaining handlers onto send directly; once nothing hands this a pc_1_8
// struct, the dispatch collapses to send and this method goes away with the
// shim.
func (c *Connection) writePlayerPacket(v java.PacketValue) error {
	if _, generated := v.(interface{ Encode(*java.Buffer) error }); generated {
		return c.send(v)
	}
	return c.writeMarshalled(v)
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
