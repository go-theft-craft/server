package conn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/login"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// This file describes what the server does today, before the migration moves
// any of it. It exists so the migration can prove it changed the transport and
// nothing else. It writes no production code and asserts no behavior the
// server does not already have.

// harness is one connection under test, driven by a scripted client over an
// in-memory pipe.
type harness struct {
	t      *testing.T
	client net.Conn
	conn   *Connection

	packets chan rawPacket
	done    chan struct{}
}

// rawPacket is what the client read off the wire: the packet ID and the body
// after it, with the length prefix already consumed.
type rawPacket struct {
	id   int32
	data []byte
}

// newHarness starts a real Connection over net.Pipe and drains everything it
// writes into a channel.
//
// The drain has to run continuously. net.Pipe is unbuffered, so a server write
// blocks until somebody reads it, and a test that reads only when it expects
// something would deadlock the moment the server sends more than it expects.
func newHarness(t *testing.T) *harness {
	t.Helper()

	return newHarnessWith(t, nil)
}

// newHarnessWith builds a harness whose configuration is adjusted before the
// connection starts. Adjusting it afterwards would race the Handle goroutine.
func newHarnessWith(t *testing.T, configure func(*config.Config)) *harness {
	t.Helper()

	// No login in this file reaches the network. The production path calls
	// Mojang for a skin even in offline mode, and a test that let it would be
	// slow when the network is present and slower when it is not.
	original := fetchSkin
	fetchSkin = func(context.Context, string) ([]mojangProperty, error) { return nil, nil }
	t.Cleanup(func() { fetchSkin = original })

	clientEnd, serverEnd := net.Pipe()

	gameData, err := v1_8.Data()
	if err != nil {
		t.Fatalf("load game data: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	settings := config.DefaultConfig()
	// Every login builds an acceptor, and an acceptor needs the server's key
	// even offline, where it never puts the public half on the wire.
	settings.PrivateKey = testServerKey()
	// This harness reads frames directly, so it cannot follow a compression
	// envelope. Tests that care about compression drive a real client stream
	// instead; see TestLoginNegotiatesCompression.
	settings.CompressionThreshold = -1
	if configure != nil {
		configure(settings)
	}

	connection, err := NewConnection(
		ctx,
		serverEnd,
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

	h := &harness{
		t:       t,
		client:  clientEnd,
		conn:    connection,
		packets: make(chan rawPacket, 256),
		done:    make(chan struct{}),
	}

	go func() {
		connection.Handle()
		close(h.done)
	}()

	go h.drain()

	// Registered last, so it runs first: the connection has to be finished
	// before the seam restores above run, or the cleanup writes a package
	// variable the Handle goroutine is still reading.
	t.Cleanup(func() {
		_ = clientEnd.Close()
		cancel()

		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Error("the connection did not shut down")
		}
	})

	return h
}

// drain reads packets until the connection closes.
func (h *harness) drain() {
	defer close(h.packets)

	for {
		packet, err := java.ReadRawPacket(h.client, testLimits())
		if err != nil {
			return
		}
		h.packets <- rawPacket{id: packet.ID, data: packet.Payload}
	}
}

// send writes one serverbound packet.
func (h *harness) send(packet java.PacketValue) {
	h.t.Helper()

	payload, err := java.Marshal(packet, testLimits())
	if err != nil {
		h.t.Fatalf("marshal %T: %v", packet, err)
	}
	if err := java.WriteRawPacket(h.client, testLimits(), protocol.Packet{
		ID:      packet.PacketID(),
		Payload: payload,
	}); err != nil {
		h.t.Fatalf("write %T: %v", packet, err)
	}
}

// sendRaw writes bytes straight onto the wire, framing and all. It is how the
// legacy-ping case sends something that is not a packet at all.
func (h *harness) sendRaw(payload []byte) {
	h.t.Helper()

	if _, err := h.client.Write(payload); err != nil {
		h.t.Fatalf("write raw bytes: %v", err)
	}
}

// expect waits for the next packet the server writes.
func (h *harness) expect(id int32) rawPacket {
	h.t.Helper()

	select {
	case packet, ok := <-h.packets:
		if !ok {
			h.t.Fatalf("connection closed while waiting for packet %#x", id)
		}
		if packet.id != id {
			h.t.Fatalf("received packet %#x, want %#x", packet.id, id)
		}

		return packet
	case <-time.After(5 * time.Second):
		h.t.Fatalf("timed out waiting for packet %#x", id)

		return rawPacket{}
	}
}

// expectClosed asserts that the connection ended rather than answering.
func (h *harness) expectClosed() {
	h.t.Helper()

	select {
	case packet, ok := <-h.packets:
		if ok {
			h.t.Fatalf("expected the connection to close, received packet %#x", packet.id)
		}
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for the connection to close")
	}
}

// handshake sends the packet that puts the connection into status or login.
func (h *harness) handshake(nextState int32) {
	h.t.Helper()

	h.send(&pkt.SetProtocol{
		ProtocolVersion: pkt.ProtocolVersion,
		ServerHost:      "localhost",
		ServerPort:      25565,
		NextState:       nextState,
	})
}

func TestStatusRequestAnswersWithTheServerDescription(t *testing.T) {
	h := newHarness(t)

	h.handshake(1)
	h.send(&pkt.PingStart{})

	response := h.expect(pkt.ServerInfo{}.PacketID())

	var info pkt.ServerInfo
	if err := java.Unmarshal(response.data, &info, testLimits()); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}

	var described struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal([]byte(info.Response), &described); err != nil {
		t.Fatalf("decode status JSON: %v", err)
	}

	defaults := config.DefaultConfig()
	if described.Version.Protocol != int(pkt.ProtocolVersion) {
		t.Fatalf("protocol = %d, want %d", described.Version.Protocol, pkt.ProtocolVersion)
	}
	if described.Version.Name != pkt.VersionName {
		t.Fatalf("version name = %q, want %q", described.Version.Name, pkt.VersionName)
	}
	if described.Players.Max != defaults.MaxPlayers {
		t.Fatalf("max players = %d, want %d", described.Players.Max, defaults.MaxPlayers)
	}
	if described.Players.Online != 0 {
		t.Fatalf("online players = %d, want 0", described.Players.Online)
	}
	if described.Description.Text != defaults.MOTD {
		t.Fatalf("MOTD = %q, want %q", described.Description.Text, defaults.MOTD)
	}
}

func TestPingEchoesItsPayload(t *testing.T) {
	h := newHarness(t)

	h.handshake(1)
	h.send(&pkt.PingStart{})
	h.expect(pkt.ServerInfo{}.PacketID())

	const sent = int64(0x0123456789abcdef)
	h.send(&pkt.PingSB{Time: sent})

	response := h.expect(pkt.PingCB{}.PacketID())

	var pong pkt.PingCB
	if err := java.Unmarshal(response.data, &pong, testLimits()); err != nil {
		t.Fatalf("unmarshal ping response: %v", err)
	}
	if pong.Time != sent {
		t.Fatalf("ping echoed %#x, want %#x", pong.Time, sent)
	}
}

// The legacy FE 01 probe is not a packet. Recording what the server does with
// it today matters because Task 9 changes it deliberately, and the change has
// to be visible as an edit to this expectation.
func TestLegacyPingIsNotAnsweredToday(t *testing.T) {
	h := newHarness(t)

	// 0xFE 0x01 is what a 1.6 client sends before any handshake. Read as a
	// VarInt length it announces 254 bytes that never arrive.
	h.sendRaw([]byte{0xFE, 0x01})
	_ = h.client.Close()

	h.expectClosed()
}

func TestOfflineLoginReachesPlay(t *testing.T) {
	h := newHarness(t)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})

	success := h.expect(pkt.Success{}.PacketID())

	var confirmed pkt.Success
	if err := java.Unmarshal(success.data, &confirmed, testLimits()); err != nil {
		t.Fatalf("unmarshal login success: %v", err)
	}
	if confirmed.Username != "Tester" {
		t.Fatalf("username = %q, want %q", confirmed.Username, "Tester")
	}
	if want := login.OfflineUUID(testerName(t)).String(); confirmed.UUID != want {
		t.Fatalf("UUID = %q, want %q", confirmed.UUID, want)
	}

	// Join Game is the first play packet, and it is what proves the login
	// handed over to the play state rather than merely answering.
	join := h.expect(pkt.Login{}.PacketID())

	var joined pkt.Login
	if err := java.Unmarshal(join.data, &joined, testLimits()); err != nil {
		t.Fatalf("unmarshal join game: %v", err)
	}
	if joined.LevelType == "" {
		t.Fatal("join game carries no level type")
	}
}

func TestHandshakeRejectsAnInvalidNextState(t *testing.T) {
	h := newHarness(t)

	h.handshake(7)
	h.expectClosed()
}

// A status request before any handshake is refused, because the connection is
// still in the handshake state and only one packet is allowed there.
func TestStatusRequestBeforeHandshakeIsRefused(t *testing.T) {
	h := newHarness(t)

	h.send(&pkt.PingStart{})
	h.expectClosed()
}

func TestClosingTheClientEndsTheConnection(t *testing.T) {
	h := newHarness(t)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})
	h.expect(pkt.Success{}.PacketID())
	h.expect(pkt.Login{}.PacketID())

	if err := h.client.Close(); err != nil && err != io.ErrClosedPipe {
		t.Fatalf("close client: %v", err)
	}

	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Handle did not return after the client disconnected")
	}
}

// The stream replaced the blocking read loop in Task 5. These describe what
// the transport now guarantees that the old io.ReadWriter did not.

func TestOversizedFrameIsRefusedByName(t *testing.T) {
	h := newHarness(t)

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	// A length prefix past the frame limit, with none of the body behind it.
	// The stream must refuse it on the prefix alone rather than allocating.
	var prefix bytes.Buffer
	if _, err := java.WriteVarInt(&prefix, int32(limits.FrameBytes())+1); err != nil {
		t.Fatalf("encode length prefix: %v", err)
	}
	h.sendRaw(prefix.Bytes())

	h.expectClosed()

	if err := h.conn.stream.Wait(); !errors.Is(err, java.ErrFrameTooLarge) {
		t.Fatalf("stream error = %v, want ErrFrameTooLarge", err)
	}
}

// writePacket no longer takes a lock; the stream's write pump serializes.
// Two writers must still produce two intact frames rather than interleaved
// halves of both.
func TestConcurrentWritesProduceIntactFrames(t *testing.T) {
	h := newHarness(t)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})
	h.expect(pkt.Success{}.PacketID())
	h.expect(pkt.Login{}.PacketID())

	// Drain the rest of the join sequence so the two writes below are the
	// only ones left to observe.
	drainUntilQuiet(t, h)

	const writers = 8

	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for index := range writers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errs <- h.conn.writePacket(&pkt.ChatCB{
				Message:  fmt.Sprintf(`{"text":"writer %d"}`, index),
				Position: 0,
			})
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}

	// Every frame must decode cleanly. An interleaved write would produce a
	// body that is not a valid chat packet.
	seen := make(map[string]bool, writers)
	for range writers {
		packet := h.expect(pkt.ChatCB{}.PacketID())

		var chat pkt.ChatCB
		if err := java.Unmarshal(packet.data, &chat, testLimits()); err != nil {
			t.Fatalf("decode concurrent write: %v", err)
		}
		seen[chat.Message] = true
	}

	if len(seen) != writers {
		t.Fatalf("received %d distinct messages, want %d", len(seen), writers)
	}
}

// drainUntilQuiet consumes packets until none arrives for a short while.
func drainUntilQuiet(t *testing.T, h *harness) {
	t.Helper()

	for {
		select {
		case _, ok := <-h.packets:
			if !ok {
				t.Fatal("connection closed while draining")
			}
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}

// Task 6 moved handshake and status onto generated packets. The session now
// owns the transition: it commits the handshake's next state before the
// connection ever sees the packet.
func TestHandshakeTransitionsTheSession(t *testing.T) {
	cases := []struct {
		name      string
		nextState int32
		want      protocol.State
	}{
		{name: "status", nextState: 1, want: v1_8.StateStatus},
		{name: "login", nextState: 2, want: v1_8.StateLogin},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newHarness(t)
			h.handshake(testCase.nextState)

			// Drive one more exchange so the handshake is known to have been
			// processed before the state is read.
			if testCase.nextState == 1 {
				h.send(&pkt.PingStart{})
				h.expect(pkt.ServerInfo{}.PacketID())
			} else {
				h.send(&pkt.LoginStart{Username: "Tester"})
				h.expect(pkt.Success{}.PacketID())
				// Join Game is written in the play state, so receiving it
				// means the session has finished moving there.
				h.expect(pkt.Login{}.PacketID())
			}

			snapshot, err := h.conn.stream.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}

			// A login carries straight on into play. Task 7 puts the login
			// packets themselves onto generated values, at which point the
			// session proposes that move too rather than being told.
			want := testCase.want
			if testCase.nextState == 2 {
				want = v1_8.StatePlay
			}
			if snapshot.State != want {
				t.Fatalf("session state = %q, want %q", snapshot.State, want)
			}
		})
	}
}

// testerName is the parsed form of the name every login test uses.
func testerName(t *testing.T) java.Username {
	t.Helper()

	name, err := java.ParseUsername("Tester")
	if err != nil {
		t.Fatalf("ParseUsername: %v", err)
	}

	return name
}

// testServerKey is generated once for the whole package. Key generation is the
// slowest thing a login test does, and every test can share one.
var testServerKey = sync.OnceValue(func() *rsa.PrivateKey {
	// 1024 bits, as Java Edition uses for this exchange.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic("generate test server key: " + err.Error())
	}

	return key
})

// testLimits is the default limit set, shared by every test in the package.
var testLimits = sync.OnceValue(func() protocol.Limits {
	limits, err := protocol.NewLimits()
	if err != nil {
		panic("build test limits: " + err.Error())
	}

	return limits
})

// Task 8 moved play decoding onto the shared reflect codec. The local structs
// and their mc tags did not change, so the same bytes must produce the same
// struct, and the failure modes must be the ones the old loop had.

func TestPlayPacketDecodesIntoTheSameStruct(t *testing.T) {
	sent := &pkt.PositionSB{X: 1.5, Y: 64, Z: -2.25, OnGround: true}

	payload, err := java.Marshal(sent, testLimits())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var received pkt.PositionSB
	if err := java.Unmarshal(payload, &received, testLimits()); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if received != *sent {
		t.Fatalf("round trip changed the packet: %+v, want %+v", received, *sent)
	}
}

// A truncated packet names what failed rather than panicking.
func TestTruncatedPlayPacketIsAnError(t *testing.T) {
	payload, err := java.Marshal(&pkt.PositionSB{X: 1, Y: 2, Z: 3}, testLimits())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var received pkt.PositionSB
	err = java.Unmarshal(payload[:len(payload)-4], &received, testLimits())
	if err == nil {
		t.Fatal("a truncated payload decoded without error")
	}
}

// An unknown play packet ID is ignored, exactly as it was before: the switch
// has no case for it and the connection stays up.
func TestUnknownPlayPacketIsIgnored(t *testing.T) {
	h := newHarness(t)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})
	h.expect(pkt.Success{}.PacketID())
	h.expect(pkt.Login{}.PacketID())
	drainUntilQuiet(t, h)

	// 0x7F is serverbound-play in no version of protocol 47.
	if err := java.WriteRawPacket(h.client, testLimits(), protocol.Packet{
		ID:      0x7F,
		Payload: []byte{0x01, 0x02},
	}); err != nil {
		t.Fatalf("write unknown packet: %v", err)
	}

	// The connection must still answer afterwards.
	if err := h.conn.writePacket(&pkt.ChatCB{Message: `{"text":"still here"}`, Position: 0}); err != nil {
		t.Fatalf("write after unknown packet: %v", err)
	}
	h.expect(pkt.ChatCB{}.PacketID())
}
