// Package v775 encodes the version-neutral world model as Java Edition 26.1's
// protocol 775 sees it: a block state is its block-state registry ID, which the
// data set publishes per block as a closed range, and a chunk column is a run
// of paletted containers.
package v775

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-theft-craft/minecraft-protocol/data"
	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/server/pkg/world"
)

// ErrNoColumnEncoder is what EncodeChunk returns until wire/java/chunk can
// write a protocol 775 column. It reads one and cannot write one, and this
// repository owns no wire layout of its own, so the column waits on the
// encoder rather than being written here.
var ErrNoColumnEncoder = errors.New("v775: no protocol 775 column encoder in this minecraft-protocol release")

// Adapter renders the world model for protocol 775.
type Adapter struct {
	reg world.StateRegistry
	dim world.Dimension

	// encode is indexed by handle, so the hot loop is an array index rather
	// than a map lookup.
	encode []int32
	decode map[int32]world.State
}

// New builds an adapter for a registry and the data set that registry was
// built from. The two must agree: a handle whose block the set does not name,
// or whose properties the block does not define, is a construction error
// rather than a wrong block at render time.
func New(reg world.StateRegistry, set *data.Set) (*Adapter, error) {
	if reg == nil {
		return nil, errors.New("v775: nil registry")
	}
	if set == nil {
		return nil, errors.New("v775: nil data set")
	}

	a := &Adapter{
		reg:    reg,
		dim:    world.Overworld261(),
		encode: make([]int32, reg.Len()),
		decode: make(map[int32]world.State, reg.Len()),
	}

	blocks := set.Blocks()
	for s := world.State(0); int(s) < reg.Len(); s++ {
		name, props, ok := reg.Lookup(s)
		if !ok {
			return nil, fmt.Errorf("v775: registry reported %d states but %d is unknown", reg.Len(), s)
		}
		block, ok := blocks.ByName(strings.TrimPrefix(name, "minecraft:"))
		if !ok {
			return nil, fmt.Errorf("v775: the data set does not know %s", name)
		}
		id, err := stateID(block, props)
		if err != nil {
			return nil, fmt.Errorf("v775: %s: %w", name, err)
		}
		if other, clash := a.decode[id]; clash {
			return nil, fmt.Errorf("v775: %s and handle %d both encode to %d", name, other, id)
		}
		a.encode[s] = id
		a.decode[id] = s
	}

	return a, nil
}

// stateID is the registry ID of one of a block's states.
//
// It is the inverse of how world.NewJavaRegistry decomposes a state: the
// offset into the block's range counts every property as a digit, with the
// last property varying fastest, which is how the vanilla registry orders
// them. A property the block does not define, a value it does not take, or a
// missing property is an error -- in particular the single metadata property
// of a pre-flattening registry, which this adapter cannot represent.
func stateID(block data.Block, props world.Properties) (int32, error) {
	if len(props) != len(block.States) {
		return 0, fmt.Errorf("%d properties, and the block defines %d", len(props), len(block.States))
	}

	offset := 0
	for _, state := range block.States {
		values := stateValues(state)
		if len(values) == 0 {
			return 0, fmt.Errorf("property %q has no values", state.Name)
		}
		value, ok := valueOf(props, state.Name)
		if !ok {
			return 0, fmt.Errorf("no value for property %q", state.Name)
		}
		index := slices.Index(values, value)
		if index < 0 {
			return 0, fmt.Errorf("property %q does not take %q", state.Name, value)
		}
		offset = offset*len(values) + index
	}

	id := int(block.MinStateID) + offset
	if id > int(block.MaxStateID) {
		return 0, fmt.Errorf("state %d is past the block's last state %d", id, block.MaxStateID)
	}

	return int32(id), nil
}

// stateValues is the ordered value list of one property. Only bool leaves
// Values implicit upstream, and its order is true before false -- which is why
// grass_block's default, snowy=false, is the second of its two states. It
// agrees with the world package's own reading of the same data, and the tests
// hold it to that through every block's default state.
func stateValues(s data.BlockState) []string {
	if len(s.Values) > 0 {
		return s.Values
	}
	if s.Type == "bool" {
		return []string{"true", "false"}
	}

	return nil
}

func valueOf(props world.Properties, key string) (string, bool) {
	for _, p := range props {
		if p.Key == key {
			return p.Value, true
		}
	}

	return "", false
}

// Registry implements world.Adapter.
func (a *Adapter) Registry() world.StateRegistry { return a.reg }

// Dimension implements world.Adapter.
func (a *Adapter) Dimension() world.Dimension { return a.dim }

// EncodeState implements world.Adapter.
func (a *Adapter) EncodeState(s world.State) (int32, error) {
	if int(s) >= len(a.encode) {
		return 0, fmt.Errorf("v775: state %d is not from this adapter's registry", s)
	}

	return a.encode[s], nil
}

// DecodeState implements world.Adapter.
func (a *Adapter) DecodeState(v int32) (world.State, error) {
	s, ok := a.decode[v]
	if !ok {
		return 0, fmt.Errorf("v775: no block state encodes to %d", v)
	}

	return s, nil
}

// EncodeUnload implements world.Adapter. Protocol 775 has a packet of its own
// for it, where protocol 47 sent an empty ground-up column.
func (a *Adapter) EncodeUnload(pos world.ChunkPos) (world.Packet, error) {
	return &v26_1.PlayClientboundUnloadChunk{
		ChunkX: int32(pos.X),
		ChunkZ: int32(pos.Z),
	}, nil
}

// EncodeChunk implements world.Adapter. It returns ErrNoColumnEncoder: see
// there for why the column is not written here.
func (a *Adapter) EncodeChunk(c *world.Chunk) (world.Packet, error) {
	if c == nil {
		return nil, errors.New("v775: nil chunk")
	}

	return nil, ErrNoColumnEncoder
}
