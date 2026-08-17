// Package provenance keeps the audit trail on disk.
//
// The format is newline-delimited JSON, one file per rotation, with a manifest
// naming each file's time range and a bloom filter over the item IDs it holds.
// NDJSON because a crash mid-append costs one line rather than a file, and
// because an operator with grep can answer a question this package has no
// query for.
package provenance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record is what the store writes. It mirrors server.Record field for field
// through JSON rather than importing it: the server package sits above this
// one, and the file's shape is the contract between them.
type Record = json.RawMessage

// fileName is how a rotation is named. The timestamp makes the ordering
// lexical, which is what lets a directory listing stand in for the manifest if
// the manifest is ever lost.
const (
	filePrefix   = "provenance-"
	fileSuffix   = ".ndjson"
	manifestName = "manifest.json"
)

// Store is a rotating NDJSON provenance store.
type Store struct {
	dir    string
	window time.Duration
	cap    int64
	now    func() time.Time

	mu       sync.Mutex
	current  *os.File
	written  int64
	manifest manifest

	// corrupt counts lines a reader could not parse. A crash mid-append
	// leaves a partial line, and a reader that died on it would lose every
	// record after it.
	corrupt int
}

// manifest names every file the store has written.
type manifest struct {
	Files []fileEntry `json:"files"`
}

// fileEntry is one rotation: when it covers, and a bloom filter over the item
// IDs inside it so a query for one item can skip a file without opening it.
type fileEntry struct {
	Name  string    `json:"name"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
	Bytes int64     `json:"bytes"`
	Bloom bloom     `json:"bloom"`
}

// New opens a store under dir.
//
// window is how long records are kept and cap is how many bytes the whole
// store may occupy; whichever is reached first, whole files are deleted oldest
// first. Deleting whole files rather than lines is what keeps retention from
// rewriting the trail it is pruning.
func New(dir string, window time.Duration, capBytes int64) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("provenance: create %s: %w", dir, err)
	}

	s := &Store{dir: dir, window: window, cap: capBytes, now: time.Now}
	if err := s.loadManifest(); err != nil {
		return nil, err
	}

	return s, nil
}

// SetClock replaces the store's clock, for a test that needs rotation without
// waiting a day.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Store) manifestPath() string { return filepath.Join(s.dir, manifestName) }

func (s *Store) loadManifest() error {
	raw, err := os.ReadFile(s.manifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("provenance: read manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &s.manifest); err != nil {
		return fmt.Errorf("provenance: parse manifest: %w", err)
	}

	return nil
}

func (s *Store) saveManifest() error {
	raw, err := json.MarshalIndent(s.manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("provenance: marshal manifest: %w", err)
	}
	tmp := s.manifestPath() + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("provenance: write manifest: %w", err)
	}
	if err := os.Rename(tmp, s.manifestPath()); err != nil {
		os.Remove(tmp)

		return fmt.Errorf("provenance: replace manifest: %w", err)
	}

	return nil
}

// Append writes lines to the current file, rotating first when it has to.
//
// Each line is one marshalled record; the caller has already produced them,
// because only the server package knows the record's shape.
func (s *Store) Append(_ context.Context, lines [][]byte, ids [][]uint64, stamps []time.Time) error {
	if len(lines) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, line := range lines {
		stamp := s.now()
		if i < len(stamps) && !stamps[i].IsZero() {
			stamp = stamps[i]
		}
		if err := s.rotateIfNeeded(stamp, int64(len(line)+1)); err != nil {
			return err
		}
		if _, err := s.current.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("provenance: append: %w", err)
		}
		s.written += int64(len(line) + 1)

		entry := &s.manifest.Files[len(s.manifest.Files)-1]
		entry.Bytes = s.written
		if entry.First.IsZero() || stamp.Before(entry.First) {
			entry.First = stamp
		}
		if stamp.After(entry.Last) {
			entry.Last = stamp
		}
		if i < len(ids) {
			for _, id := range ids[i] {
				entry.Bloom.add(id)
			}
		}
	}

	if err := s.current.Sync(); err != nil {
		return fmt.Errorf("provenance: sync: %w", err)
	}
	if err := s.prune(); err != nil {
		return err
	}

	return s.saveManifest()
}

// maxFileBytes is the size a rotation reaches before the next one starts. 32 MB
// is roughly 120,000 records at the sizes this server writes, which keeps a
// linear scan of one file well under a second.
const maxFileBytes = 32 << 20

// rotateIfNeeded starts a new file when the current one is full or when the
// day has turned. A day boundary is what makes a manifest readable by a human
// looking for "what happened on Tuesday".
func (s *Store) rotateIfNeeded(stamp time.Time, incoming int64) error {
	if s.current != nil {
		sameDay := len(s.manifest.Files) > 0 &&
			s.manifest.Files[len(s.manifest.Files)-1].Last.UTC().YearDay() == stamp.UTC().YearDay() &&
			s.manifest.Files[len(s.manifest.Files)-1].Last.UTC().Year() == stamp.UTC().Year()
		if sameDay && s.written+incoming <= maxFileBytes {
			return nil
		}
		if err := s.current.Close(); err != nil {
			return fmt.Errorf("provenance: close rotation: %w", err)
		}
		s.current = nil
	}

	name := fmt.Sprintf("%s%s%s", filePrefix, stamp.UTC().Format("20060102-150405.000000000"), fileSuffix)
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("provenance: open rotation: %w", err)
	}

	s.current = f
	s.written = 0
	s.manifest.Files = append(s.manifest.Files, fileEntry{Name: name, First: stamp, Last: stamp})

	return nil
}

// prune deletes whole files, oldest first, once the window or the cap says to.
func (s *Store) prune() error {
	cutoff := s.now().Add(-s.window)

	var total int64
	for _, f := range s.manifest.Files {
		total += f.Bytes
	}

	keep := make([]fileEntry, 0, len(s.manifest.Files))
	for i, f := range s.manifest.Files {
		last := i == len(s.manifest.Files)-1
		tooOld := s.window > 0 && f.Last.Before(cutoff)
		tooBig := s.cap > 0 && total > s.cap

		// The file being written is never pruned: deleting it out from under
		// the open handle would lose the records this Append just made.
		if last || (!tooOld && !tooBig) {
			keep = append(keep, f)

			continue
		}

		if err := os.Remove(filepath.Join(s.dir, f.Name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("provenance: prune %s: %w", f.Name, err)
		}
		total -= f.Bytes
	}
	s.manifest.Files = keep

	return nil
}

// Close closes the current rotation.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current == nil {
		return nil
	}
	err := s.current.Close()
	s.current = nil

	return err
}

// Corrupt is how many unparseable lines the reader has skipped, which is what
// a crash mid-append leaves behind.
func (s *Store) Corrupt() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.corrupt
}

// ErrClosed reports a store used after Close.
var ErrClosed = errors.New("provenance: store is closed")

// Scan walks every record whose file overlaps the window, newest file first,
// and calls fn with each raw line.
//
// It is a linear scan over the files the manifest says overlap, and it says so
// here so nobody builds an interactive feature on top of it. fn returning
// false stops the walk.
func (s *Store) Scan(window time.Duration, fn func(line []byte) bool) error {
	s.mu.Lock()
	files := make([]fileEntry, len(s.manifest.Files))
	copy(files, s.manifest.Files)
	cutoff := s.now().Add(-window)
	dir := s.now
	_ = dir
	s.mu.Unlock()

	// Newest first, because every query this store answers wants the most
	// recent thing that happened.
	sort.Slice(files, func(i, j int) bool { return files[i].Last.After(files[j].Last) })

	for _, f := range files {
		if window > 0 && f.Last.Before(cutoff) {
			continue
		}
		stop, err := s.scanFile(f.Name, fn)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}

	return nil
}

// ScanForItem is Scan with the bloom filter applied: a file whose filter
// excludes the ID is never opened.
func (s *Store) ScanForItem(id uint64, fn func(line []byte) bool) error {
	s.mu.Lock()
	files := make([]fileEntry, len(s.manifest.Files))
	copy(files, s.manifest.Files)
	s.mu.Unlock()

	// Oldest first: a chain reads in the order it happened.
	sort.Slice(files, func(i, j int) bool { return files[i].First.Before(files[j].First) })

	for _, f := range files {
		if !f.Bloom.mayContain(id) {
			continue
		}
		stop, err := s.scanFile(f.Name, fn)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}

	return nil
}

// FilesSkippedForItem reports how many rotations the bloom filter excluded,
// which is the only way to see the filter working.
func (s *Store) FilesSkippedForItem(id uint64) (skipped, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, f := range s.manifest.Files {
		total++
		if !f.Bloom.mayContain(id) {
			skipped++
		}
	}

	return skipped, total
}

func (s *Store) scanFile(name string, fn func(line []byte) bool) (bool, error) {
	f, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("provenance: open %s: %w", name, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// A record is a few hundred bytes; 1 MB is far past anything real and
	// stops one corrupt length from consuming the heap.
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		// A partial line from a crash is skipped and counted, so one bad
		// record does not cost every record after it.
		if !json.Valid(line) {
			s.mu.Lock()
			s.corrupt++
			s.mu.Unlock()

			continue
		}
		if !fn(append([]byte(nil), line...)) {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("provenance: read %s: %w", name, err)
	}

	return false, nil
}
