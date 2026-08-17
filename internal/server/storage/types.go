package storage

// The shapes of the JSON world files the pre-M11.3 server wrote. Nothing
// writes them any more; Migrate reads them once and renames them.

// WorldData holds world-level metadata for persistence.
type WorldData struct {
	Age       int64 `json:"age"`
	TimeOfDay int64 `json:"time_of_day"`
}

// BlockOverrideEntry is a single block override for JSON serialization.
type BlockOverrideEntry struct {
	X       int   `json:"x"`
	Y       int   `json:"y"`
	Z       int   `json:"z"`
	StateID int32 `json:"state_id"`
}

// ChestEntry is one stored chest for JSON serialization. Slots is written in
// window order and is always ChestSlots long, so a hand-edited file that is
// short or long is rejected rather than silently shifting items.
type ChestEntry struct {
	X     int              `json:"x"`
	Y     int              `json:"y"`
	Z     int              `json:"z"`
	Slots []ChestSlotEntry `json:"slots"`
}

// ChestSlotEntry is one item stack inside a stored chest.
type ChestSlotEntry struct {
	ID     int16 `json:"id"`
	Count  int8  `json:"count"`
	Damage int16 `json:"damage"`
}
