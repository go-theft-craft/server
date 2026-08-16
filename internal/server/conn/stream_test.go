package conn

import (
	"context"
	"log/slog"
	"net"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

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
