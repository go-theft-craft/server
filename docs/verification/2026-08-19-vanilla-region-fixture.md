# The vanilla-written region fixture

`pkg/world/anvil/testdata/r.0.0.mca` is nine chunks written by a vanilla
Java Edition 1.8.9 server. It is what `TestAVanillaRegionReads` and
`TestAVanillaChestReads` read, and M11.3 recorded its absence rather than
ticking it away: until it existed, every claim this package made about the
Anvil format was checked against this package's own writer.

## What it is

| | |
| --- | --- |
| Server | vanilla 1.8.9, `Starting minecraft server version 1.8.9` |
| World | freshly generated, `level-type=DEFAULT`, `level-seed=1848` |
| Chunks | 9, chunk coordinates 3–5 by 11–13, from region 0,0 |
| Edits | one chest, at block 72,80,200, placed through the server's own `/setblock` |
| Players | none. No client ever connected, and `world/playerdata/` is empty |

## How to make it again

The jar is not in this repository and must not be. Any 1.8.9 server jar
verified against Mojang's manifest will do; `minecraft-reference` is the
project's pipeline for obtaining one.

```sh
# A directory holding server.jar, eula.txt (eula=true), and:
cat > server.properties <<'EOF'
level-name=world
level-type=DEFAULT
level-seed=1848
online-mode=false
server-ip=127.0.0.1
server-port=25700
EOF

java -Xms256M -Xmx2G \
  -Dio.netty.transport.noNative=true -Dio.netty.noUnsafe=true \
  -jar server.jar nogui
```

The two netty properties are what let a server of this age run on a modern
JVM, and they are the same two `headless-minecraft`'s vanilla lane passes.

Wait for `Done (`, then on the server console:

```
setblock 72 80 200 minecraft:chest 2 replace {Items:[{id:"minecraft:stone",Count:64b,Slot:0b},{id:"minecraft:diamond_pickaxe",Count:1b,Slot:13b,Damage:42s}]}
save-all
stop
```

**`/setblock` does not generate a chunk.** In 1.8 it refuses one that is not
loaded — `Cannot place block outside of the world` — so the fixture is cut
from the 25×25 spawn area the server generates by itself, and the chest has to
go inside it. That area straddles the region boundary: 225 of its chunks
landed in region 0,0 and the rest in region -1,0. That is why the chest is at 72,80,200 rather than near the origin.

## Why nine chunks and not 225

The region file that server wrote is 1.5 MB. The largest file otherwise
tracked here is 83 KB, so committing it would have made the repository's
biggest file by eighteen times, to prove something nine chunks prove.

Nine chunks were kept by rewriting the 8 KiB header — the sector offsets have
to name where the payloads landed — and copying each kept chunk's payload
byte for byte: its four-byte length, its compression-scheme byte, and its
zlib stream, exactly as vanilla wrote them. Nothing in a chunk was re-encoded,
so every byte the NBT reader, the section decoder, and the tile-entity decoder
see is vanilla's.

**What that costs, stated rather than hidden:** the location table is this
project's layout of vanilla's payloads, so the fixture is not proof that the
reader handles a location table it did not lay out. The compressed payloads,
their length and scheme prefixes, and all of the NBT are untouched, and that
is where the format's difficulty lives.

## What it was checked for before it was committed

Every printable run of four or more characters in the nine decompressed
chunks, 106 distinct strings in total: NBT tag names (`Level`, `Sections`,
`TileEntities`, `Blocks`, `Data`, `BlockLight`, `SkyLight`, `Biomes`,
`HeightMap`, `TileTicks`, `Entities`, `xPos`, `zPos`, and the chest's
`Chest`, `Items`, `Slot`, `Count`, `Damage`, `Lock`), the block names
in the tile-tick queue (`minecraft:gravel`, `minecraft:flowing_water`,
`minecraft:flowing_lava`), the two item names in the chest, and coincidental
runs of printable bytes inside the block and light arrays.

No username, no UUID, no IP address, no session token. The `Entities` lists
are empty: nothing had time to spawn, and no player was ever there to keep
one loaded.
