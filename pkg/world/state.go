package world

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/go-theft-craft/minecraft-protocol/data"
)

// State is an opaque handle to a block state. Its numeric value is assigned at
// registry construction and is meaningful only to the process that built it.
// It is never persisted and never put on the wire: storage holds canonical
// names and the wire holds each version's own encoding, produced by an Adapter.
type State uint32

// Property is one key/value pair of a block state's property set.
type Property struct{ Key, Value string }

// Properties is a block state's property set, sorted by key. Java 1.8 has no
// properties of its own and encodes variants as metadata, which this package
// models as the single property "metadata"; Java 26.1 uses real properties.
type Properties []Property

// StateRegistry maps block state identities to handles and back. A registry is
// built once, frozen, and then read concurrently without a lock.
type StateRegistry interface {
	// Intern returns the handle for a block state identity. An empty property
	// set means "the block's default state", which is metadata 0 before the
	// flattening and DefaultState after it. Interning an identity a frozen
	// registry does not know is a programming error and panics.
	Intern(name string, props Properties) State
	// Lookup returns the canonical identity a handle stands for.
	Lookup(s State) (name string, props Properties, ok bool)
	// Air is the handle for the dimension's empty block.
	Air() State
	// Len is how many distinct states the registry minted.
	Len() int
}

// canonicalName namespaces a block name. The generated game data publishes
// "stone"; storage and every other version-neutral surface says
// "minecraft:stone".
func canonicalName(name string) string {
	if strings.Contains(name, ":") {
		return name
	}

	return "minecraft:" + name
}

// airName is the one block name this package needs to know by heart.
const airName = "minecraft:air"

// metadataKey is the property a pre-flattening version's variants live in.
const metadataKey = "metadata"

// normalize returns a sorted copy of props with empty entries dropped, so that
// two callers naming the same identity in a different order agree.
func normalize(props Properties) Properties {
	if len(props) == 0 {
		return nil
	}

	out := slices.Clone(props)
	slices.SortFunc(out, func(a, b Property) int { return strings.Compare(a.Key, b.Key) })

	return out
}

// identity renders a normalized identity as the registry's map key.
func identity(name string, props Properties) string {
	if len(props) == 0 {
		return name
	}

	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('[')
	for i, p := range props {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	b.WriteByte(']')

	return b.String()
}

type stateEntry struct {
	name  string
	props Properties
}

type stateRegistry struct {
	entries  []stateEntry
	byIdent  map[string]State
	defaults map[string]State
	air      State
	frozen   bool
}

func newStateRegistry() *stateRegistry {
	return &stateRegistry{
		byIdent:  make(map[string]State),
		defaults: make(map[string]State),
	}
}

// define mints a handle at build time. A duplicate identity is a bug in the
// caller's enumeration of the version's states, not a benign re-registration,
// so it reports rather than returning the existing handle.
func (r *stateRegistry) define(name string, props Properties, isDefault bool) (State, error) {
	if r.frozen {
		return 0, errors.New("world: registry is frozen")
	}

	name = canonicalName(name)
	props = normalize(props)
	ident := identity(name, props)
	if _, ok := r.byIdent[ident]; ok {
		return 0, fmt.Errorf("world: duplicate block state %q", ident)
	}

	s := State(len(r.entries))
	r.entries = append(r.entries, stateEntry{name: name, props: props})
	r.byIdent[ident] = s
	if isDefault {
		if _, ok := r.defaults[name]; ok {
			return 0, fmt.Errorf("world: block %q has two default states", name)
		}
		r.defaults[name] = s
	}

	return s, nil
}

func (r *stateRegistry) freeze() { r.frozen = true }

func (r *stateRegistry) Intern(name string, props Properties) State {
	name = canonicalName(name)
	props = normalize(props)

	// An empty property set names the block's default state. It is what a
	// generator palette writes — reg.Intern("minecraft:stone", nil) — and what
	// keeps that palette the same text on a version whose stone has no
	// properties and on one whose variants are metadata.
	if len(props) == 0 {
		if s, ok := r.defaults[name]; ok {
			return s
		}
	}

	ident := identity(name, props)
	if s, ok := r.byIdent[ident]; ok {
		return s
	}

	if r.frozen {
		// A lazily minted handle would need a lock on the block write path,
		// which is the cost this whole model exists to avoid.
		panic(fmt.Sprintf("world: unknown block state %q", ident))
	}

	s, err := r.define(name, props, false)
	if err != nil {
		panic(err)
	}

	return s
}

func (r *stateRegistry) Lookup(s State) (string, Properties, bool) {
	if int(s) >= len(r.entries) {
		return "", nil, false
	}
	e := r.entries[s]

	return e.name, slices.Clone(e.props), true
}

func (r *stateRegistry) Air() State { return r.air }

func (r *stateRegistry) Len() int { return len(r.entries) }

// NewJavaRegistry builds a frozen registry from a version's block data.
//
// Before the flattening a block's variants are metadata 0 through 15 and the
// data set leaves the state range zero; after it, a block occupies the closed
// range MinStateID through MaxStateID and States describes what varies. The
// choice is made once for the whole set rather than per block, because a
// flattened version still has blocks — air among them — whose only state is 0.
func NewJavaRegistry(set *data.Set) (StateRegistry, error) {
	if set == nil {
		return nil, errors.New("world: nil data set")
	}

	blocks := set.Blocks().All()
	if len(blocks) == 0 {
		return nil, errors.New("world: data set has no blocks")
	}

	flattened := false
	for _, b := range blocks {
		if b.MaxStateID != 0 {
			flattened = true

			break
		}
	}

	r := newStateRegistry()
	for _, b := range blocks {
		if err := defineBlock(r, b, flattened); err != nil {
			return nil, err
		}
	}

	air, ok := r.defaults[airName]
	if !ok {
		return nil, fmt.Errorf("world: data set has no %s", airName)
	}
	r.air = air
	r.freeze()

	return r, nil
}

func defineBlock(r *stateRegistry, b data.Block, flattened bool) error {
	if !flattened {
		for meta := range 16 {
			props := Properties{{Key: metadataKey, Value: strconv.Itoa(meta)}}
			if _, err := r.define(b.Name, props, meta == 0); err != nil {
				return err
			}
		}

		return nil
	}

	for id := b.MinStateID; id <= b.MaxStateID; id++ {
		props, err := propertiesForState(b.States, int(id-b.MinStateID))
		if err != nil {
			return fmt.Errorf("world: block %q state %d: %w", b.Name, id, err)
		}
		if _, err := r.define(b.Name, props, id == b.DefaultState); err != nil {
			return err
		}
	}

	return nil
}

// propertiesForState decomposes a block's state offset into its property
// values. The last property varies fastest, which is how the vanilla registry
// orders them.
func propertiesForState(states data.BlockStates, offset int) (Properties, error) {
	if len(states) == 0 {
		if offset != 0 {
			return nil, fmt.Errorf("offset %d for a block with no properties", offset)
		}

		return nil, nil
	}

	props := make(Properties, len(states))
	rem := offset
	for i := len(states) - 1; i >= 0; i-- {
		values := stateValues(states[i])
		if len(values) == 0 {
			return nil, fmt.Errorf("property %q has no values", states[i].Name)
		}
		props[i] = Property{Key: states[i].Name, Value: values[rem%len(values)]}
		rem /= len(values)
	}
	if rem != 0 {
		return nil, fmt.Errorf("offset %d exceeds the property space", offset)
	}

	return normalize(props), nil
}

// stateValues is the ordered value list of one property. Only bool leaves
// Values implicit upstream, and its order is true before false — which is why
// grass_block's default, snowy=false, is the second of its two states.
func stateValues(s data.BlockState) []string {
	if len(s.Values) > 0 {
		return s.Values
	}
	if s.Type == "bool" {
		return []string{"true", "false"}
	}

	return nil
}
