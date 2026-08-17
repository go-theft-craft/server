# M11.3 storage measurements

**Date:** 2026-08-17
**Machine:** AMD Ryzen 9 9950X (16 cores), Linux, local SSD, `t.TempDir()`
**How to reproduce:**

```bash
M11_MEASURE=1 devbox run -- go test -mod vendor -run 'TestStorage' -v ./server/
```

The M11.3 design set three thresholds. Crossing any of them reopens the
question of whether the vanilla Anvil format is the right thing to keep a world
in. None of them is crossed.

| Metric | Threshold | Measured | Verdict |
| --- | --- | --- | --- |
| Incremental save of 100 dirty chunks | > 250 ms | **45.5 ms** | passes |
| Cold load of a 25-chunk view | > 500 ms | **20.0 ms** | passes |
| Bytes on disk per chunk, mean | > 3× the section data | **4,148 bytes vs ~39,000** (0.11×) | passes |

The world for the first two is 9,801 resident chunks — a 99×99 square, which is
the plan's "10,000 resident chunks" — generated flat so that 10,000 columns fit
in memory. The third is measured on 625 chunks of generated terrain, because
flat terrain is one section per column and compresses unusually well.

## The first measurement was a failure, and it was not the format

The first run of the incremental save took **300 ms across two regions**, over
the threshold. Breaking it down by how the dirty chunks were spread said what
the cost actually was:

| Case | Time |
| --- | --- |
| 100 dirty chunks across 2 regions | 300 ms |
| 100 dirty chunks inside 1 region | 149 ms |

A region is the unit of write — 1,024 columns in one file — so the price was
per region and not per chunk. That much is the format's, and vanilla pays it
too. What was *not* the format's was what the save did inside a region: it
decompressed all 1,024 columns to a tree of NBT, then re-compressed all 1,024,
in order to change one of them.

Carrying the untouched columns through still compressed (`anvil.Payload`, and
`Region.Payloads` returning zlib bytes rather than decoded documents) removed
that:

| Case | Before | After |
| --- | --- | --- |
| 100 dirty chunks across 2 regions | 300 ms | **45.5 ms** |
| 100 dirty chunks inside 1 region | 149 ms | **23.5 ms** |

6.6× faster, and the threshold is no longer close. The measurement was worth
taking for this alone: the plan expected the number to either pass or
reopen a format question, and it did neither — it found an implementation
mistake that a passing number would have hidden.

## Bytes on disk

On 625 chunks of generated terrain with caves, ores, and trees:

- 2,592,768 bytes of region file, **4,148 bytes per chunk**
- 2,975 non-empty sections resident, 4.76 per chunk
- a section is 8,192 bytes on the wire, so a chunk's block data is about
  39,000 bytes there

Disk is roughly **0.11×** the wire form. zlib on block data that is mostly runs
of stone is doing nearly all of that, and it is the reason the threshold is not
in danger.

The in-memory figure is a different number again and worth stating so nobody
compares against the wrong one: `world.State` is a `uint32`, so 2,975 resident
sections are 48.7 MB of block data in this process, twice their wire size. That
is M11.2's finding, recorded there, and not what this threshold is about.

## Full save

A full save of all 9,801 chunks takes about 1.5 s. It is not one of the
thresholds, and it only happens on the first save of a world — every autosave
after it writes the regions that moved.

## What is still unmeasured

The measurements run against `t.TempDir()` on a local SSD. Nothing here says
what a network filesystem or a slow disk does to the numbers, and the region
rewrite is a whole-file write, so a slow disk would show up in the first
measurement before anywhere else.
