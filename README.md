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

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Command-Line Tools                             │
│                                                                         │
│  cmd/server             cmd/dmd              cmd/codegen                │
│  Entry point            Data downloader      Code generator             │
│       │                      │                  │    │                  │
└───────┼──────────────────────┼──────────────────┼────┼──────────────────┘
        │                      │                  │    │
        ▼                      ▼                  │    │
┌──────────────────┐   ┌──────────────┐           │    │
│  internal/server │   │ PrismarineJS │           │    │
│                  │   │ minecraft-   │           │    │
│  Server          │   │ data (GitHub)│           │    │
│  TCP listener,   │   └──────┬───────┘           │    │
│  orchestration   │          │                   │    │
└──┬──┬──┬──┬──────┘          ▼                   │    │
   │  │  │  │         ┌──────────────┐            │    │
   │  │  │  │         │  scheme/     │◄───────────┘    │
   │  │  │  │         │  pc-1.8/     │                 │
   │  │  │  │         │  JSON schemas│                 │
   │  │  │  │         └──────────────┘                 │
   │  │  │  │                                          │
   │  │  │  │         ┌──────────────┐                 │
   │  │  │  │         │  Go templates│◄────────────────┘
   │  │  │  │         └──────┬───────┘
   │  │  │  │                │ generates
   │  │  │  │                ▼
   │  │  │  │    ┌─────────────────────────────────────────────────┐
   │  │  │  │    │              internal/gamedata                   │
   │  │  │  │    │                                                 │
   │  │  │  │    │  GameData facade ◄── Registry interfaces        │
   │  │  │  │    │  (Blocks, Items,     (ByID, ByName, All)        │
   │  │  │  │    │   Entities ...)      Version Loader             │
   │  │  │  │    │                         │                       │
   │  │  │  │    │           ┌─────────────┘                       │
   │  │  │  │    │           ▼                                     │
   │  │  │  │    │  versions/pc_1_8/  (generated)                  │
   │  │  │  │    │  Registries, packet structs, protocol metadata  │
   │  │  │  │    └───────────────────────────┬─────────────────────┘
   │  │  │  │                                │
   │  │  │  │      ··· packet structs ·······╂····················
   │  │  │  │                                │                   :
   ▼  ▼  ▼  ▼                                ▼                   :
┌──────────────────────────────────────────────────────────────────┐
│                    internal/server (core)                         │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ conn — Connection State Machine                             │ │
│  │                                                             │ │
│  │  Handshake ──► Status (MOTD, ping/pong)                     │ │
│  │      │                                                      │ │
│  │      └──────► Login ──► Play                                │ │
│  │               offline UUID    movement, blocks,             │ │
│  │               RSA+AES auth    chat, multiplayer              │ │
│  │               Mojang verify                                 │ │
│  └──────────┬──────────────────────────────────────────────────┘ │
│             │ uses                                                │
│  ┌──────────▼──────────────────┐  ┌───────────────────────────┐  │
│  │ net — Protocol I/O          │  │ config                    │  │
│  │                             │  │ Port, MOTD, online-mode,  │  │
│  │  VarInt/VarLong encoding    │  │ max-players, view-dist,   │  │
│  │  Marshal/Unmarshal (mc: tag)│  │ RSA keys                  │  │
│  │  Packet Read/Write          │  └───────────────────────────┘  │
│  └─────────────────────────────┘                                 │
│                                                                  │
│  ┌────────────────────────────┐  ┌────────────────────────────┐  │
│  │ player                     │  │ world                      │  │
│  │                            │  │                            │  │
│  │  Player                    │  │  World                     │  │
│  │  position, entity state,   │  │  chunk cache + block       │  │
│  │  UUID, skin properties     │  │  overrides (persistence)   │  │
│  │                            │  │                            │  │
│  │  Manager                   │  │  Chunk Encoder             │  │
│  │  entity ID allocation,     │  │  section bitmask, blocks,  │  │
│  │  tracking, broadcasts,     │  │  light data, biomes        │  │
│  │  tab list, visibility      │  │                            │  │
│  └────────────────────────────┘  │  ┌──────────────────────┐  │  │
│                                  │  │ gen — World Gen       │  │  │
│                                  │  │                      │  │  │
│                                  │  │  FlatGenerator       │  │  │
│                                  │  │  bedrock/stone/grass │  │  │
│                                  │  │                      │  │  │
│                                  │  │  DefaultGenerator    │  │  │
│                                  │  │  Perlin noise,       │  │  │
│                                  │  │  caves, ores, trees, │  │  │
│                                  │  │  biomes, surface     │  │  │
│                                  │  └──────────────────────┘  │  │
│                                  └────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### Connection Lifecycle

```
Client                  Server               World          PlayerManager
  │                       │                    │                  │
  │── Handshake ─────────►│                    │                  │
  │   (protocol 47)       │                    │                  │
  │                       │                    │                  │
  ├─── Status? ──────────►│                    │                  │
  │◄── MOTD, players ─────┤                    │                  │
  │── Ping ──────────────►│                    │                  │
  │◄── Pong ──────────────┤                    │                  │
  │                       │                    │                  │
  ├─── Login Start ──────►│                    │                  │
  │   (username)          │                    │                  │
  │                       │                    │                  │
  │   ┌ online mode ──────────────────┐        │                  │
  │◄──┤ Encryption Request (RSA key)  │        │                  │
  │───┤ Encryption Response (secret)  │        │                  │
  │   │ → enable AES/CFB8             │        │                  │
  │   │ → verify with Mojang          │        │                  │
  │   └───────────────────────────────┘        │                  │
  │                       │                    │                  │
  │◄── Login Success ─────┤                    │                  │
  │    (UUID, username)   │                    │                  │
  │                       │                    │                  │
  │◄── Join Game ─────────┤                    │                  │
  │◄── Spawn Position ────┤                    │                  │
  │◄── Player Abilities ──┤                    │                  │
  │◄── Player Position ───┤                    │                  │
  │                       │── generate grid ──►│                  │
  │◄── Map Chunk (×N) ────────────────────────┤                  │
  │                       │                    │                  │
  │                       │── register ───────────────────────────►│
  │◄── PlayerInfo (tab) ──────────────────────────────────────────┤
  │◄── Spawn nearby ──────────────────────────────────────────────┤
  │                       │                    │                  │
  │── KeepAlive echo ────►│ (every 5s)        │                  │
  │◄── KeepAlive ─────────┤                    │                  │
  │         ...           │                    │                  │
```

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

## Protocol Coverage

Minecraft 1.8.8 (protocol 47) implementation status:

| Category | Implemented | Total | Coverage |
|---|---|---|---|
| Handshake | 1 | 1 | 100% |
| Status | 4 | 4 | 100% |
| Login | 4 | 4 | 100% |
| Play (server-bound) | 9 | 26 | 35% |
| Play (client-bound) | 17 | 74 | 23% |
| **Total** | **35** | **109** | **32%** |

### What works

- Full connection lifecycle: handshake, status ping, login (offline + online mode)
- Player movement and look (position, flying, position_look)
- Block dig and place with broadcast to other players
- Chat messaging (broadcast)
- Multiplayer: player spawning, entity tracking, visibility streaming
- KeepAlive with 30s timeout
- Client settings (logged)

### What's missing

**Inventory & Items** — No inventory management. Missing: `held_item_slot`, `window_click`, `window_items`, `set_slot`, `set_creative_slot`, `open_window`, `transaction`, `craft_progress_bar`.

**Entity Interaction & Combat** — No PvP, no mob combat. Missing: `use_entity`, `arm_animation`, `entity_equipment`, `entity_velocity`, `entity_metadata`, `entity_effect`, `update_attributes`, `combat_event`, `update_health`.

**Player Actions** — No sneak, sprint, or respawn. Missing: `entity_action`, `client_command`, `abilities`.

**World Features** — No day/night, weather, sounds, or particles. Missing: `update_time`, `game_state_change`, `spawn_entity_weather`, `world_border`, `explosion`, `named_sound_effect`, `world_particles`, `world_event`, `block_action`, `block_break_animation`, `map_chunk_bulk`, `tile_entity_data`.

**Mobs & NPCs** — No mob spawning or AI. Missing: `spawn_entity_living`, `spawn_entity`, `spawn_entity_painting`, `spawn_entity_experience_orb`, `attach_entity`, `collect`, `entity_status`.

**Scoreboard & Teams** — Missing: `scoreboard_objective`, `scoreboard_score`, `scoreboard_display_objective`, `scoreboard_team`.

**UI & Misc** — Missing: `tab_complete`, `update_sign`, `title`, `playerlist_header`, `statistics`, `map`, `camera`, `custom_payload`, `resource_pack_send`, `difficulty`.

## Roadmap

1. **Inventory system** — held item switching, creative inventory, window clicks
2. **Entity interaction** — use_entity, arm animation, equipment display
3. **Player actions** — sneak/sprint, respawn, abilities
4. **World ambience** — day/night cycle, weather, sounds, particles
5. **Mob spawning** — living entities, AI, health, combat
6. **Tile entities** — signs, chests, banners
7. **Scoreboard & Teams** — sidebar scores, team colors
8. **Plugin channels** — custom_payload support

## License

[Apache License 2.0](LICENSE)
