package config

// Config holds the server configuration.
type Config struct {
	Port       int
	OnlineMode bool
	MOTD       string
	MaxPlayers int
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
