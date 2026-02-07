# minecraft-server

A Minecraft 1.8.9 (protocol 47) server implementation in Go.

## Features

- **Offline mode** — UUID v3 login, no encryption
- **Online mode** — RSA/AES-CFB8 encryption, Mojang session authentication
- **Procedural world generation** — Perlin noise terrain with 11 biomes, caves, ores, and trees
- **Flat world generator** — Classic bedrock/stone/grass layers
- **Dynamic chunk loading** — View-distance-based loading/unloading with optional world boundary
- **Block interaction** — Dig and place blocks with broadcast and persistence
- **Multiplayer** — Player spawning, entity tracking, visibility streaming, movement sync
- **Chat & commands** — `/tp`, `/gamemode`, `/time`, `/help`, `/list`, `/say`, `/me`, `/kill`, `/seed`
- **Inventory** — 36-slot hotbar, 4-slot armor, held item switching, item dropping
- **Persistence** — Auto-save world state and player data (position, inventory, gamemode)
- **KeepAlive** — 30-second timeout enforcement
- **Server list** — MOTD, player count, version info
- **Codegen** — Generates Go types from PrismarineJS minecraft-data JSON schemas

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

# Flat world with a seed
devbox run -- task server -- -generator flat -seed 42

# Limit world radius (in chunks)
devbox run -- task server -- -world-radius 32
```

Connect with a Minecraft 1.8.x client to `localhost:25565`.

### Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | 25565 | Server listening port |
| `-online-mode` | false | Enable Mojang authentication + encryption |
| `-motd` | "A Minecraft Server" | Server description |
| `-max-players` | 20 | Max players shown in server list |
| `-view-distance` | 8 | Chunk view distance |
| `-seed` | 0 | World generation seed |
| `-generator` | "default" | World generator: `default` or `flat` |
| `-world-radius` | 0 (infinite) | World boundary in chunks |

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

## Architecture

### High-Level Overview

```mermaid
graph TB
    subgraph CLI["Command-Line Tools"]
        SERVER["cmd/server<br/>Entry point"]
        DMD["cmd/dmd<br/>Data downloader"]
        CODEGEN["cmd/codegen<br/>Code generator"]
    end

    subgraph External["External"]
        PRISMARINE["PrismarineJS<br/>minecraft-data<br/>(GitHub)"]
    end

    subgraph Data["Data Layer"]
        SCHEME["scheme/<br/>pc-1.8/<br/>JSON schemas"]
        TEMPLATES["Go templates"]
    end

    subgraph GameData["internal/gamedata"]
        FACADE["GameData facade"]
        REGISTRIES["Registries<br/>(ByID, ByName, All)"]
        VERSIONS["versions/pc_1_8/<br/>(generated)"]
    end

    subgraph Server["internal/server"]
        CONFIG["config<br/>Port, MOTD, online-mode,<br/>max-players, view-dist"]
        CONN["conn<br/>Connection state machine,<br/>encryption, packet handlers"]
        NET["net<br/>VarInt/VarLong encoding,<br/>Marshal/Unmarshal,<br/>Packet Read/Write"]
        PLAYER["player<br/>Player state, inventory,<br/>entity tracking, broadcasts"]
        WORLD["world<br/>Chunk cache, block overrides,<br/>dynamic loading"]
        GEN["world/gen<br/>FlatGenerator,<br/>DefaultGenerator<br/>(noise, biomes, caves,<br/>ores, trees)"]
        STORAGE["storage<br/>JSON file persistence"]
    end

    DMD -->|fetches| PRISMARINE
    PRISMARINE -->|JSON| SCHEME
    CODEGEN -->|reads| SCHEME
    CODEGEN -->|reads| TEMPLATES
    CODEGEN -->|generates| VERSIONS
    VERSIONS --> FACADE
    REGISTRIES --> FACADE

    SERVER --> CONFIG
    SERVER --> CONN
    CONN --> NET
    CONN --> PLAYER
    CONN --> WORLD
    WORLD --> GEN
    SERVER --> STORAGE
    CONN -.->|packet structs| FACADE
```

### Connection Lifecycle

```mermaid
sequenceDiagram
    participant C as Client
    participant S as Server
    participant W as World
    participant PM as PlayerManager

    C->>S: Handshake (protocol 47)

    alt Status Request
        C->>S: Status Request
        S->>C: MOTD, players, version
        C->>S: Ping
        S->>C: Pong
    end

    C->>S: Login Start (username)

    alt Online Mode
        S->>C: Encryption Request (RSA public key)
        C->>S: Encryption Response (shared secret)
        Note over S: Enable AES-CFB8 encryption
        Note over S: Verify with Mojang session server
    end

    S->>C: Login Success (UUID, username)
    S->>C: Join Game
    S->>C: Spawn Position
    S->>C: Player Abilities
    S->>C: Player Position And Look

    S->>W: Generate chunks around player
    W-->>S: Chunk data
    S->>C: Map Chunk (xN)

    S->>PM: Register player
    PM-->>C: PlayerInfo (tab list)
    PM-->>C: Spawn nearby players

    loop Every 15s
        S->>C: KeepAlive
        C->>S: KeepAlive echo
    end
```

### World Generation Pipeline

```mermaid
graph TD
    A["Terrain Noise<br/>Perlin + detail noise,<br/>biome-specific height scaling"] --> B["Terrain Fill<br/>Bedrock (y=0-3) → Stone →<br/>Surface layers → Water (y≤62)"]
    B --> C["Cave Carving<br/>Cellular automata<br/>through stone"]
    C --> D["Ore Placement<br/>Coal, iron, gold, diamond,<br/>redstone, lapis<br/>with depth constraints"]
    D --> E["Tree & Vegetation<br/>Biome-specific placement<br/>and decoration"]
    E --> F["Chunk Ready"]

    subgraph Biomes
        B1["Ocean"]
        B2["Plains"]
        B3["Forest"]
        B4["Desert"]
        B5["Jungle"]
        B6["Mountains"]
        B7["Taiga"]
        B8["Savanna"]
        B9["Beach"]
        B10["Snow Tundra"]
        B11["Dark Forest"]
    end

    A -.->|selects| Biomes
```

### Play State Packet Handling

```mermaid
graph LR
    IN["Client Packet"] --> D{Packet ID}
    D -->|0x00| KA["KeepAlive<br/>echo response"]
    D -->|0x01| CHAT["Chat Message<br/>command dispatch<br/>or broadcast"]
    D -->|0x03| PG["Player<br/>ground state"]
    D -->|0x04| PP["Player Position<br/>movement"]
    D -->|0x05| PL["Player Look<br/>yaw/pitch"]
    D -->|0x06| PPL["Position And Look<br/>combined"]
    D -->|0x07| BD["Block Dig<br/>break/drop items"]
    D -->|0x08| BP["Block Place<br/>place block"]
    D -->|0x09| HI["Held Item Change<br/>slot selection"]
    D -->|0x0A| ANIM["Animation<br/>arm swing"]
    D -->|0x0B| EA["Entity Action<br/>sneak/sprint"]
    D -->|0x15| CS["Client Settings<br/>skin parts"]
```

## Project Structure

```
cmd/
  server/          Minecraft server entry point
  dmd/             Minecraft Data Downloader (PrismarineJS fetcher)
  codegen/         Code generator (JSON schemas -> Go types)
internal/
  server/
    config/        Server configuration and CLI flags
    conn/          Connection state machine, encryption, packet handlers, commands
    net/           Protocol I/O (VarInt, packets, marshaling)
    packet/        Packet type definitions (handshake, status, login, play)
    player/        Player state, inventory, entity tracking, broadcasts
    world/         World state, chunk cache, dynamic loading
      gen/         World generators (default, flat, noise, biomes, caves, ores)
    storage/       File persistence (JSON) for world and player data
  gamedata/        Domain types, registries, version loader
    versions/      Generated version-specific data (via codegen)
scheme/            Downloaded Minecraft data JSON files
vendor/            Vendored Go dependencies
```

## Chat Commands

| Command | Description |
|---------|-------------|
| `/help` | List available commands |
| `/list` | Show online players |
| `/tp <player>` | Teleport to a player |
| `/tp <x> <y> <z>` | Teleport to coordinates |
| `/gamemode <mode>` | Switch game mode (survival, creative, adventure, spectator) |
| `/time set <value>` | Set world time (day, night, noon, midnight, or number) |
| `/say <message>` | Broadcast server announcement |
| `/me <action>` | Send action message |
| `/kill` | Respawn player |
| `/seed` | Show world seed |

## Persistence

The server auto-saves every 5 minutes and on shutdown.

```
storage/
├── config.json              # Server config
├── world/
│   └── overrides.json       # Player-made block modifications
└── players/
    ├── <uuid>.json          # Position, gamemode, inventory per player
    └── ...
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
- Player movement, look, sneaking, sprinting
- Block dig and place with broadcast to other players
- Chat messaging and commands
- Multiplayer: player spawning, entity tracking, visibility streaming
- Inventory: hotbar, armor, held item, item dropping
- Procedural world generation with biomes, caves, ores, trees
- Dynamic chunk loading/unloading
- World and player data persistence
- KeepAlive with 30s timeout

### What's missing

**Entity Interaction & Combat** — No PvP, no mob combat. Missing: `use_entity`, `entity_equipment`, `entity_velocity`, `entity_metadata`, `entity_effect`, `update_attributes`, `combat_event`, `update_health`.

**World Features** — No day/night, weather, sounds, or particles. Missing: `update_time`, `game_state_change`, `spawn_entity_weather`, `world_border`, `explosion`, `named_sound_effect`, `world_particles`, `world_event`, `block_action`, `block_break_animation`, `map_chunk_bulk`, `tile_entity_data`.

**Mobs & NPCs** — No mob spawning or AI. Missing: `spawn_entity_living`, `spawn_entity`, `spawn_entity_painting`, `spawn_entity_experience_orb`, `attach_entity`, `collect`, `entity_status`.

**Scoreboard & Teams** — Missing: `scoreboard_objective`, `scoreboard_score`, `scoreboard_display_objective`, `scoreboard_team`.

**UI & Misc** — Missing: `tab_complete`, `update_sign`, `title`, `playerlist_header`, `statistics`, `map`, `camera`, `custom_payload`, `resource_pack_send`, `difficulty`.

## Roadmap

1. **Entity interaction** — use_entity, arm animation, equipment display
2. **World ambience** — day/night cycle, weather, sounds, particles
3. **Mob spawning** — living entities, AI, health, combat
4. **Tile entities** — signs, chests, banners
5. **Scoreboard & Teams** — sidebar scores, team colors
6. **Plugin channels** — custom_payload support

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

## License

[Apache License 2.0](LICENSE)
