package conn

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/internal/server/config"
	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
)

// These fixtures are the bytes the server puts on the wire today. They are
// captured before the migration so the migration has something to be measured
// against: after it, the transport is a managed stream and the packets are
// generated types, and every fixture here must still match byte for byte.
//
// Regenerate with:
//
//	go test ./internal/server/conn -run TestByteParity -update
//
// Regenerating is an assertion that the bytes were meant to change. Task 9
// changes exactly one of them on purpose — the legacy ping — and does it in
// the same commit as the code, so review sees both halves.

var updateFixtures = flag.Bool("update", false, "rewrite the byte-parity fixtures")

// fixedPublicKeyDER is the placeholder the encryption-request fixture stores
// where a real key would be. The acceptor derives the key from the server's
// keypair, which is generated per test run, so the bytes cannot be pinned; the
// harness substitutes this before comparing, which keeps the framing and the
// field order pinned instead. No private material appears in this file or in
// testdata.
var fixedPublicKeyDER = []byte{
	0x30, 0x82, 0x01, 0x22, 0x30, 0x0d, 0x06, 0x09,
	0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x01,
}

// fixedVerifyToken is what the captured encryption request carries. The real
// token is four random bytes, so the harness substitutes this one to keep the
// fixture stable; the randomness itself is asserted separately.
var fixedVerifyToken = []byte{0xde, 0xad, 0xbe, 0xef}

func fixturePath(name string) string {
	return filepath.Join("testdata", name+".bin")
}

// assertFixture compares captured bytes with the recorded ones.
func assertFixture(t *testing.T, name string, got []byte) {
	t.Helper()

	path := fixturePath(name)

	if *updateFixtures {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to record it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s changed:\n got %x\nwant %x", name, got, want)
	}
}

// encode renders one packet exactly as the server writes it, length prefix
// and all. It goes through the same writer the connection uses, so a change
// in framing shows up in every fixture rather than in none of them.
func encode(t *testing.T, packet java.PacketValue) []byte {
	t.Helper()

	payload, err := java.Marshal(packet, testLimits())
	if err != nil {
		t.Fatalf("marshal %T: %v", packet, err)
	}

	var buf bytes.Buffer
	if err := java.WriteRawPacket(&buf, testLimits(), protocol.Packet{
		ID:      packet.PacketID(),
		Payload: payload,
	}); err != nil {
		t.Fatalf("encode %T: %v", packet, err)
	}

	return buf.Bytes()
}

// framed reassembles a packet the harness read, so a captured packet and an
// encoded one are directly comparable.
func framed(t *testing.T, packet rawPacket) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := java.WriteRawPacket(&buf, testLimits(), protocol.Packet{
		ID:      packet.id,
		Payload: packet.data,
	}); err != nil {
		t.Fatalf("reframe packet %#x: %v", packet.id, err)
	}

	return buf.Bytes()
}

func TestByteParityStatusAndPing(t *testing.T) {
	h := newHarness(t)

	h.handshake(1)
	h.send(&pkt.PingStart{})
	assertFixture(t, "status_response", framed(t, h.expect(pkt.ServerInfo{}.PacketID())))

	h.send(&pkt.PingSB{Time: 0x0123456789abcdef})
	assertFixture(t, "ping_response", framed(t, h.expect(pkt.PingCB{}.PacketID())))
}

func TestByteParityLoginSuccess(t *testing.T) {
	h := newHarness(t)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})
	assertFixture(t, "login_success", framed(t, h.expect(pkt.Success{}.PacketID())))
}

// The encryption request is captured from a real online-mode login, with the
// two values that cannot be stable — the public key and the verify token —
// pinned by the harness rather than left to chance.
func TestByteParityEncryptionRequest(t *testing.T) {
	h := newOnlineHarness(t, nil)

	h.handshake(2)
	h.send(&pkt.LoginStart{Username: "Tester"})

	request := h.expect(pkt.EncryptionBeginCB{}.PacketID())

	var decoded pkt.EncryptionBeginCB
	if err := java.Unmarshal(request.data, &decoded, testLimits()); err != nil {
		t.Fatalf("unmarshal encryption request: %v", err)
	}
	if decoded.ServerID != "" {
		t.Fatalf("server ID = %q, want empty", decoded.ServerID)
	}
	// The key on the wire has to be the server's own, and it has to be
	// readable as one. Both halves matter: a request carrying a malformed key
	// reaches a client that cannot answer it.
	parsed, err := java.ParseServerPublicKey(decoded.PublicKey)
	if err != nil {
		t.Fatalf("the encryption request must carry a parseable public key: %v", err)
	}
	if !parsed.Equal(&testServerKey().PublicKey) {
		t.Fatal("the encryption request must carry the server's own public key")
	}
	if len(decoded.VerifyToken) != 4 {
		t.Fatalf("verify token is %d bytes, want 4", len(decoded.VerifyToken))
	}

	// Neither the key nor the token can be pinned: one is generated per test
	// run and the other per connection. Substituting both keeps the framing
	// and the field order pinned, which is what the migration can break.
	decoded.PublicKey = fixedPublicKeyDER
	decoded.VerifyToken = fixedVerifyToken
	assertFixture(t, "encryption_request", encode(t, &decoded))
}

// Two connections must not receive the same verify token. It is the one field
// the fixture deliberately does not pin, so it is asserted here instead.
func TestVerifyTokenIsFreshPerConnection(t *testing.T) {
	tokens := make([][]byte, 0, 2)

	for range 2 {
		h := newOnlineHarness(t, nil)
		h.handshake(2)
		h.send(&pkt.LoginStart{Username: "Tester"})

		var decoded pkt.EncryptionBeginCB
		request := h.expect(pkt.EncryptionBeginCB{}.PacketID())
		if err := java.Unmarshal(request.data, &decoded, testLimits()); err != nil {
			t.Fatalf("unmarshal encryption request: %v", err)
		}
		tokens = append(tokens, decoded.VerifyToken)
	}

	if bytes.Equal(tokens[0], tokens[1]) {
		t.Fatal("two connections received the same verify token")
	}
}

// A login-state disconnect is what a failed online login produces today. It
// is the form Task 9 must keep sending when a login is refused.
func TestByteParityLoginDisconnect(t *testing.T) {
	assertFixture(t, "login_disconnect", encode(t, &pkt.Disconnect{
		Reason: `{"text":"Failed to verify with Mojang."}`,
	}))
}

// The play-state form is the other one. It is only reachable today after a
// thirty-second keep-alive timeout, so the fixture pins its encoding rather
// than driving the timeout, and Task 9 asserts that a kicked player receives
// exactly these bytes.
func TestByteParityPlayDisconnect(t *testing.T) {
	assertFixture(t, "play_disconnect", encode(t, &pkt.KickDisconnect{
		Reason: `{"text":"Timed out"}`,
	}))
}

// errRefused is what the stubbed session server answers when it rejects.
var errRefused = errors.New("account did not join")

// newOnlineHarness is newHarness with online mode on and the session-server
// call stubbed. verify is what the session server would have answered; nil
// means it refuses, which is the path that produces the login disconnect.
//
// The key is the package's own test key, generated once per run and never
// written to disk.
func newOnlineHarness(t *testing.T, verify *mojangProfile) *harness {
	t.Helper()

	originalVerify := verifyMojang
	verifyMojang = func(context.Context, string, string) (*mojangProfile, error) {
		if verify == nil {
			return nil, errRefused
		}

		return verify, nil
	}
	t.Cleanup(func() { verifyMojang = originalVerify })

	return newHarnessWith(t, func(settings *config.Config) {
		settings.OnlineMode = true
	})
}
