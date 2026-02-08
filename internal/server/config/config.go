package config

import "crypto/rsa"

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
	GeneratorType   string `json:"generator_type"`    // "default" or "flat"
	WorldRadius     int    `json:"world_radius"`      // world boundary in chunks (0 = infinite)
	AutoSaveMinutes int    `json:"auto_save_minutes"` // auto-save interval in minutes (0 = disabled)
	MaxBuildHeight  int    `json:"max_build_height"`  // maximum Y axis (default 256)

	// RSA keypair for online-mode encryption handshake.
	PrivateKey   *rsa.PrivateKey `json:"-"`
	PublicKeyDER []byte          `json:"-"`
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
}
