package conn

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
)

func TestWritePacketHandsTheSessionADecodedValue(t *testing.T) {
	// A packet written with Value set must reach the wire with the same
	// bytes the reflect-codec path produced, and must let the session see
	// what was written.
	c, peer := newTestConnection(t)
	defer peer.Close()

	err := c.writePacket(protocol.Packet{
		State:     v1_8.StatePlay,
		Direction: protocol.DirectionClientbound,
		ID:        v1_8.PlayClientboundKeepAlive{}.PacketID(),
		Value:     &v1_8.PlayClientboundKeepAlive{KeepAliveID: 4242},
	})
	if err != nil {
		t.Fatalf("writePacket: %v", err)
	}

	got := readOneFrame(t, peer)
	want := marshalWithOldCodec(t, &pkt.KeepAliveCB{KeepAliveID: 4242})
	if !bytes.Equal(got, want) {
		t.Errorf("generated encoding differs from the reflect codec:\n got %x\nwant %x", got, want)
	}
}

func TestSessionStateFollowsAWrittenPacket(t *testing.T) {
	// Writing the login success packet must move the session to play on its
	// own, without the connection setting the state itself.
	c, peer := newTestConnectionInState(t, v1_8.StateLogin)
	defer peer.Close()

	if err := c.send(&v1_8.LoginClientboundSuccess{
		UUID:     "00000000-0000-0000-0000-000000000000",
		Username: "tester",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	snapshot, err := c.stream.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.State != v1_8.StatePlay {
		t.Errorf("session is in %q after login success, want play", snapshot.State)
	}
}

func TestWritePacketRejectsAPacketWithNoValue(t *testing.T) {
	c, peer := newTestConnection(t)
	defer peer.Close()

	err := c.writePacket(protocol.Packet{
		State:     v1_8.StatePlay,
		Direction: protocol.DirectionClientbound,
		ID:        0x00,
	})
	if err == nil {
		t.Fatal("writePacket accepted a packet with neither Value nor Payload")
	}
}

// newTestConnection builds a play-state Connection facing a client peer.
//
// The transport is a TCP loopback pair rather than net.Pipe: a small frame
// buffers in the kernel, so a write returns without the peer having read yet,
// which is what lets the byte-equality test write and then read in sequence.
func newTestConnection(t *testing.T) (*Connection, net.Conn) {
	t.Helper()

	return newTestConnectionInState(t, v1_8.StatePlay)
}

// newTestConnectionInState builds a Connection whose session and cached state
// both start in the requested protocol state, so a test can exercise a write
// the session only accepts in that state (login success needs login).
func newTestConnectionInState(t *testing.T, state protocol.State) (*Connection, net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	type dialed struct {
		conn net.Conn
		err  error
	}
	dials := make(chan dialed, 1)
	go func() {
		conn, err := net.Dial("tcp", listener.Addr().String())
		dials <- dialed{conn, err}
	}()

	serverEnd, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	client := <-dials
	if client.err != nil {
		t.Fatalf("dial: %v", client.err)
	}
	clientEnd := client.conn

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	stream, err := newStream(serverEnd, limits)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := stream.Start(ctx); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	if err := stream.SetState(ctx, state); err != nil {
		t.Fatalf("set state %q: %v", state, err)
	}

	t.Cleanup(func() {
		_ = clientEnd.Close()
		cancel()
		_ = stream.Close()
		_ = serverEnd.Close()
	})

	c := &Connection{
		conn:   serverEnd,
		stream: stream,
		limits: limits,
		ctx:    ctx,
		cancel: cancel,
		state:  state,
		log:    slog.New(slog.DiscardHandler),
	}

	return c, clientEnd
}

// readOneFrame reads one uncompressed frame from the peer and returns its
// packet body — the varint packet ID followed by the field bytes, exactly as
// it sits on the wire. This is the encoding the generated value produced.
func readOneFrame(t *testing.T, peer net.Conn) []byte {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	packet, err := java.ReadRawPacket(peer, limits)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	body, err := java.JoinPacketBody(packet, limits)
	if err != nil {
		t.Fatalf("join packet body: %v", err)
	}

	return body
}

// marshalWithOldCodec reproduces the packet body the reflect-codec write path
// produced: java.Marshal for the field bytes, framed with the packet ID the
// same way the stream frames one. Comparing this against readOneFrame proves
// the generated value encodes to the same wire bytes as the old struct.
//
// It is the bridge that proves the two encodings agree, and it is deleted in
// Task 8 along with the package it marshals.
func marshalWithOldCodec(t *testing.T, p java.PacketValue) []byte {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	payload, err := java.Marshal(p, limits)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body, err := java.JoinPacketBody(protocol.Packet{ID: p.PacketID(), Payload: payload}, limits)
	if err != nil {
		t.Fatalf("join packet body: %v", err)
	}

	return body
}
