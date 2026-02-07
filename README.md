# minecraft-server

A Minecraft 1.8.9 (protocol 47) server implementation in Go.

## Features

- **Offline mode** — UUID v3 login, no encryption
- **Online mode** — RSA/AES-CFB8 encryption, Mojang session authentication
- **Flat stone world** — 7×7 chunk grid with block dig/place support (Creative mode)
- **Block persistence** — world state survives player reconnects
- **KeepAlive** — 30-second timeout enforcement
- **Server list** — MOTD, player count, version info
- **Codegen** — generates Go types from PrismarineJS minecraft-data JSON schemas

## Prerequisites

- [Devbox](https://www.jetify.com/devbox) (provides Go 1.24, gofumpt, golangci-lint, go-task, etc.)

## Getting Started

```bash
git clone git@github.com:OCharnyshevich/minecraft-server.git
cd minecraft-server
direnv allow   # or: devbox shell
```

## Run

```bash
# Offline mode (default)
devbox run -- task server

# Online mode (Mojang authentication)
devbox run -- task server -- -online-mode

# Custom port and MOTD
devbox run -- task server -- -port 25566 -motd "My Server"
```

Connect with a Minecraft 1.8.x client to `localhost:25565`.

## Useful Commands

| Command | Description |
|---------|-------------|
| `devbox run -- task server` | Run the server |
| `devbox run -- task test` | Run all tests with coverage |
| `devbox run -- task fmt` | Format code (gci + gofumpt) |
| `devbox run -- task lint` | Run golangci-lint |
| `devbox run -- task build` | Build binary to `build/app` |
| `devbox run -- task deps` | Download, tidy, and vendor dependencies |
| `devbox run -- task gen:dmd` | Download Minecraft data schemas |
| `devbox run -- task gen:codegen` | Generate Go types from schemas |
| `devbox run -- task cleanup` | Remove build artifacts |

Run a single test:

```bash
devbox run -- go test -mod vendor -run TestName ./path/to/package/...
```

## How to Commit

1. Format and lint before committing:
   ```bash
   devbox run -- task fmt
   devbox run -- task lint
   ```

2. Run tests:
   ```bash
   devbox run -- task test
   ```

3. Stage and commit:
   ```bash
   git add <files>
   git commit -m "Short description of the change"
   ```

All commands must be run through `devbox run --` to use the Nix-managed toolchain. Never run `go build`, `gofumpt`, or `golangci-lint` directly.

## Project Structure

```
cmd/
  server/          Minecraft server entry point
  dmd/             Minecraft Data Downloader (PrismarineJS fetcher)
  codegen/         Code generator (JSON schemas -> Go types)
internal/
  server/
    config/        Server configuration
    conn/          Connection state machine, encryption, packet handlers
    net/           Protocol I/O (VarInt, packets, marshaling)
    packet/        Packet type definitions (handshake, status, login, play)
    world/         World state, chunk generation
  gamedata/        Domain types, registries, version loader
    versions/      Generated version-specific data (via codegen)
scheme/            Downloaded Minecraft data JSON files
vendor/            Vendored Go dependencies
```

## Roadmap

- [ ] Survival mode (health, hunger, damage)
- [ ] Multi-player (entity tracking, player-to-player visibility)
- [ ] Chat commands
- [ ] World persistence (save/load to disk)
- [ ] Mob spawning and AI
- [ ] Inventory management
- [ ] Crafting
- [ ] Redstone
- [ ] Nether and End dimensions
- [ ] Plugin API

## License

[Apache License 2.0](LICENSE)
