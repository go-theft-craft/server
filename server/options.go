package server

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
)

// ErrInvalidOption reports an option rejected before any network, disk, or
// key-generation work happened.
var ErrInvalidOption = errors.New("invalid server option")

// Option configures a server. Options apply in the order given, so a caller
// that loads a configuration file and then applies command-line flags passes
// WithSettings first and the flags after it.
type Option func(*builder) error

// builder accumulates option results. It exists so New can validate the whole
// set before constructing anything, rather than half-building a server and
// discovering the port is negative.
type builder struct {
	settings    *config.Config
	log         *slog.Logger
	generator   gen.Generator
	registry    gen.Registry
	genName     string
	genParams   json.RawMessage
	dimension   world.Dimension
	migrateFrom string
	worldStore  WorldStore
	sideStore   SideStore
	chunkDetail bool
	commands    Set
	hasCommands bool
	authorizer  Authorizer
	playerStore PlayerStore
	observer    Observer

	provenance         ProvenanceStore
	provenanceOverflow OverflowPolicy
	itemIdentity       bool
	duplicatePolicy    DuplicatePolicy
}

// WithSettings replaces the whole settings struct. The value is copied, so the
// caller may keep mutating its own.
func WithSettings(settings *config.Config) Option {
	return func(b *builder) error {
		if settings == nil {
			return fmt.Errorf("%w: nil settings", ErrInvalidOption)
		}
		copied := *settings
		b.settings = &copied

		return nil
	}
}

// WithLogger sets the logger. A server with no logger would silently drop the
// operational record, so nil is an error rather than a default.
func WithLogger(log *slog.Logger) Option {
	return func(b *builder) error {
		if log == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidOption)
		}
		b.log = log

		return nil
	}
}

// WithGenerator supplies a world generator, replacing the one the generator
// type in settings would have selected.
func WithGenerator(g gen.Generator) Option {
	return func(b *builder) error {
		if g == nil {
			return fmt.Errorf("%w: nil generator", ErrInvalidOption)
		}
		b.generator = g

		return nil
	}
}

// WithPlayerStore supplies per-player persistence. Omit it to run without
// player persistence; a nil store is an error rather than a silent no-op,
// because "I passed a store and nothing was saved" is the harder failure to
// diagnose.
func WithPlayerStore(store PlayerStore) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil player store, omit WithPlayerStore to run without one", ErrInvalidOption)
		}
		b.playerStore = store

		return nil
	}
}

// WithDimension sets the world's vertical extent and name. The default is
// Java 1.8's overworld: 0 to 255.
func WithDimension(d world.Dimension) Option {
	return func(b *builder) error {
		if d.Height <= 0 || d.Height%16 != 0 {
			return fmt.Errorf("%w: dimension height %d is not a positive multiple of 16", ErrInvalidOption, d.Height)
		}
		if d.MinY%16 != 0 {
			return fmt.Errorf("%w: dimension floor %d is not a multiple of 16", ErrInvalidOption, d.MinY)
		}
		b.dimension = d

		return nil
	}
}

// WithGeneratorRegistry replaces the set of named generators the server can
// resolve. The default is gen.DefaultRegistry(), which holds "default" and
// "flat".
//
// An application registering its own starts from DefaultRegistry and adds to
// it; passing a registry that omits the built-ins is a supported way to refuse
// them.
func WithGeneratorRegistry(r gen.Registry) Option {
	return func(b *builder) error {
		if r == nil {
			return fmt.Errorf("%w: nil generator registry", ErrInvalidOption)
		}
		b.registry = r

		return nil
	}
}

// WithGeneratorNamed selects a generator by registered name and gives it
// parameters, which are resolved against the registry when New runs.
//
// Pass nil parameters for the generator's defaults.
func WithGeneratorNamed(name string, params gen.Params) Option {
	return func(b *builder) error {
		if name == "" {
			return fmt.Errorf("%w: empty generator name", ErrInvalidOption)
		}
		raw, err := gen.MarshalParams(params)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidOption, err)
		}
		b.genName, b.genParams = name, raw
		b.settings.GeneratorType = name

		return nil
	}
}

// WithPort sets the listening port.
func WithPort(port int) Option {
	return func(b *builder) error {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%w: port %d outside 1-65535", ErrInvalidOption, port)
		}
		b.settings.Port = port

		return nil
	}
}

// WithSeed sets the world generation seed.
func WithSeed(seed int64) Option {
	return func(b *builder) error {
		b.settings.Seed = seed

		return nil
	}
}

// WithMOTD sets the description shown in the server list.
func WithMOTD(motd string) Option {
	return func(b *builder) error {
		b.settings.MOTD = motd

		return nil
	}
}

// WithOnlineMode enables or disables Mojang authentication.
func WithOnlineMode(online bool) Option {
	return func(b *builder) error {
		b.settings.OnlineMode = online

		return nil
	}
}

// WithMaxPlayers sets the player count shown in the server list.
func WithMaxPlayers(maximum int) Option {
	return func(b *builder) error {
		if maximum < 0 {
			return fmt.Errorf("%w: max players %d is negative", ErrInvalidOption, maximum)
		}
		b.settings.MaxPlayers = maximum

		return nil
	}
}

// WithViewDistance sets the entity view distance in chunks.
func WithViewDistance(chunks int) Option {
	return func(b *builder) error {
		if chunks < 1 {
			return fmt.Errorf("%w: view distance %d is below 1", ErrInvalidOption, chunks)
		}
		b.settings.ViewDistance = chunks

		return nil
	}
}

// WithWorldRadius sets the world boundary in chunks. Zero means unbounded.
func WithWorldRadius(chunks int) Option {
	return func(b *builder) error {
		if chunks < 0 {
			return fmt.Errorf("%w: world radius %d is negative", ErrInvalidOption, chunks)
		}
		b.settings.WorldRadius = chunks

		return nil
	}
}

// WithCompressionThreshold sets the packet size at or above which the server
// compresses. A negative value disables compression.
func WithCompressionThreshold(threshold int) Option {
	return func(b *builder) error {
		b.settings.CompressionThreshold = threshold

		return nil
	}
}

// WithPrivateKey supplies the RSA keypair the login acceptor uses. Generating
// one costs real time, so the application owns when that happens.
func WithPrivateKey(key *rsa.PrivateKey) Option {
	return func(b *builder) error {
		if key == nil {
			return fmt.Errorf("%w: nil private key", ErrInvalidOption)
		}
		b.settings.PrivateKey = key

		return nil
	}
}

// WithChunkDetail labels chunk samples with exact chunk coordinates instead of
// the 32×32 region they fall in.
//
// Cardinality: one label value per resident chunk. A world with 10,000
// resident chunks produces 10,000 series per chunk metric, against about 10
// with the region default. That is the difference between a graph and a
// memory incident in whatever is storing the series, and it is stated here
// because here is where somebody reads it at the moment they are about to turn
// it on.
//
// Use it to investigate a specific column, not as a standing configuration.
// The region label stays set either way, so a query written against regions
// keeps working while this is on.
func WithChunkDetail() Option {
	return func(b *builder) error {
		b.chunkDetail = true

		return nil
	}
}

// WithCommands replaces the command set this server dispatches.
//
// Omit it and the server answers the built-ins. Compose rather than replace
// wholesale when you only want to add one:
//
//	server.WithCommands(server.Merge(vanilla.Stubs(), server.BuiltinCommands(), mine))
//
// The later set wins for any name two of them answer to, which is what makes
// the stub list useful rather than in the way.
func WithCommands(set Set) Option {
	return func(b *builder) error {
		b.commands, b.hasCommands = set, true

		return nil
	}
}

// Authorizer decides whether a caller may run a command.
//
// It gates suggestion as well as execution: completing /ban for somebody who
// cannot run it tells them the server has one.
type Authorizer func(caller Caller, cmd *Command) bool

// AllowAll grants every command to every caller. It is the default, and it
// matches what this server did before it had an authorizer at all.
//
// That is deliberate rather than an oversight. This server has no operator
// list, and a framework milestone that silently introduced one would lock
// people out of their own worlds on upgrade.
func AllowAll() Authorizer {
	return func(Caller, *Command) bool { return true }
}

// WithAuthorizer gates command execution and suggestion.
//
// Without it every command is allowed. See AllowAll for why.
func WithAuthorizer(a Authorizer) Option {
	return func(b *builder) error {
		if a == nil {
			return fmt.Errorf("%w: nil authorizer, omit WithAuthorizer to allow everything", ErrInvalidOption)
		}
		b.authorizer = a

		return nil
	}
}
