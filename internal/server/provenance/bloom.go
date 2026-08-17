package provenance

import "encoding/json"

// A bloom filter over the item IDs one rotation holds.
//
// It exists so that "everything that ever happened to this item" does not open
// every file. A filter says "definitely not here" or "maybe here"; the maybes
// are scanned and the definitely-nots are skipped, so the answer is exact and
// only the work is reduced.
//
// Sized for about 1% false positives at 120,000 records per file, which is
// what 32 MB of records comes to: 1.2 MB of filter per million records, as the
// design estimated.

const (
	// bloomBits is the filter's size in bits. 2^20 bits is 128 KB per file.
	bloomBits = 1 << 20
	bloomMask = bloomBits - 1
	// bloomHashes is how many positions each ID sets. Four is the optimum for
	// this bits-per-item ratio.
	bloomHashes = 4
)

// bloom is a fixed-size filter. It marshals as raw bytes so a manifest stays
// one file rather than a file plus a sidecar.
type bloom struct {
	bits []byte
}

func (b *bloom) ensure() {
	if b.bits == nil {
		b.bits = make([]byte, bloomBits/8)
	}
}

// positions is the set of bits an ID sets. The two halves of a 64-bit hash are
// combined the way Kirsch and Mitzenmacher describe, so one hash produces all
// four positions.
func positions(id uint64) [bloomHashes]uint32 {
	h1 := mix(id)
	h2 := mix(id ^ 0x9E3779B97F4A7C15)

	var out [bloomHashes]uint32
	for i := range out {
		out[i] = uint32((h1 + uint64(i)*h2) & bloomMask)
	}

	return out
}

// mix is splitmix64's finalizer, which spreads a counter-like ID across the
// whole word — and item IDs are counter-like by construction.
func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31

	return x
}

func (b *bloom) add(id uint64) {
	b.ensure()
	for _, p := range positions(id) {
		b.bits[p/8] |= 1 << (p % 8)
	}
}

// mayContain reports false only when the ID is definitely absent.
func (b *bloom) mayContain(id uint64) bool {
	if b.bits == nil {
		return false
	}
	for _, p := range positions(id) {
		if b.bits[p/8]&(1<<(p%8)) == 0 {
			return false
		}
	}

	return true
}

// MarshalJSON writes the filter as base64 bytes, which encoding/json does for
// a []byte.
func (b bloom) MarshalJSON() ([]byte, error) { return json.Marshal(b.bits) }

// UnmarshalJSON reads it back.
func (b *bloom) UnmarshalJSON(raw []byte) error { return json.Unmarshal(raw, &b.bits) }
