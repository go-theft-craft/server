# minecraft-server

A Minecraft 1.8.9 (protocol 47) server implementation in Go.

## Features

- **Offline mode** — UUID v3 login, no encryption
- **Online mode** — RSA/AES-CFB8 encryption, Mojang session authentication
- **Flat stone world** — 7×7 chunk grid with block dig/place support (Creative mode)
- **Block persistence** — world state survives player reconnects
- **KeepAlive** — 30-second timeout enforcement

## Start

```bash
git clone git@github.com:OCharnyshevich/minecraft-server.git
direnv allow
devbox shell
task -l
```

## Run

```bash
# Offline mode (default)
task server

# Online mode (Mojang authentication)
task server -- -online-mode

# Custom port
task server -- -port 25566
```

## Test

```bash
task test
```