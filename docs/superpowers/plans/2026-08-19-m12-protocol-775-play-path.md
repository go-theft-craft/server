# Protocol 775 Play Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve a protocol 775 client everything this server already serves a protocol 47 client — join, world, movement, inventory, crafting, mining, combat, persistence — from one version-neutral play path with two adapters under it.

**Architecture:** The version boundary moves to where `world.Adapter` already put it for blocks. A `conn.Dialect` turns a decoded generated packet into a version-neutral action and turns a version-neutral message into a generated packet; `handler_play.go`, `inventory.go`, and `player.Manager` stop naming `v1_8` and name the dialect instead. `v47` implements it against the existing types, byte-for-byte, and `v775` implements it against `v26_1`. Underneath, `pkg/world/v775` renders a column the way `pkg/world/v47` does, on a paletted-container encoder that lands in `minecraft-protocol` beside the decoder that already reads one.

**Tech Stack:** Go 1.26.6 via devbox; `minecraft-protocol` generated `v1_8` and `v26_1` packages; `wire/java/chunk`; `login.Acceptor` with `WithConfiguration`; Task; golangci-lint; the pinned Node interop lane; `headless-minecraft`'s conformance runner.

## Global Constraints

- **This repository owns no wire code.** Every packet is a generated `minecraft-protocol` type, and a layout that is not a packet — a chunk column, a paletted container — belongs in `minecraft-protocol/wire/java`, not here. Stage A and Stage B are executed in that repository and released before Stage C consumes them.
- **Never widen a limit or relax a decode check to make a test pass.** A generated codec that rejects a real client's packet is a bug in `minecraft-protocol`: fix it there, add a byte fixture, re-run.
- **The byte-parity fixtures in `internal/server/conn/testdata` are the contract.** Every stage here runs them unchanged. Regenerating one with `-update` asserts the bytes were meant to change and belongs in the same commit as the code that changed them. Stage C changes no bytes at all, and its fixtures passing untouched is the whole gate.
- **Both repositories are public and permanently published.** A commit here is served by `proxy.golang.org` and hashed into `sum.golang.org`; treat it as unrecallable. Do not name the private proxy project, its protocol, or its codename anywhere in this work.
- `task verify` (fmt, lint, secrets, test, race, interop, vuln, build) is green at the end of every task that touches code.
- Go 1.26.6 from the `openserbia/go-flake` pins. Always `devbox run -- task <name>`; never a bare `go build` or `golangci-lint`.
- Import order is `gci`'s: stdlib → third-party → `github.com/openserbia` → project module. Formatting is `gofumpt`.
- The core module's dependency list does not grow. Anything needing a new dependency belongs in `examples/`, which is its own module.

---

## Stage map

Each stage is an independently testable deliverable and ends at a commit. Stages A and B are `minecraft-protocol` changes with their own release; C onward are this repository.

| Stage | Repository | Deliverable | Gate |
| --- | --- | --- | --- |
| A | `minecraft-protocol` | `Encode775`: paletted containers and column blobs | Re-encoding the captured 26.1 column decodes to the values it decoded to |
| B | `minecraft-protocol` | `SessionControl`: swap the session at the handshake boundary | A started stream decodes its next frame with the session installed after it started, and refuses the swap once the state has moved |
| C | `server` | `conn.Dialect`, implemented once for protocol 47 | Byte-parity fixtures and the Node interop lane pass **unchanged** |
| D | `server` | `pkg/world/v775` world adapter | A column this adapter writes decodes back to the blocks it was given |
| E | `server` | Configuration state and the 775 join | A 775 client reaches play and receives a world |
| F | `server` | Movement, keep-alive, chat, and the brigadier tree at 775 | The owned-server matrix row runs at 775 |
| G | `server` | Inventory, windows, crafting, chests at 775 | The window scenarios run at both versions |
| H | `server` | Mining, placement, damage, combat, persistence, and the lanes | Both versions through every conformance lane |

Stages E through H are scoped at the end of this document rather than written as tasks. Their task-level plans are written when the stage before them lands, because each one's interfaces are decided by its predecessor and a plan written against guessed interfaces is a plan that gets rewritten. **Do not treat their absence as permission to improvise: write the plan, get it reviewed, then implement.**

---

## Stage A — the protocol 775 column encoder

Executed in `../minecraft-protocol`. `wire/java/chunk` reads a 775 column and cannot write one, so nothing in this project can serve a 775 chunk.

### Task 1: Paletted container encoding

**Files:**
- Modify: `wire/java/chunk/protocol775.go`
- Modify: `wire/java/chunk/protocol775_test.go`

**Interfaces:**
- Consumes: `decodeContainer`, `longsFor`, `BlocksPerSection`, `BiomesPerSection775`, `MaxBlockPaletteBits775`, `MaxBiomePaletteBits775`, all already in the file.
- Produces:
  - `type SectionValues struct { Blocks []uint32; Biomes []uint32; BlockCount int16; FluidCount int16 }`
  - `type Global775 struct { BlockBits int; BiomeBits int }`
  - `func EncodeSection775(dst []byte, section SectionValues, global Global775) ([]byte, error)`
  - `func EncodeColumn775(sections []SectionValues, global Global775) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

Add to `wire/java/chunk/protocol775_test.go`:

```go
func TestEncodeSection775RoundTripsASingleValuedContainer(t *testing.T) {
	blocks := make([]uint32, BlocksPerSection)
	biomes := make([]uint32, BiomesPerSection775)

	raw, err := EncodeSection775(nil, SectionValues{
		Blocks: blocks, Biomes: biomes,
	}, Global775{BlockBits: 15, BiomeBits: 7})
	if err != nil {
		t.Fatalf("EncodeSection775: %v", err)
	}

	// A section of one block value and one biome value is two headers and
	// two VarInts: four bytes plus the two counts.
	if len(raw) != 4+4 {
		t.Fatalf("encoded %d bytes, want 8: % x", len(raw), raw)
	}

	sections, err := Split775(raw, 0)
	if err != nil {
		t.Fatalf("Split775: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("split into %d sections, want 1", len(sections))
	}
	got, err := DecodeSection775(sections[0].Blocks)
	if err != nil {
		t.Fatalf("DecodeSection775: %v", err)
	}
	if !slices.Equal(got, blocks) {
		t.Fatal("a section of air did not decode back to air")
	}
}

func TestEncodeSection775DeclaresFourBitsForANarrowBlockPalette(t *testing.T) {
	// Two distinct block states need one bit. Vanilla's block-state strategy
	// has a four-bit floor and a client unpacks at four however few the
	// palette needs, so declaring one silently misreads every entry.
	blocks := make([]uint32, BlocksPerSection)
	for i := range blocks {
		if i%2 == 0 {
			blocks[i] = 9
		}
	}
	raw, err := EncodeSection775(nil, SectionValues{
		Blocks: blocks, Biomes: make([]uint32, BiomesPerSection775),
	}, Global775{BlockBits: 15, BiomeBits: 7})
	if err != nil {
		t.Fatalf("EncodeSection775: %v", err)
	}

	// byte 0 and 1 are the block count, 2 and 3 the fluid count, 4 the width.
	if raw[4] != 4 {
		t.Fatalf("declared %d bits an entry for a two-entry palette, want 4", raw[4])
	}

	sections, err := Split775(raw, 0)
	if err != nil {
		t.Fatalf("Split775: %v", err)
	}
	got, err := DecodeSection775(sections[0].Blocks)
	if err != nil {
		t.Fatalf("DecodeSection775: %v", err)
	}
	if !slices.Equal(got, blocks) {
		t.Fatal("a two-value section did not round-trip")
	}
}

func TestEncodeSection775UsesTheGlobalPaletteWhenTheIndirectOneWouldOverflow(t *testing.T) {
	blocks := make([]uint32, BlocksPerSection)
	for i := range blocks {
		blocks[i] = uint32(i) // 4096 distinct values: far past eight bits
	}
	raw, err := EncodeSection775(nil, SectionValues{
		Blocks: blocks, Biomes: make([]uint32, BiomesPerSection775),
	}, Global775{BlockBits: 15, BiomeBits: 7})
	if err != nil {
		t.Fatalf("EncodeSection775: %v", err)
	}
	if raw[4] != 15 {
		t.Fatalf("declared %d bits an entry, want the global width 15", raw[4])
	}

	sections, err := Split775(raw, 0)
	if err != nil {
		t.Fatalf("Split775: %v", err)
	}
	got, err := DecodeSection775(sections[0].Blocks)
	if err != nil {
		t.Fatalf("DecodeSection775: %v", err)
	}
	if !slices.Equal(got, blocks) {
		t.Fatal("a global-palette section did not round-trip")
	}
}
```

Add `"slices"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `devbox run -- go test ./wire/java/chunk -run TestEncodeSection775 -v`
Expected: FAIL, `undefined: EncodeSection775`.

- [ ] **Step 3: Write the implementation**

Append to `wire/java/chunk/protocol775.go`:

```go
// minBlockPaletteBits775 is the narrowest block-state container vanilla will
// read as it was written.
//
// Vanilla picks a palette configuration from the width it read, and its
// block-state strategy maps one, two, three, and four all to a four-bit linear
// palette. It then unpacks with the configuration's width rather than with the
// width on the wire, so a container that declares three bits is read at four:
// every entry after the first is wrong, and nothing reports an error. Biomes
// have no such floor -- their strategy maps one, two, and three to themselves.
//
// This package's own decoder honours the declared width literally, which is
// what a reader should do; the floor exists for the client, not for it.
const minBlockPaletteBits775 = 4

// SectionValues is one section's contents as values, ready to be encoded.
//
// Blocks is one entry per block, indexed y*256 + z*16 + x, and Biomes one per
// biome cell, indexed y*16 + z*4 + x. Both hold registry IDs -- the same
// numbers DecodeSection775 and DecodeBiomes775 return.
type SectionValues struct {
	Blocks     []uint32
	Biomes     []uint32
	BlockCount int16
	FluidCount int16
}

// Global775 is how wide the global palette is, for each of the two containers.
//
// It is a parameter rather than a constant because the width is a property of
// the game version's registries -- the number of block states and the number
// of biomes it defines -- and this package owns wire layout rather than
// registry contents. A caller computes it as the bit length of the highest ID
// its data set defines. Getting it wrong is not detectable here: a client
// unpacks a global container at the width its own registry implies, so a
// disagreement reads as wrong blocks rather than as an error.
type Global775 struct {
	BlockBits int
	BiomeBits int
}

// EncodeSection775 appends one section to dst and returns the extended slice.
func EncodeSection775(dst []byte, section SectionValues, global Global775) ([]byte, error) {
	if len(section.Blocks) != BlocksPerSection {
		return nil, fmt.Errorf(
			"%w: %d blocks, want %d", ErrSection, len(section.Blocks), BlocksPerSection,
		)
	}
	if len(section.Biomes) != BiomesPerSection775 {
		return nil, fmt.Errorf(
			"%w: %d biomes, want %d", ErrSection, len(section.Biomes), BiomesPerSection775,
		)
	}

	dst = binary.BigEndian.AppendUint16(dst, uint16(section.BlockCount))
	dst = binary.BigEndian.AppendUint16(dst, uint16(section.FluidCount))

	dst, err := encodeContainer(
		dst, section.Blocks, minBlockPaletteBits775, MaxBlockPaletteBits775, global.BlockBits,
	)
	if err != nil {
		return nil, err
	}

	return encodeContainer(dst, section.Biomes, 1, MaxBiomePaletteBits775, global.BiomeBits)
}

// EncodeColumn775 writes a whole column: every section, bottom-most first.
//
// The blob does not say where the column starts, so the caller and the client
// agree on that through the dimension_type registry rather than through these
// bytes. Split775 takes the same value as its bottom argument.
func EncodeColumn775(sections []SectionValues, global Global775) ([]byte, error) {
	if len(sections) > sectionsPerColumnLimit {
		return nil, fmt.Errorf(
			"%w: %d sections, more than %d", ErrColumn, len(sections), sectionsPerColumnLimit,
		)
	}

	var out []byte
	var err error
	for index, section := range sections {
		out, err = EncodeSection775(out, section, global)
		if err != nil {
			return nil, fmt.Errorf("section %d: %w", index, err)
		}
	}

	return out, nil
}

// encodeContainer writes one paletted container: a width, the palette that
// width implies, and the packed entries.
func encodeContainer(
	dst []byte, values []uint32, minBits, maxPaletteBits, globalBits int,
) ([]byte, error) {
	palette, index := paletteOf(values)

	switch {
	case len(palette) == 1:
		// One value and nothing to distinguish: a width of zero, the value,
		// and no long array at all.
		dst = append(dst, 0)

		return appendVarInt(dst, int32(palette[0])), nil

	case bitsFor(len(palette)-1) <= maxPaletteBits:
		bits := max(bitsFor(len(palette)-1), minBits)
		dst = append(dst, byte(bits))
		dst = appendVarInt(dst, int32(len(palette)))
		for _, value := range palette {
			dst = appendVarInt(dst, int32(value))
		}
		packed := make([]uint32, len(values))
		for i, value := range values {
			packed[i] = index[value]
		}

		return appendPacked(dst, packed, bits), nil

	default:
		if globalBits <= 0 || globalBits > maxPackedBits {
			return nil, fmt.Errorf(
				"%w: a global palette of %d bits", ErrSection, globalBits,
			)
		}
		dst = append(dst, byte(globalBits))

		return appendPacked(dst, values, globalBits), nil
	}
}

// paletteOf returns the distinct values in the order they first appear, and
// the map back from a value to its index.
//
// First-seen order rather than sorted order, because it is what vanilla's
// linear palette produces and a fixture regenerated by a different ordering
// would show a diff that means nothing.
func paletteOf(values []uint32) ([]uint32, map[uint32]uint32) {
	palette := make([]uint32, 0, 16)
	index := make(map[uint32]uint32, 16)
	for _, value := range values {
		if _, seen := index[value]; seen {
			continue
		}
		index[value] = uint32(len(palette))
		palette = append(palette, value)
	}

	return palette, index
}

// appendPacked writes the long array. Entries are packed whole into each long
// and never straddle one, so a long holds 64/bits of them and the remainder
// costs a whole long -- which is what longsFor counts.
func appendPacked(dst []byte, values []uint32, bits int) []byte {
	perLong := 64 / bits
	longs := longsFor(len(values), perLong)
	for cell := range longs {
		var word uint64
		for slot := range perLong {
			i := cell*perLong + slot
			if i >= len(values) {
				break
			}
			word |= uint64(values[i]) << (slot * bits)
		}
		dst = binary.BigEndian.AppendUint64(dst, word)
	}

	return dst
}

// bitsFor is how many bits the value needs. Zero needs one: a palette of one
// entry is handled before this is called, and a palette of two indexes 0 and 1.
func bitsFor(highest int) int {
	if highest <= 0 {
		return 1
	}

	return bits.Len(uint(highest))
}

func appendVarInt(dst []byte, value int32) []byte {
	var buffer [5]byte

	return append(dst, buffer[:java.PutVarInt(buffer[:], value)]...)
}
```

Add `"math/bits"` and `"github.com/go-theft-craft/minecraft-protocol/wire/java"` to the file's imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `devbox run -- go test ./wire/java/chunk -run TestEncodeSection775 -v`
Expected: PASS, all three.

- [ ] **Step 5: Commit**

```bash
devbox run -- task precommit
git add wire/java/chunk/protocol775.go wire/java/chunk/protocol775_test.go
git commit -m "feat(chunk): encode a protocol 775 paletted container"
```

### Task 2: Round-trip the captured column, and release

**Files:**
- Modify: `wire/java/chunk/protocol775_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `EncodeColumn775`, `Split775`, `DecodeSection775`, `DecodeBiomes775`, and `testdata/chunk-26.1-0-0.bin`, the column captured from a real Paper 26.1 server.
- Produces: no new API.

- [ ] **Step 1: Write the failing test**

```go
// TestEncodeColumn775RoundTripsACapturedColumn is the test that matters. The
// fixture is a column a real server wrote, so decoding it, re-encoding it, and
// decoding that again asks whether this encoder produces something a client
// would read the way the server meant.
//
// It compares values rather than bytes on purpose. Vanilla chooses a palette
// strategy per section from its own history -- a section it edited keeps a
// wider palette than its contents now need -- so byte equality would assert
// something about the server that wrote the fixture rather than about this
// encoder.
func TestEncodeColumn775RoundTripsACapturedColumn(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "chunk-26.1-0-0.bin"))
	if err != nil {
		t.Fatal(err)
	}

	const bottom = -4
	original, err := Split775(raw, bottom)
	if err != nil {
		t.Fatalf("Split775: %v", err)
	}

	values := make([]SectionValues, len(original))
	for i, section := range original {
		blocks, err := DecodeSection775(section.Blocks)
		if err != nil {
			t.Fatalf("section %d blocks: %v", i, err)
		}
		biomes, err := DecodeBiomes775(section.Biomes)
		if err != nil {
			t.Fatalf("section %d biomes: %v", i, err)
		}
		values[i] = SectionValues{
			Blocks:     blocks,
			Biomes:     biomes,
			BlockCount: section.BlockCount,
			FluidCount: section.FluidCount,
		}
	}

	encoded, err := EncodeColumn775(values, Global775{BlockBits: 15, BiomeBits: 7})
	if err != nil {
		t.Fatalf("EncodeColumn775: %v", err)
	}

	back, err := Split775(encoded, bottom)
	if err != nil {
		t.Fatalf("Split775 of the re-encoded column: %v", err)
	}
	if len(back) != len(original) {
		t.Fatalf("re-encoded to %d sections, want %d", len(back), len(original))
	}

	for i := range back {
		if back[i].Y != original[i].Y {
			t.Errorf("section %d is at Y %d, want %d", i, back[i].Y, original[i].Y)
		}
		if back[i].BlockCount != original[i].BlockCount ||
			back[i].FluidCount != original[i].FluidCount {
			t.Errorf("section %d counts changed", i)
		}
		blocks, err := DecodeSection775(back[i].Blocks)
		if err != nil {
			t.Fatalf("section %d blocks: %v", i, err)
		}
		if !slices.Equal(blocks, values[i].Blocks) {
			t.Errorf("section %d decoded to different blocks", i)
		}
		biomes, err := DecodeBiomes775(back[i].Biomes)
		if err != nil {
			t.Fatalf("section %d biomes: %v", i, err)
		}
		if !slices.Equal(biomes, values[i].Biomes) {
			t.Errorf("section %d decoded to different biomes", i)
		}
	}
}
```

Add `"os"` and `"path/filepath"` to the imports if absent.

- [ ] **Step 2: Run it**

Run: `devbox run -- go test ./wire/java/chunk -run TestEncodeColumn775 -v`
Expected: PASS if Task 1 is right. A failure here is a real defect in the encoder, not a fixture problem — read which section and which container disagreed before changing anything.

- [ ] **Step 3: Record it in the changelog**

Add under `## Unreleased`, `### Added`, in the existing style: `wire/java/chunk` encodes a protocol 775 column as well as reading one; name `EncodeSection775`, `EncodeColumn775`, `SectionValues`, and `Global775`; state the four-bit floor and why (a client reads a narrower declaration at four and silently misaligns); state that `Global775` is a parameter because the width is a registry property this package does not own.

- [ ] **Step 4: Run the full gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
devbox run -- task precommit
git add wire/java/chunk/protocol775_test.go CHANGELOG.md
git commit -m "test(chunk): round-trip a captured 775 column through the encoder"
```

---

## Stage B — swapping the session at the handshake boundary

Executed in `../minecraft-protocol`. A server builds its session before it has read anything, so it must choose a protocol before the client has said which one it speaks. `newStream` in this repository picks `v1_8.Protocol()` unconditionally, which is exactly right for a server that speaks one version and impossible for one that speaks two.

The handshake packet is the same on both versions, so the fix is to read it on either session and then swap. Nothing today can swap.

### Task 3: `SessionControl`

**Files:**
- Modify: `session.go` (the `Control` implementations)
- Modify: `stream_runtime.go` (`processControl`)
- Modify: `stream_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `Control`, `Session`, `Stream.Control`, and the discipline `EnableEncryption` already follows — refuse when the buffer is not empty.
- Produces: `type SessionControl struct { Session Session }` with `ControlName() string`, accepted by `Stream.Control` only in the handshaking state and only with no buffered inbound bytes.

- [ ] **Step 1: Write the failing test**

```go
func TestSessionControlSwapsTheSessionAtTheHandshakeBoundary(t *testing.T) {
	limits := testStreamLimits(t)

	// Both sessions start in handshaking, which is the only state the swap is
	// legal in: it is where a server sits when it has read the packet that
	// says which version the client speaks and has decoded nothing else.
	first := newTestSession(t, limits)
	first.state = State("handshaking")
	second := newTestSession(t, limits)
	second.state = State("handshaking")

	reader := newScriptedReader()
	writer := &syncWriter{}
	stream, err := NewStream(first, Transport{
		Reader:    reader,
		Writer:    writer,
		Interrupt: reader.interrupt,
	})
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := stream.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if err := stream.Control(ctx, SessionControl{Session: second}); err != nil {
		t.Fatalf("SessionControl: %v", err)
	}

	reader.deliver(testFrameBytes(0x01, 'a'))
	if _, err := stream.Read(ctx); err != nil {
		t.Fatalf("Read after the swap: %v", err)
	}

	if len(second.decodeStates) != 1 {
		t.Fatalf("the new session decoded %d frames, want 1", len(second.decodeStates))
	}
	if len(first.decodeStates) != 0 {
		t.Fatalf("the replaced session decoded %d frames, want 0", len(first.decodeStates))
	}
}

func TestSessionControlIsRefusedOnceTheStateHasMoved(t *testing.T) {
	// The refusal is the point. A swap after the state has moved would hand
	// the next frame to a session that never saw the transition, and it would
	// decode under a different set of packet IDs without reporting anything.
	limits := testStreamLimits(t)
	session := newTestSession(t, limits) // its default state is play
	replacement := newTestSession(t, limits)

	reader := newScriptedReader()
	writer := &syncWriter{}
	stream, err := NewStream(session, Transport{
		Reader:    reader,
		Writer:    writer,
		Interrupt: reader.interrupt,
	})
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := stream.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = stream.Close() }()

	err = stream.Control(ctx, SessionControl{Session: replacement})
	if err == nil {
		t.Fatal("the swap was accepted in the play state")
	}
	if !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("error = %v, want an ErrInvalidStream", err)
	}

	// The stream still works, on the session it kept.
	reader.deliver(testFrameBytes(0x01, 'a'))
	if _, err := stream.Read(ctx); err != nil {
		t.Fatalf("Read after a refused swap: %v", err)
	}
	if len(session.decodeStates) != 1 {
		t.Fatalf("the kept session decoded %d frames, want 1", len(session.decodeStates))
	}
}
```

Both use the fakes in `stream_test_helpers_test.go` — `newTestSession`,
`newScriptedReader`, `syncWriter`, `testFrameBytes`, `testStreamLimits`. Read
that file before adding a helper of your own; it already provides everything
these need.

- [ ] **Step 2: Run them to verify they fail**

Run: `devbox run -- go test . -run TestSessionControl -v`
Expected: FAIL, `undefined: SessionControl`.

- [ ] **Step 3: Implement**

In `session.go`:

```go
// SessionControl replaces the session a stream decodes with.
//
// It exists for one situation and refuses every other: a server has to build a
// session before a client has said which version it speaks, and the handshake
// that says so is the one packet every version encodes the same way. So a
// server reads the handshake on whichever session it happened to build, and
// swaps to the one the client asked for before the login begins.
//
// The stream refuses it unless the session is still in the handshaking state
// and no inbound bytes are buffered, for the reason EnableEncryption refuses on
// a non-empty buffer: bytes already read were framed by the old session, and
// handing them to a new one decodes them under a different set of packet IDs.
type SessionControl struct {
	Session Session
}

// ControlName implements Control.
func (SessionControl) ControlName() string { return "session" }
```

In `stream_runtime.go`'s `processControl`, handle it beside the existing controls: verify the state, verify the buffer is empty, install the session, refresh the cached snapshot. Follow the surrounding code's error style; return `ErrInvalidStream`-wrapped errors rather than new sentinels.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- go test . -run TestSessionControl -v`
Expected: PASS.

- [ ] **Step 5: Changelog, gate, commit**

```bash
devbox run -- task verify
devbox run -- task precommit
git add session.go stream_runtime.go stream_test.go CHANGELOG.md
git commit -m "feat(stream): swap the session at the handshake boundary"
```

- [ ] **Step 6: Release and take it**

Tag `minecraft-protocol` per its `RELEASING.md`, which names the five consumer modules a release is not finished without. Then, in this repository:

```bash
devbox run -- go get github.com/go-theft-craft/minecraft-protocol@<tag>
devbox run -- task tidy
devbox run -- task verify
git commit -am "build: take minecraft-protocol <tag>"
```

---

## Stage C — the version-neutral play seam

This repository. **No behaviour changes and no bytes change.** The whole stage is moving `v1_8` out of the play path and behind an interface, with the existing fixtures as the proof that nothing moved with it.

The order matters: the seam is defined and implemented for 47 *before* any 775 code exists, so the interface is shaped by a version that works rather than by one that is being guessed at. Where 775 is known to differ, the difference is recorded in a comment on the method rather than designed for now.

### Task 4: Define the dialect

**Files:**
- Create: `internal/server/conn/dialect.go`
- Create: `internal/server/conn/dialect_test.go`

**Interfaces:**
- Consumes: `world.Packet` (`interface{ PacketID() int32 }`), `player.Slot`, `world.BlockPos`, `protocol.Packet`.
- Produces: the `Dialect` interface and the neutral action types below. Every later task in stages C through H names these.

- [ ] **Step 1: Write the interface**

`dialect.go` defines two halves and nothing else. Inbound, one type per thing a client can ask for:

```go
// Package-level note for dialect.go:
//
// A Dialect is the version boundary of the play path. Above it, a handler
// reasons about what a player did; below it, a generated packet type says how
// one version spells that. It is the same seam world.Adapter is for blocks,
// and it exists for the same reason: the code that decides what happens when a
// player breaks a block is not version-specific, and until this existed it was
// written as though it were.

// Action is what a client asked for, in terms no version owns.
type Action interface{ action() }

type MoveAction struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
	HasPos     bool
	HasLook    bool
}

type DigAction struct {
	Pos    world.BlockPos
	Status DigStatus
	Face   BlockFace
}

type PlaceAction struct {
	Pos      world.BlockPos
	Face     BlockFace
	Cursor   [3]float32
	Hand     Hand
	Sequence int32 // 775 only; zero on 47, and acknowledged only where non-zero
}

	// The rest, one per serverbound packet the 47 play path handles today.
	// The list is exhaustive as of 2026-08-19 and was read out of the
	// handlePlay switch rather than invented:
	//
	//   ClickAction         (window_click)       CloseWindowAction  (close_window)
	//   ChatAction          (chat)               KeepAliveAction    (keep_alive)
	//   HeldSlotAction      (held_item_slot)     ArmSwingAction     (arm_animation)
	//   EntityActionAction  (entity_action)      UseEntityAction    (use_entity)
	//   SettingsAction      (settings)           AbilitiesAction    (abilities)
	//   TabCompleteAction   (tab_complete)       CreativeSlotAction (set_creative_slot)
	//   SignAction          (update_sign)        TransactionAction  (transaction)
	//   ClientCommandAction (client_command)     PayloadAction      (custom_payload)
	//   SpectateAction      (spectate)           ResourcePackAction (resource_pack_receive)
	//
	// steer_vehicle and enchant_item are handled by being ignored, so Read
	// returns (nil, true) for them and neither gets a type. Do not add one
	// for symmetry.

// Dialect turns one version's packets into actions and back.
type Dialect interface {
	// Protocol is the descriptor the connection's session was built from.
	Protocol() protocol.Protocol

	// Read turns a decoded inbound packet into an action. A packet this
	// version defines and this server ignores returns (nil, true): the
	// distinction between "ignored" and "unknown" is the connection's to log.
	Read(packet protocol.Packet) (Action, bool)

	// The write half. Each returns the packet its version spells the message
	// with; the caller sends it.
	Join(JoinFields) (world.Packet, error)
	Position(PositionFields) (world.Packet, error)
	KeepAlive(id int64) (world.Packet, error)
	Chat(message string, position byte) (world.Packet, error)
	BlockChange(pos world.BlockPos, state int32) (world.Packet, error)
	MultiBlockChange(section world.ChunkPos, changes []StateChange) (world.Packet, error)
	WindowItems(window int8, slots []player.Slot) (world.Packet, error)
	SetSlot(window int8, slot int16, item player.Slot) (world.Packet, error)
	OpenWindow(OpenWindowFields) (world.Packet, error)
	Transaction(window int8, action int16, accepted bool) (world.Packet, error)
	Abilities(flags int8, flying, walking float32) (world.Packet, error)
	UpdateHealth(health float32, food int32, saturation float32) (world.Packet, error)
	UpdateTime(age, time int64) (world.Packet, error)
	SpawnPosition(pos world.BlockPos) (world.Packet, error)
	Respawn(RespawnFields) (world.Packet, error)
	GameStateChange(reason uint8, value float32) (world.Packet, error)
	Disconnect(reason string) (world.Packet, error)
	TabComplete(matches []string) (world.Packet, error)
	CustomPayload(channel string, payload []byte) (world.Packet, error)
	BlockBreakAnimation(entity int32, pos world.BlockPos, stage int8) (world.Packet, error)
	WorldEvent(effect int32, pos world.BlockPos, data int32, global bool) (world.Packet, error)
	SprintParticles(x, y, z float64, state int32) (world.Packet, error)

	// The entity half, which player.Manager drives through the narrow
	// interface it declares for itself.
	SpawnPlayer(SpawnPlayerFields) (world.Packet, error)
	SpawnEntity(SpawnEntityFields) (world.Packet, error)
	DestroyEntities(ids []int32) (world.Packet, error)
	EntityMove(id int32, dx, dy, dz int8, onGround bool) (world.Packet, error)
	EntityLook(id int32, yaw, pitch int8, onGround bool) (world.Packet, error)
	EntityMoveLook(EntityMoveLookFields) (world.Packet, error)
	EntityTeleport(EntityTeleportFields) (world.Packet, error)
	EntityHeadRotation(id int32, yaw int8) (world.Packet, error)
	EntityVelocity(id int32, dx, dy, dz int16) (world.Packet, error)
	EntityMetadata(id int32, entries []MetadataEntry) (world.Packet, error)
	EntityEquipment(id int32, slot int16, item player.Slot) (world.Packet, error)
	EntityStatus(id int32, status int8) (world.Packet, error)
	EntityAnimation(id int32, animation uint8) (world.Packet, error)
	CollectItem(collected, collector int32) (world.Packet, error)
	PlayerInfo(PlayerInfoFields) (world.Packet, error)
}
```

That list is exhaustive as of 2026-08-19: it is every distinct clientbound type
`internal/server/conn` and `internal/server/player` construct, plus `MapChunk`
and `KickDisconnect`, which the world adapter and the disconnect path already
own. Verify it before implementing, because a packet added between this plan
and its execution has to be here too:

```bash
grep -rhno 'v1_8\.PlayClientbound[A-Za-z]*' internal/server/conn/*.go internal/server/player/*.go \
  | grep -v _test | sed 's/.*v1_8\.PlayClientbound//' | sort -u
```

`EntityMetadata` takes neutral `MetadataEntry` values rather than a generated
metadata list, because the two versions terminate one differently — `0x7F`
against `0xFF`, which is why `protocolinfo.MetadataEnd` is version-scoped — and
encode their entries differently. Do not add a method for something only 775
might want; the interface is complete when it covers what the 47 path sends.

Two naming rules keep the rest of the file mechanical, so nothing here is a
judgement call:

- **Every remaining action carries exactly the fields its packet carries**,
  with the same names and version-neutral types — `KeepAliveAction{ID int64}`,
  `ChatAction{Message string}`, `HeldSlotAction{Slot int16}`. Where the two
  versions size a field differently, take the wider of the two: protocol 47's
  keep-alive ID is a VarInt and 775's is an `int64`, so the action says
  `int64` and the 47 dialect narrows it.
- **Every `…Fields` struct is the argument list of the packet it names**, for
  the packets whose argument list is too long to sit in a signature —
  `JoinFields`, `PositionFields`, `OpenWindowFields`, `RespawnFields`,
  `SpawnPlayerFields`, `SpawnEntityFields`, `EntityMoveLookFields`,
  `EntityTeleportFields`, `PlayerInfoFields`. Same field names as the
  generated type, neutral types, defined in `dialect.go` beside the interface.
  `StateChange`, `MetadataEntry`, `DigStatus`, `BlockFace`, and `Hand` are
  defined there too.


- [ ] **Step 2: Write the compile-time assertion test**

```go
func TestV47SatisfiesTheDialect(t *testing.T) {
	var _ Dialect = (*v47Dialect)(nil)
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `devbox run -- go test ./internal/server/conn -run TestV47Satisfies -v`
Expected: FAIL, `undefined: v47Dialect`.

- [ ] **Step 4: Commit the interface alone**

```bash
devbox run -- task precommit
git add internal/server/conn/dialect.go internal/server/conn/dialect_test.go
git commit -m "feat(conn): define the play path's version boundary"
```

### Task 5: Implement the dialect for protocol 47

**Files:**
- Create: `internal/server/conn/dialect_v47.go`
- Create: `internal/server/conn/dialect_v47_test.go`

**Interfaces:**
- Consumes: `Dialect` and every action type from Task 4; `v1_8` generated types.
- Produces: `func newV47Dialect() *v47Dialect` and the `v47Dialect` type satisfying `Dialect`.

- [ ] **Step 1: Write the failing test**

Two table tests, one per direction. Inbound: one case for each serverbound
packet the 47 path handles today. Outbound: one case per method, pairing
arguments with the exact `v1_8` value expected, compared with
`reflect.DeepEqual`. The cases below are the shape; write one for every entry
in the two lists in `dialect.go`, and let the count of cases be what says the
migration is complete.

```go
func TestV47DialectReadsEveryPacketThePlayPathHandles(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  Action
	}{
		{
			name:  "position",
			value: &v1_8.PlayServerboundPosition{X: 1, Y: 2, Z: 3, OnGround: true},
			want:  MoveAction{X: 1, Y: 2, Z: 3, OnGround: true, HasPos: true},
		},
		{
			name:  "look",
			value: &v1_8.PlayServerboundLook{Yaw: 90, Pitch: 45, OnGround: true},
			want:  MoveAction{Yaw: 90, Pitch: 45, OnGround: true, HasLook: true},
		},
		{
			name: "position_look",
			value: &v1_8.PlayServerboundPositionLook{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: 45, OnGround: true,
			},
			want: MoveAction{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: 45,
				OnGround: true, HasPos: true, HasLook: true,
			},
		},
		{
			name:  "flying",
			value: &v1_8.PlayServerboundFlying{OnGround: true},
			want:  MoveAction{OnGround: true},
		},
		{
			name:  "keep_alive",
			value: &v1_8.PlayServerboundKeepAlive{KeepAliveID: 7},
			want:  KeepAliveAction{ID: 7},
		},
		{
			name:  "chat",
			value: &v1_8.PlayServerboundChat{Message: "hello"},
			want:  ChatAction{Message: "hello"},
		},
		// ... and so on, one per entry in the inbound list in dialect.go.
	}

	dialect := newV47Dialect()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, known := dialect.Read(protocol.Packet{Value: test.value})
			if !known {
				t.Fatalf("Read reported %T unknown", test.value)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Read(%T) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

func TestV47DialectWritesThePacketsTheHandlerUsedToBuild(t *testing.T) {
	dialect := newV47Dialect()

	tests := []struct {
		name string
		got  func() (world.Packet, error)
		want world.Packet
	}{
		{
			name: "keep_alive",
			got:  func() (world.Packet, error) { return dialect.KeepAlive(7) },
			want: &v1_8.PlayClientboundKeepAlive{KeepAliveID: 7},
		},
		{
			name: "block_change",
			got: func() (world.Packet, error) {
				return dialect.BlockChange(world.BlockPos{X: 1, Y: 2, Z: 3}, 208)
			},
			want: &v1_8.PlayClientboundBlockChange{
				Location: v1_8.Position{X: 1, Y: 2, Z: 3},
				Type:     208,
			},
		},
		// ... and so on, one per method on Dialect.
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.got()
			if err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}
```

Where a case here disagrees with the handler as it stands — a field the handler
fills that this omits — the handler is right and the case is wrong. Read the
call site before changing either.


- [ ] **Step 2: Run to verify failure**, then **Step 3: implement** — each method is the packet literal that is in the handler today, moved, not rewritten. **Step 4: run to verify pass.**

- [ ] **Step 5: Commit**

```bash
git add internal/server/conn/dialect_v47.go internal/server/conn/dialect_v47_test.go
git commit -m "feat(conn): implement the dialect for protocol 47"
```

### Task 6: Move the play path onto the dialect

**Files:**
- Modify: `internal/server/conn/connection.go` (hold a `Dialect`; set it in `newConnection`)
- Modify: `internal/server/conn/handler_play.go`, `inventory.go`, `chest.go`, `crafting.go`, `damage.go`, `mining.go`, `commands.go`, `tab_complete.go`, `slot.go`
- Modify: `internal/server/player/manager.go`, `metadata.go`, `equipment.go`, `item_entity.go`, `inventory.go`

**Interfaces:**
- Consumes: `Dialect`, `newV47Dialect`.
- Produces: no new API. `handlePlay` becomes a switch over `Action` rather than over `*v1_8.…`, and every `c.send(&v1_8.X{…})` becomes `c.send(c.dialect.X(…))`.

`player.Manager` cannot name `conn.Dialect` — it sits below `conn`. Give it the narrow interface it actually needs, declared in `player` and satisfied structurally by the dialect, the way `world.Generator` is declared in `world` and satisfied by `gen.Generator`. Do not move `player` above `conn` to avoid this.

- [ ] **Step 1: Run the fixtures before touching anything, and keep the output**

Run: `devbox run -- go test ./internal/server/conn -run TestParity -v`
Expected: PASS. This output is the baseline the rest of the task is measured against.

- [ ] **Step 2: Migrate one file at a time, running the fixtures after each**

Order: `handler_play.go`, then `inventory.go`, `chest.go`, `crafting.go`, `damage.go`, `mining.go`, `commands.go`, `tab_complete.go`, `slot.go`, then the `player` package. After each file: `devbox run -- go test ./internal/server/... -count=1`.

- [ ] **Step 3: Assert the boundary holds**

Add to `dialect_test.go`:

```go
// TestThePlayPathNamesNoVersion is the seam's own gate. A v1_8 reference
// outside the dialect files is a version leaking back into the path this
// stage exists to make neutral.
func TestThePlayPathNamesNoVersion(t *testing.T) {
	// Walk internal/server/conn and internal/server/player, parse each
	// non-test file, and fail on an import of a generated version package
	// outside dialect_v47.go and dialect_v775.go.
}
```

Write it with `go/parser` over the package directory rather than by shelling out to grep.

- [ ] **Step 4: Run everything**

Run: `devbox run -- task verify`
Expected: PASS, with the byte-parity fixtures **unchanged** — no `-update`, no diff. If a fixture disagrees, the migration changed a byte and the fix is in the migration, never in the fixture.

- [ ] **Step 5: Commit**

```bash
devbox run -- task precommit
git add internal/server/conn internal/server/player
git commit -m "refactor(conn): put the play path behind the dialect"
```

### Task 7: Record the seam

**Files:**
- Modify: `CLAUDE.md` (the `internal/server/` bullet)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Describe the seam in CLAUDE.md** beside the `world.Adapter` description it mirrors: what a `Dialect` is, that `player` takes a structural interface of its own because it sits below `conn`, and that `TestThePlayPathNamesNoVersion` is what keeps it true.

- [ ] **Step 2: Changelog entry**, noting that no bytes changed and the fixtures are what says so.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md CHANGELOG.md
git commit -m "docs: record the play path's version boundary"
```

---

## Stage D — the protocol 775 world adapter

### Task 8: `pkg/world/v775` construction and state encoding

**Files:**
- Create: `pkg/world/v775/adapter.go`
- Create: `pkg/world/v775/adapter_test.go`

**Interfaces:**
- Consumes: `world.StateRegistry`, `world.NewJavaRegistry`, `data.Set` from `v26_1.Data()`, `chunk.Global775`.
- Produces: `func New(reg world.StateRegistry, set *data.Set) (*Adapter, error)` satisfying `world.Adapter`, plus `Overworld261() world.Dimension`.

Read `pkg/world/v47/adapter.go` first and mirror its shape: a slice indexed by handle for encode, a map for decode, a data set for item names, an air section built once.

The mapping differs entirely. Protocol 47 packs an ID and a metadata nibble; 775 sends the block-state registry ID, which the data set publishes directly: `data.Block.MinStateID`, `MaxStateID`, `DefaultState`, and `States` describing the properties the range varies over. A handle whose properties the block does not define is a construction error, not a render-time fallback.

`Overworld261()` is `world.Dimension{Name: "minecraft:overworld", MinY: -64, Height: 384}`. It goes in `pkg/world/dimension.go` beside `Overworld18()`, not in the adapter.

- [ ] **Step 1: Write the failing test** — every state in the 26.1 registry round-trips through `EncodeState`/`DecodeState`, and the encoded value for `minecraft:stone` equals the data set's `DefaultState` for stone.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Commit** — `feat(v775): encode a block state as protocol 775 numbers it`.

### Task 9: Column rendering

**Files:**
- Modify: `pkg/world/v775/adapter.go`
- Modify: `pkg/world/v775/adapter_test.go`

**Interfaces:**
- Consumes: `chunk.EncodeColumn775`, `chunk.SectionValues`, `chunk.Global775`, `v26_1.PlayClientboundMapChunk`.
- Produces: `EncodeChunk` and `EncodeUnload` on `*Adapter`.

Three decisions this task makes, each of which needs its reason in a comment:

**Heightmaps.** The packet carries a list of `{Type string, Data []int64}` and the generated mapper names the six types: `world_surface_wg`, `world_surface`, `ocean_floor_wg`, `ocean_floor`, `motion_blocking`, `motion_blocking_no_leaves`. Send `motion_blocking` and `world_surface`, computed from the column: 256 entries packed at nine bits, which is what a 384-tall dimension needs to name a height in 0..384.

**Light.** Send a full-bright sky: every section's mask bit set and a 2048-byte array of `0xFF` for each, with block light empty. The alternative is computing propagation, which is a world-model feature and not a wire concern; a comment must say that this is a deliberate stand-in and what it costs (a cave is lit).

**Global palette width.** `chunk.Global775{BlockBits: bits.Len(uint(highestStateID)), BiomeBits: bits.Len(uint(highestBiomeID))}`, both computed from the data set at construction with `math/bits`, never hardcoded. `chunk`'s own `bitsFor` is unexported and stays that way: the caller owns the registry, so the caller owns the arithmetic over it.

- [ ] **Step 1: Write the failing test** — build a chunk with known blocks at known positions, encode it, `Split775` the packet's `ChunkData` at the dimension's bottom section, `DecodeSection775` the section each block is in, and assert the block is where it was put. Include one test that a column of air encodes and decodes to air, and one that the section count equals `Dimension().Sections()`.
- [ ] **Step 2–4: fail, implement, pass.**
- [ ] **Step 5: Commit** — `feat(v775): render a column as protocol 775 sends one`.

### Task 10: Prove the two adapters disagree the way they should

**Files:**
- Modify: `pkg/world/v775/adapter_test.go`

- [ ] **Step 1: Write the test.** The same canonical block name, interned into each version's registry, encodes to different numbers and to the same block. This is `TestTheTwoJavaRegistriesAgreeOnNamesAndNotOnHandles` one layer up, and it is what says the world model stayed version-neutral while gaining a second version.
- [ ] **Step 2: Run, then commit** — `test(v775): the two adapters agree on blocks and not on numbers`.

---

## Stages E through H — scope, entry conditions, and gates

Each of these gets its own plan document, written when the stage before it lands.

### Stage E — configuration and the 775 join

**Entry condition:** Stages A–D committed, `minecraft-protocol` released and taken.

**Scope.** `newStream` builds a session from the handshake's protocol version through the Stage B control. `login.Acceptor` gains its `WithConfiguration` step: the server sends `select_known_packs`, the registry payload, tags, and `finish_configuration`. The registry payload's source is `data.Set.LoginPacket.DimensionCodec`, which upstream publishes as tagged JSON and the wire needs as binary NBT, so this stage owns that conversion — `pkg/world/nbt` writes NBT already and is where it belongs.

**Gate:** `headless-minecraft`'s 775 adapter logs in, reaches play, and receives a column it can decode. A real vanilla 26.1.2 client reaching the world is the stronger check and needs a person; record which one ran.

**The trap this stage will hit:** a 26.1 client that receives no registry data waits in configuration looking healthy. `minecraft-protocol`'s live check found the mirror image of this on the client side; expect to spend the stage's debugging budget there.

### Stage F — movement, keep-alive, chat, commands

**Entry condition:** Stage E's client reaches play.

**Scope.** `MoveAction` and the position/teleport-confirm exchange at 775, keep-alive at 775, chat as a text component rather than a JSON string, and `v775.RenderCommands` finally sent to a client. The chunk-batch handshake (`chunk_batch_start`, `chunk_batch_finished`, and answering `chunk_batch_received`) is this stage's, and without it a client receives one batch and stops.

**Gate:** `task test:owned` in `headless-minecraft` runs its six movement scenarios against this server at 775 as well as 47 — the M10 matrix row that is currently protocol 47 only. M11.7's brigadier tree reaches a client, which closes the limitation its package doc records.

### Stage G — inventory, windows, crafting, chests

**Entry condition:** Stage F green.

**Scope.** The slot format is where 775 diverges most: an item is a count, an ID, and a set of components, against protocol 47's ID, count, damage, and NBT blob. `player.Slot` stays version-neutral and the dialect owns the translation both ways. Window IDs are VarInts rather than bytes, and the click packet carries a state ID the server must track and echo.

**Gate:** the chest, crafting, and drag-click test suites run against both dialects. `TestRandomClickSequencesNeverBreakTheInvariant` is the one that matters: it must hold at 775 without being weakened.

### Stage H — mining, placement, combat, persistence, and the lanes

**Entry condition:** Stage G green.

**Scope.** Dig and place with the 775 sequence-acknowledgement, damage and combat, entity metadata (which terminates at `0xFF` rather than protocol 47's `0x7F` — `protocolinfo.MetadataEnd` is already commented for this), equipment, and the player-data path. Then the lanes: the Node interop lane at 775 if the pinned client supports it (as of 2026-08-18 `minecraft-protocol@1.66.2` stops at 1.21.11, so expect to record that it cannot rather than to make it), the vanilla-client check, and the conformance matrix rows.

**Gate:** every lane that runs at 47 runs at 775 or records why it cannot, and `RELEASING.md` names what a 775-capable release is not finished without.

---

## Notes for the implementer

**The one subtle thing.** Vanilla picks its palette configuration from the width byte it read and then unpacks with *that configuration's* width, not with the width on the wire. For block states, one through four all map to four bits. An encoder that declares three because three is enough writes a container a client reads at four, and every entry after the first is wrong — with no error anywhere, on either side. `minBlockPaletteBits775` is why. If you find yourself removing the floor because a test passes without it, the test is this package's decoder agreeing with this package's encoder, which is exactly the agreement that cannot catch this.

**The second subtle thing.** A 775 column blob does not say where it starts. The bottom section index comes from the `dimension_type` registry sent in configuration, so Stage E's registry payload and Stage D's `Overworld261()` must agree on `MinY = -64`. They disagree by four sections' worth of blocks if one of them is edited alone, which reads as a working client standing inside the ground.

**Do not extend the dialect for 775 before Stage E needs it.** The interface is complete when it covers what the 47 path sends today. A method added in Stage C because 775 might want it is a method designed without a caller, and the first real caller will want a different shape.

**`player` sits below `conn`.** When the migration needs the manager to build a packet, declare the narrow interface in `player` and let the dialect satisfy it structurally. Moving `player` above `conn`, or importing `conn` from `player`, is an import cycle wearing a disguise.

**If a generated 775 codec rejects a real client's packet**, that is a `minecraft-protocol` bug and the fix is a byte fixture and a codec change there, then a release, then a bump here. It is never a widened limit in this repository.
