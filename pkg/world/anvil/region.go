package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-theft-craft/server/pkg/world/gen"
)

const (
	sectorSize      = 4096
	headerSectors   = 2 // location table + timestamp table
	compressionZlib = 2
)

// SaveRegion writes all provided chunks to a .mca region file.
// chunks maps chunk positions to their uncompressed NBT data.
func SaveRegion(dir string, rx, rz int, chunks map[gen.ChunkPos][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create region dir: %w", err)
	}

	// Compress all chunks with zlib.
	type chunkEntry struct {
		index      int
		compressed []byte
	}
	entries := make([]chunkEntry, 0, len(chunks))

	for pos, nbtData := range chunks {
		var cbuf bytes.Buffer
		zw, err := zlib.NewWriterLevel(&cbuf, zlib.DefaultCompression)
		if err != nil {
			return fmt.Errorf("create zlib writer: %w", err)
		}
		if _, err := zw.Write(nbtData); err != nil {
			return fmt.Errorf("compress chunk (%d,%d): %w", pos.X, pos.Z, err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("close zlib writer: %w", err)
		}

		idx := (pos.X & 31) + (pos.Z&31)*32
		entries = append(entries, chunkEntry{index: idx, compressed: cbuf.Bytes()})
	}

	// Build the file content.
	locations := make([]byte, sectorSize)
	timestamps := make([]byte, sectorSize)
	now := uint32(time.Now().Unix())

	// Each chunk's data: 4 bytes length + 1 byte compression type + compressed data,
	// padded to sector boundary.
	var dataBuf bytes.Buffer
	currentSector := uint32(headerSectors)

	for i := range entries {
		e := &entries[i]

		// Chunk payload: length (4 bytes) + compression (1 byte) + compressed NBT.
		payloadLen := uint32(len(e.compressed)) + 1 // +1 for compression byte
		totalLen := 4 + payloadLen                  // 4 for the length field itself
		sectorCount := (totalLen + sectorSize - 1) / sectorSize

		// Write location entry: (offset << 8) | sectorCount
		off := e.index * 4
		binary.BigEndian.PutUint32(locations[off:off+4],
			(currentSector<<8)|uint32(sectorCount&0xFF))

		// Write timestamp.
		binary.BigEndian.PutUint32(timestamps[off:off+4], now)

		// Write chunk data to buffer.
		var header [5]byte
		binary.BigEndian.PutUint32(header[0:4], payloadLen)
		header[4] = compressionZlib
		dataBuf.Write(header[:])
		dataBuf.Write(e.compressed)

		// Pad to sector boundary.
		paddedSize := int(sectorCount) * sectorSize
		if pad := paddedSize - int(totalLen); pad > 0 {
			dataBuf.Write(make([]byte, pad))
		}

		currentSector += uint32(sectorCount)
	}

	// Write the file atomically.
	path := filepath.Join(dir, fmt.Sprintf("r.%d.%d.mca", rx, rz))
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp region file: %w", err)
	}
	defer func() {
		f.Close()
		os.Remove(tmp)
	}()

	if _, err := f.Write(locations); err != nil {
		return fmt.Errorf("write locations: %w", err)
	}
	if _, err := f.Write(timestamps); err != nil {
		return fmt.Errorf("write timestamps: %w", err)
	}
	if _, err := f.Write(dataBuf.Bytes()); err != nil {
		return fmt.Errorf("write chunk data: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close region file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename region file: %w", err)
	}

	return nil
}
