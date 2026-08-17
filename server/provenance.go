package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-theft-craft/server/pkg/world"
)

// Provenance: the record of what happened to a block or an item.
//
// Recording runs off the tick. A caller hands a record to the Recorder and
// carries on; a writer goroutine drains a bounded queue into a store. When the
// queue is full the record is dropped and counted rather than blocking the
// tick, because a stalled world is a worse failure than a gap in an audit
// trail — unless an operator says otherwise, which ProvenanceOverflowBlocks
// is for.
//
// Everything here is off unless an application asks for it.

// RecordKind is what a record is about.
type RecordKind string

// The two kinds.
const (
	RecordItem  RecordKind = "item"
	RecordBlock RecordKind = "block"
)

// Reason is why something happened. It is a closed vocabulary so that a query
// can filter on it, and a string so that a log line reads.
type Reason string

// The reasons this server records.
const (
	ReasonMint      Reason = "mint"
	ReasonMove      Reason = "move"
	ReasonRetire    Reason = "retire"
	ReasonPlace     Reason = "place"
	ReasonBreak     Reason = "break"
	ReasonCraft     Reason = "craft"
	ReasonDrop      Reason = "drop"
	ReasonPickup    Reason = "pickup"
	ReasonExpire    Reason = "expire"
	ReasonSpill     Reason = "spill"
	ReasonDuplicate Reason = "duplicate"
	ReasonReconcile Reason = "reconcile"
)

// maxCauseDepth bounds a cause chain.
//
// A chain is what links a block break to the mob that caused it to the player
// who lit the mob. Bounding it is what stops a cycle — a chain that referred
// to itself — from being written forever.
const maxCauseDepth = 8

// Record is one thing that happened.
//
// Blocks are named canonically — "minecraft:chest" — never by handle: a handle
// is meaningful only to the process that minted it, and a record outlives the
// process.
type Record struct {
	At     time.Time  `json:"at"`
	Kind   RecordKind `json:"kind"`
	Reason Reason     `json:"reason"`
	Actor  Actor      `json:"actor"`

	// From and To are where an item went. A mint has no From and a retirement
	// has no To.
	From Location `json:"from,omitempty"`
	To   Location `json:"to,omitempty"`

	// Items is what moved, for an item record.
	Items []ItemID `json:"items,omitempty"`

	// Block and Pos are what changed, for a block record.
	Block string         `json:"block,omitempty"`
	Pos   world.BlockPos `json:"pos,omitempty"`

	// Cause is why this happened, outermost first, bounded at maxCauseDepth.
	Cause []Reason `json:"cause,omitempty"`

	// Note carries what a reason cannot: the text of a detected duplication,
	// the count a reconciliation found.
	Note string `json:"note,omitempty"`
}

// bound trims a record's cause chain to the depth limit.
func (r Record) bound() Record {
	if len(r.Cause) > maxCauseDepth {
		r.Cause = r.Cause[:maxCauseDepth]
	}

	return r
}

// ProvenanceStore is where records go.
//
// The three queries are the three questions the design set out to answer, and
// each names its own cost: a store is free to be a linear scan, and the
// default one is.
type ProvenanceStore interface {
	// Append writes records. It runs on the recorder's goroutine, never on
	// the tick.
	Append(ctx context.Context, records []Record) error
	// AtPosition is every record about one block, newest first.
	AtPosition(ctx context.Context, pos world.BlockPos, window time.Duration) ([]Record, error)
	// ByActor is every record one actor caused, newest first.
	ByActor(ctx context.Context, uuid string, window time.Duration) ([]Record, error)
	// ForItem is every record about one item, oldest first, which is the
	// order a chain reads in.
	ForItem(ctx context.Context, id ItemID) ([]Record, error)
	Close() error
}

// OverflowPolicy is what a full queue does.
type OverflowPolicy uint8

// The two overflow policies.
const (
	// OverflowDrop counts the record and carries on. It is the default: a
	// stalled world is worse than a gap in an audit trail.
	OverflowDrop OverflowPolicy = iota
	// OverflowBlock waits for room, which trades the tick for completeness.
	OverflowBlock
)

// recorderQueue is how many records the queue holds.
//
// 8,192 absorbs a full inventory transfer plus an explosion. At the record
// sizes this server writes — 200 to 300 bytes marshalled — that is about
// 2 MB of pending work.
const recorderQueue = 8192

// warnInterval bounds how often an overflow warns. The condition that fills
// the queue also produces a record per drop, so an unlimited warning would
// turn one problem into two.
const warnInterval = 30 * time.Second

// Recorder takes records off the tick.
type Recorder struct {
	store    ProvenanceStore
	log      *slog.Logger
	policy   OverflowPolicy
	queue    chan Record
	dropped  atomic.Uint64
	lastWarn atomic.Int64

	closeOnce sync.Once
	done      chan struct{}
}

// NewRecorder starts a recorder writing into store.
//
// A nil store returns a nil recorder, which every Record call tolerates: that
// is what "off by default" costs at the call site.
func NewRecorder(store ProvenanceStore, log *slog.Logger, policy OverflowPolicy) *Recorder {
	if store == nil {
		return nil
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	r := &Recorder{
		store:  store,
		log:    log,
		policy: policy,
		queue:  make(chan Record, recorderQueue),
		done:   make(chan struct{}),
	}
	go r.run()

	return r
}

// Record hands one record to the writer. It never blocks under the default
// policy, and a nil recorder is a no-op.
func (r *Recorder) Record(rec Record) {
	if r == nil {
		return
	}
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	rec = rec.bound()

	if r.policy == OverflowBlock {
		select {
		case r.queue <- rec:
		case <-r.done:
		}

		return
	}

	select {
	case r.queue <- rec:
	default:
		r.drop()
	}
}

// RecordDuplicate is the shape a detected duplication takes.
func (r *Recorder) RecordDuplicate(d *ErrDuplicate) {
	if r == nil || d == nil {
		return
	}
	r.Record(Record{
		Kind:   RecordItem,
		Reason: ReasonDuplicate,
		Actor:  d.By,
		From:   d.Expected,
		To:     d.Actual,
		Items:  []ItemID{d.ID},
		Note:   d.Error(),
	})
}

// Dropped is how many records the queue lost, which M11.6 samples so a silent
// audit gap is visible in two places rather than one.
func (r *Recorder) Dropped() uint64 {
	if r == nil {
		return 0
	}

	return r.dropped.Load()
}

func (r *Recorder) drop() {
	total := r.dropped.Add(1)

	now := time.Now().UnixNano()
	last := r.lastWarn.Load()
	if now-last < int64(warnInterval) {
		return
	}
	if !r.lastWarn.CompareAndSwap(last, now) {
		return
	}
	r.log.Warn("provenance records are being dropped; the audit trail has a gap",
		"dropped", total, "queue", cap(r.queue))
}

// run drains the queue in batches, so a burst becomes one append rather than
// one per record.
func (r *Recorder) run() {
	const maxBatch = 256

	batch := make([]Record, 0, maxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := r.store.Append(context.Background(), batch); err != nil {
			r.log.Error("write provenance records", "error", err, "records", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case rec, ok := <-r.queue:
			if !ok {
				flush()
				close(r.done)

				return
			}
			batch = append(batch, rec)
			if len(batch) >= maxBatch {
				flush()
			}
		default:
			flush()
			rec, ok := <-r.queue
			if !ok {
				close(r.done)

				return
			}
			batch = append(batch, rec)
		}
	}
}

// Close drains what is queued and stops the writer.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}

	r.closeOnce.Do(func() {
		close(r.queue)
		<-r.done
	})

	return r.store.Close()
}

// WithProvenance records block and item history to store.
//
// Omit it and nothing is recorded and nothing is allocated; a nil store is an
// error rather than a silent no-op, for the same reason the other stores
// refuse one.
func WithProvenance(store ProvenanceStore) Option {
	return func(b *builder) error {
		if store == nil {
			return fmt.Errorf("%w: nil provenance store, omit WithProvenance to run without one", ErrInvalidOption)
		}
		b.provenance = store

		return nil
	}
}

// ProvenanceOverflowBlocks makes a full record queue wait rather than drop.
//
// It trades the tick for completeness, which is the right trade only where an
// audit trail with a gap in it is worse than a world that stutters.
func ProvenanceOverflowBlocks() Option {
	return func(b *builder) error {
		b.provenanceOverflow = OverflowBlock

		return nil
	}
}

// WithItemIdentity turns on item identity and the duplication detector.
//
// It needs provenance to be useful — a detection with nowhere to go is only a
// log line — but it does not require it, so a server can run the detector and
// keep the records in the log alone.
func WithItemIdentity(policy DuplicatePolicy) Option {
	return func(b *builder) error {
		b.itemIdentity = true
		b.duplicatePolicy = policy

		return nil
	}
}

// marshalRecord is what the file store writes and what a test compares.
func marshalRecord(r Record) ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal provenance record: %w", err)
	}

	return raw, nil
}
