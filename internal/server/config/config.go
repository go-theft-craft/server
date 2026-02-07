package config

import "crypto/rsa"

// Config holds the server configuration.
type Config struct {
	Port       int
	OnlineMode bool
	MOTD       string
	MaxPlayers int

	// RSA keypair for online-mode encryption handshake.
	PrivateKey   *rsa.PrivateKey
	PublicKeyDER []byte
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:       25565,
		OnlineMode: false,
		MOTD:       "A Minecraft Server",
		MaxPlayers: 20,
	}
}
