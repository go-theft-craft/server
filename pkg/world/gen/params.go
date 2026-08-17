package gen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/go-theft-craft/server/pkg/world"
)

// Generators are named types built from typed parameters.
//
// A Factory owns a name, its defaults, a parser, and a constructor. A Registry
// maps names to factories and is a *value*, not a package global: the
// interoperability lane runs servers side by side in one test binary, and a
// package-level map would let one test's generator leak into another's.

// Params is a generator's configuration. Each named generator defines its own
// concrete type; the interface exists so a world's metadata can carry any of
// them.
type Params interface {
	// Type is the registered name the parameters belong to.
	Type() string
}

// Factory builds a generator from parameters and a seed.
type Factory interface {
	Name() string
	// Version is the generator's own version. It goes into the world's
	// metadata so a later build can tell that terrain would come out
	// differently now — see the server's startup comparison.
	Version() int
	// Defaults returns freshly allocated default parameters, which is also
	// what documents the surface: marshal them to see every knob.
	Defaults() Params
	// Parse decodes parameters from the form the world's metadata stored them
	// in. An unknown key is an error, not a silently ignored line.
	Parse(raw json.RawMessage) (Params, error)
	New(seed int64, p Params, reg world.StateRegistry) (Generator, error)
}

// Registry maps generator names to factories.
type Registry interface {
	Register(f Factory) error
	Lookup(name string) (Factory, bool)
	Names() []string
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry { return &registry{factories: map[string]Factory{}} }

// DefaultRegistry returns a registry holding the generators this package
// ships: "default" and "flat".
//
// It is a fresh value on each call, so an application registering its own into
// it cannot affect anyone else's.
func DefaultRegistry() Registry {
	r := NewRegistry()
	for _, f := range []Factory{defaultFactory{}, flatFactory{}} {
		if err := r.Register(f); err != nil {
			panic(err) // two built-ins with the same name is a build-time bug
		}
	}

	return r
}

type registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func (r *registry) Register(f Factory) error {
	if f == nil {
		return fmt.Errorf("gen: nil factory")
	}
	name := f.Name()
	if name == "" {
		return fmt.Errorf("gen: factory with no name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.factories[name]; ok {
		return fmt.Errorf("gen: generator %q is already registered", name)
	}
	r.factories[name] = f

	return nil
}

func (r *registry) Lookup(name string) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, ok := r.factories[name]

	return f, ok
}

func (r *registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// parseParams decodes raw into dst, refusing a key dst does not have.
//
// A typo in a parameter file is an error rather than a line nobody reads. An
// empty or absent raw message leaves dst at whatever defaults the caller put
// in it.
func parseParams[T any](raw json.RawMessage, dst *T) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("gen: parse parameters: %w", err)
	}

	return nil
}

// MarshalParams renders parameters the way the world's metadata stores them.
func MarshalParams(p Params) (json.RawMessage, error) {
	if p == nil {
		return nil, nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("gen: marshal %s parameters: %w", p.Type(), err)
	}

	return raw, nil
}
