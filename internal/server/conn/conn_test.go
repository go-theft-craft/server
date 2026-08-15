package conn

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/player"
	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	mcnet "github.com/go-theft-craft/server/pkg/protocol"
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
	if configure != nil {
		configure(settings)
	}

	connection := NewConnection(
		ctx,
		serverEnd,
		settings,
		slog.New(slog.DiscardHandler),
		world.NewWorld(gen.NewFlatGenerator(0)),
		player.NewManager(8),
		nil,
		gameData,
	)

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
		id, data, err := mcnet.ReadRawPacket(h.client)
		if err != nil {
			return
		}
		h.packets <- rawPacket{id: id, data: data}
	}
}

// send writes one serverbound packet.
func (h *harness) send(packet mcnet.Packet) {
	h.t.Helper()

	if err := mcnet.WritePacket(h.client, packet); err != nil {
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
	if err := mcnet.Unmarshal(response.data, &info); err != nil {
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
	if err := mcnet.Unmarshal(response.data, &pong); err != nil {
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
	if err := mcnet.Unmarshal(success.data, &confirmed); err != nil {
		t.Fatalf("unmarshal login success: %v", err)
	}
	if confirmed.Username != "Tester" {
		t.Fatalf("username = %q, want %q", confirmed.Username, "Tester")
	}
	if want := formatUUID(offlineUUID("Tester")); confirmed.UUID != want {
		t.Fatalf("UUID = %q, want %q", confirmed.UUID, want)
	}

	// Join Game is the first play packet, and it is what proves the login
	// handed over to the play state rather than merely answering.
	join := h.expect(pkt.Login{}.PacketID())

	var joined pkt.Login
	if err := mcnet.Unmarshal(join.data, &joined); err != nil {
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
