package conn

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/login"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// These drive the real client half from minecraft-protocol against the real
// server. It is the only harness that can follow a compression envelope or an
// encrypted stream, because it is a protocol.Stream on both ends.

// clientPair starts a server connection and returns a client stream already in
// the login state, facing it.
func clientPair(t *testing.T, configure func(*config.Config)) (*protocol.Stream, *Connection) {
	t.Helper()

	original := fetchSkin
	fetchSkin = func(context.Context, string) ([]mojangProperty, error) { return nil, nil }
	t.Cleanup(func() { fetchSkin = original })

	clientConn, serverConn := net.Pipe()

	gameData, err := v1_8.Data()
	if err != nil {
		t.Fatalf("load game data: %v", err)
	}

	settings := config.DefaultConfig()
	settings.PrivateKey = testServerKey()
	if configure != nil {
		configure(settings)
	}

	ctx, cancel := context.WithCancel(context.Background())

	connection, err := NewConnection(
		ctx,
		serverConn,
		settings,
		slog.New(slog.DiscardHandler),
		world.NewWorld(gen.NewFlatGenerator(0)),
		player.NewManager(8),
		nil,
		gameData,
	)
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}

	done := make(chan struct{})
	go func() {
		connection.Handle()
		close(done)
	}()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}
	session, err := v1_8.Protocol().NewSession(protocol.RoleClient, limits)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	client, err := protocol.NewStream(session, protocol.Transport{
		Reader:    clientConn,
		Writer:    clientConn,
		Interrupt: clientConn.Close,
	})
	if err != nil {
		t.Fatalf("client stream: %v", err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start client: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close()
		_ = clientConn.Close()
		cancel()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the connection did not shut down")
		}
	})

	// The handshake puts both ends into login.
	if err := client.Write(ctx, protocol.Packet{
		State:     v1_8.StateHandshaking,
		Direction: protocol.DirectionServerbound,
		ID:        v1_8.HandshakingServerboundSetProtocol{}.PacketID(),
		Value: &v1_8.HandshakingServerboundSetProtocol{
			ProtocolVersion: 47,
			ServerHost:      "localhost",
			ServerPort:      25565,
			NextState:       2,
		},
	}); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	return client, connection
}

// negotiateAgainstServer runs the real client negotiator through a login.
func negotiateAgainstServer(t *testing.T, client *protocol.Stream) login.Profile {
	t.Helper()

	authenticator, err := login.NewOffline("Tester")
	if err != nil {
		t.Fatalf("NewOffline: %v", err)
	}
	negotiator, err := login.NewNegotiator(authenticator)
	if err != nil {
		t.Fatalf("NewNegotiator: %v", err)
	}

	profile, err := negotiator.Negotiate(t.Context(), client)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}

	return profile
}

func TestLoginNegotiatesCompression(t *testing.T) {
	// A low threshold so the join sequence crosses it in both directions:
	// some packets are below sixteen bytes and the chunk data is far above.
	client, _ := clientPair(t, func(settings *config.Config) {
		settings.CompressionThreshold = 16
	})

	profile := negotiateAgainstServer(t, client)
	if profile.Name.String() != "Tester" {
		t.Fatalf("profile name = %q, want %q", profile.Name, "Tester")
	}

	snapshot, err := client.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Pipeline["compression.enabled"] != "true" {
		t.Fatal("the client must have compression enabled after login")
	}
	if snapshot.Pipeline["compression.threshold"] != "16" {
		t.Fatalf("threshold = %q, want %q", snapshot.Pipeline["compression.threshold"], "16")
	}

	// Everything after login crosses under compression. Reading the join
	// sequence proves both a small packet and a large one survive the
	// envelope, because the sequence contains both.
	assertJoinSequence(t, client)
}

func TestLoginWithoutCompression(t *testing.T) {
	client, _ := clientPair(t, func(settings *config.Config) {
		settings.CompressionThreshold = -1
	})

	negotiateAgainstServer(t, client)

	snapshot, err := client.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Pipeline["compression.enabled"] != "false" {
		t.Fatal("a negative threshold must leave compression off")
	}

	assertJoinSequence(t, client)
}

// A threshold of one compresses nearly everything, which is the setting most
// likely to expose an envelope bug.
func TestLoginWithThresholdOne(t *testing.T) {
	client, _ := clientPair(t, func(settings *config.Config) {
		settings.CompressionThreshold = 1
	})

	negotiateAgainstServer(t, client)
	assertJoinSequence(t, client)
}

// assertJoinSequence reads until the chunk data arrives, which is the largest
// packet the join produces and the one a broken envelope truncates.
func assertJoinSequence(t *testing.T, client *protocol.Stream) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var sawJoin, sawChunk bool
	for range 64 {
		packet, err := client.Read(ctx)
		if err != nil {
			t.Fatalf("read play packet: %v", err)
		}

		switch value := packet.Value.(type) {
		case *v1_8.PlayClientboundLogin:
			sawJoin = true
		case *v1_8.PlayClientboundMapChunk:
			if len(value.ChunkData) == 0 {
				t.Fatal("chunk data arrived empty")
			}
			sawChunk = true
		}

		if sawJoin && sawChunk {
			return
		}
	}

	t.Fatalf("join sequence incomplete: join=%v chunk=%v", sawJoin, sawChunk)
}

// A login that the acceptor refuses must reach the client as a disconnect it
// can read, not as a dropped socket.
func TestOnlineLoginRejectionDisconnects(t *testing.T) {
	originalVerify := verifyMojang
	verifyMojang = func(context.Context, string, string) (*mojangProfile, error) {
		return nil, errRefused
	}
	t.Cleanup(func() { verifyMojang = originalVerify })

	client, _ := clientPair(t, func(settings *config.Config) {
		settings.OnlineMode = true
	})

	authenticator, err := login.NewOffline("Tester")
	if err != nil {
		t.Fatalf("NewOffline: %v", err)
	}
	negotiator, err := login.NewNegotiator(authenticator)
	if err != nil {
		t.Fatalf("NewNegotiator: %v", err)
	}

	// The offline authenticator cannot prove the account joined, so the
	// server's verifier refuses and the client is told why.
	if _, err := negotiator.Negotiate(t.Context(), client); !errors.Is(err, login.ErrLoginDisconnected) {
		t.Fatalf("error = %v, want ErrLoginDisconnected", err)
	}
}

// A kicked player is told why. Before Task 9 the socket simply closed, which a
// client can only report as a lost connection.
func TestDisconnectSendsAPlayDisconnectPacket(t *testing.T) {
	client, connection := clientPair(t, func(settings *config.Config) {
		settings.CompressionThreshold = -1
	})

	negotiateAgainstServer(t, client)

	// Read the join sequence out of the way so the disconnect is the next
	// packet the client sees rather than being queued behind chunks.
	assertJoinSequence(t, client)
	drainClient(t, client)

	const reason = "Kicked for testing"

	go connection.Disconnect(reason)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for range 256 {
		packet, err := client.Read(ctx)
		if err != nil {
			t.Fatalf("the client never received a disconnect: %v", err)
		}

		kick, ok := packet.Value.(*v1_8.PlayClientboundKickDisconnect)
		if !ok {
			continue
		}
		if !strings.Contains(kick.Reason, reason) {
			t.Fatalf("kick reason = %q, want it to carry %q", kick.Reason, reason)
		}

		return
	}

	t.Fatal("no disconnect packet arrived")
}

// drainClient reads whatever is already queued, so the next read observes a
// new packet rather than a backlog.
func drainClient(t *testing.T, client *protocol.Stream) {
	t.Helper()

	for range 512 {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		_, err := client.Read(ctx)
		cancel()

		if err != nil {
			return
		}
	}
}
