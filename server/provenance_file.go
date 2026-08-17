package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-theft-craft/server/internal/server/provenance"
	"github.com/go-theft-craft/server/pkg/world"
)

// FileProvenance keeps the audit trail as rotating newline-delimited JSON
// under dir.
//
// window is how long records are kept and capBytes is how much disk the whole
// trail may use; whichever is reached first, whole files are deleted oldest
// first. Both are limits an operator picks, because what a trail is worth
// keeping is not something this package can know.
//
// The records hold player names and UUIDs. They are local runtime data, in the
// data directory, and nothing here sends them anywhere.
func FileProvenance(dir string, window time.Duration, capBytes int64) (ProvenanceStore, error) {
	store, err := provenance.New(dir, window, capBytes)
	if err != nil {
		return nil, err
	}

	return &fileProvenance{store: store}, nil
}

type fileProvenance struct{ store *provenance.Store }

// Append marshals each record and hands the store its lines, together with the
// item IDs it should index and the timestamps it should rotate by.
func (p *fileProvenance) Append(ctx context.Context, records []Record) error {
	lines := make([][]byte, 0, len(records))
	ids := make([][]uint64, 0, len(records))
	stamps := make([]time.Time, 0, len(records))

	for _, rec := range records {
		line, err := marshalRecord(rec)
		if err != nil {
			return err
		}
		lines = append(lines, line)
		stamps = append(stamps, rec.At)

		raw := make([]uint64, 0, len(rec.Items))
		for _, id := range rec.Items {
			raw = append(raw, uint64(id))
		}
		ids = append(ids, raw)
	}

	return p.store.Append(ctx, lines, ids, stamps)
}

// AtPosition is every record about one block, newest first.
//
// It is a linear scan of the files the manifest says overlap the window.
// Nothing interactive should be built on it.
func (p *fileProvenance) AtPosition(_ context.Context, pos world.BlockPos, window time.Duration) ([]Record, error) {
	return p.collect(window, func(rec Record) bool {
		return rec.Pos == pos ||
			(rec.From.Kind == LocationContainer && rec.From.Block == pos) ||
			(rec.To.Kind == LocationContainer && rec.To.Block == pos) ||
			(rec.From.Kind == LocationWorld && rec.From.Block == pos) ||
			(rec.To.Kind == LocationWorld && rec.To.Block == pos)
	})
}

// ByActor is every record one actor caused, newest first. Also a linear scan.
func (p *fileProvenance) ByActor(_ context.Context, uuid string, window time.Duration) ([]Record, error) {
	return p.collect(window, func(rec Record) bool { return rec.Actor.UUID == uuid })
}

// ForItem is every record about one item, oldest first, which is the order a
// chain reads in. The bloom filter keeps it from opening files the item was
// never in.
func (p *fileProvenance) ForItem(_ context.Context, id ItemID) ([]Record, error) {
	var out []Record
	var failure error

	err := p.store.ScanForItem(uint64(id), func(line []byte) bool {
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			failure = fmt.Errorf("provenance: parse record: %w", err)

			return false
		}
		for _, held := range rec.Items {
			if held == id {
				out = append(out, rec)

				break
			}
		}

		return true
	})
	if err != nil {
		return nil, err
	}

	return out, failure
}

func (p *fileProvenance) collect(window time.Duration, keep func(Record) bool) ([]Record, error) {
	var out []Record
	var failure error

	err := p.store.Scan(window, func(line []byte) bool {
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			failure = fmt.Errorf("provenance: parse record: %w", err)

			return false
		}
		if keep(rec) {
			out = append(out, rec)
		}

		return true
	})
	if err != nil {
		return nil, err
	}

	return out, failure
}

func (p *fileProvenance) Close() error { return p.store.Close() }

// ProvenanceCorruptLines is how many unparseable lines the store has skipped.
//
// A crash mid-append leaves a partial line, and a reader that died on it would
// lose every record after it. This is how an operator sees that happened.
func ProvenanceCorruptLines(store ProvenanceStore) int {
	fp, ok := store.(*fileProvenance)
	if !ok {
		return 0
	}

	return fp.store.Corrupt()
}

// ProvenanceFilesSkipped reports how many rotations the bloom filter excluded
// for an item, and how many there are.
//
// It is the only way to see the filter working: the answer to ForItem is exact
// either way, and only the work changes.
func ProvenanceFilesSkipped(store ProvenanceStore, id ItemID) (skipped, total int) {
	fp, ok := store.(*fileProvenance)
	if !ok {
		return 0, 0
	}

	return fp.store.FilesSkippedForItem(uint64(id))
}
