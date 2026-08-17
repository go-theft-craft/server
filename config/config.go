package config

import (
	"crypto/rsa"
	"encoding/json"
)

// Supported world generator types.
const (
	GeneratorDefault = "default"
	GeneratorFlat    = "flat"
)

// Config holds the server configuration.
type Config struct {
	Port            int    `json:"port"`
	OnlineMode      bool   `json:"online_mode"`
	MOTD            string `json:"motd"`
	MaxPlayers      int    `json:"max_players"`
	ViewDistance    int    `json:"view_distance"`
	Seed            int64  `json:"seed"`
	GeneratorType   string `json:"generator_type"`    // a name in the generator registry
	WorldRadius     int    `json:"world_radius"`      // world boundary in chunks (0 = infinite)
	AutoSaveMinutes int    `json:"auto_save_minutes"` // auto-save interval in minutes (0 = disabled)
	MaxBuildHeight  int    `json:"max_build_height"`  // maximum Y axis (default 256)

	// GeneratorParams is the selected generator's parameters, as raw JSON.
	//
	// Raw because this package cannot name a type an application registered:
	// the factory that owns the schema is the one that parses it, and it is
	// the same shape the world's own metadata stores.
	GeneratorParams json.RawMessage `json:"generator_params,omitempty"`

	// CompressionThreshold is the packet size at or above which the server
	// compresses. A negative value disables compression entirely.
	CompressionThreshold int `json:"compression_threshold"`

	// PrivateKey is the RSA keypair the login acceptor uses. Its public half
	// is derived from it when a login needs it, so the server does not carry
	// a second, separately encoded copy that could drift.
	PrivateKey *rsa.PrivateKey `json:"-"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:            25565,
		OnlineMode:      false,
		MOTD:            "A go-theft-craft server",
		MaxPlayers:      20,
		ViewDistance:    12,
		GeneratorType:   GeneratorDefault,
		AutoSaveMinutes: 5,
		WorldRadius:     500,
		MaxBuildHeight:  256,

		// Vanilla's own default. The server had no compression at all before
		// M3, so this is new behavior rather than a migrated setting.
		CompressionThreshold: 256,
	}
}

// Merge applies file-loaded config values into cfg, but only for fields
// that were NOT explicitly set via CLI flags. explicitFlags contains the
// flag names that were explicitly provided on the command line.
func Merge(cfg *Config, fromFile *Config, explicitFlags map[string]bool) {
	if !explicitFlags["port"] {
		cfg.Port = fromFile.Port
	}
	if !explicitFlags["online-mode"] {
		cfg.OnlineMode = fromFile.OnlineMode
	}
	if !explicitFlags["motd"] {
		cfg.MOTD = fromFile.MOTD
	}
	if !explicitFlags["max-players"] {
		cfg.MaxPlayers = fromFile.MaxPlayers
	}
	if !explicitFlags["view-distance"] {
		cfg.ViewDistance = fromFile.ViewDistance
	}
	if !explicitFlags["seed"] {
		cfg.Seed = fromFile.Seed
	}
	if !explicitFlags["generator"] {
		cfg.GeneratorType = fromFile.GeneratorType
		// Parameters follow their generator: a file that names one and
		// configures it must not have the configuration applied to another.
		cfg.GeneratorParams = fromFile.GeneratorParams
	}
	if !explicitFlags["world-radius"] {
		cfg.WorldRadius = fromFile.WorldRadius
	}
	if !explicitFlags["auto-save"] {
		cfg.AutoSaveMinutes = fromFile.AutoSaveMinutes
	}
	if !explicitFlags["max-build-height"] {
		cfg.MaxBuildHeight = fromFile.MaxBuildHeight
	}
	if !explicitFlags["compression-threshold"] {
		cfg.CompressionThreshold = fromFile.CompressionThreshold
	}
}
