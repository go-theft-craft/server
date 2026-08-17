package world

// BlocksPerSection is how many blocks one 16×16×16 section holds.
const BlocksPerSection = 16 * 16 * 16

// SectionBlockIndex is the index a block's local coordinates take inside a
// section. Y is the local Y within the section, not the world Y.
func SectionBlockIndex(x, localY, z int) int { return localY*256 + z*16 + x }

// Section holds the block states of one 16×16×16 slice of a chunk. A Section
// is immutable once a Builder or a With call has returned it: a write produces
// a new Section, which is what makes a snapshot a pointer copy and lets an
// encoder memoize bytes on the pointer.
type Section struct {
	states [BlocksPerSection]State
}

// Change is one block write inside a section.
type Change struct {
	Index int
	State State
}

// At returns the state at a section-local index.
func (s *Section) At(index int) State {
	if s == nil {
		return 0
	}

	return s.states[index]
}

// With returns a Section that differs from the receiver at one index. The
// receiver is untouched.
func (s *Section) With(index int, state State) *Section {
	next := new(Section)
	if s != nil {
		next.states = s.states
	}
	next.states[index] = state

	return next
}

// WithMany returns a Section with every change applied, copying once however
// many blocks change. A MultiBlockChange is only affordable because of this.
func (s *Section) WithMany(changes []Change) *Section {
	next := new(Section)
	if s != nil {
		next.states = s.states
	}
	for _, c := range changes {
		next.states[c.Index] = c.State
	}

	return next
}

// IsAir reports whether every block in the section is the given air state,
// which is what decides a section's bit in a chunk bitmap.
func (s *Section) IsAir(air State) bool {
	if s == nil {
		return true
	}
	for _, st := range s.states {
		if st != air {
			return false
		}
	}

	return true
}

// States exposes the section's blocks for encoding. The caller must not write
// to it: a Section is shared by every snapshot that has ever seen it.
func (s *Section) States() []State {
	if s == nil {
		return nil
	}

	return s.states[:]
}
