package server

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-theft-craft/server/config"
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
	settings  *config.Config
	log       *slog.Logger
	generator gen.Generator
	store     Store
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
