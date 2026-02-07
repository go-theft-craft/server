package gamedata

type Block struct {
	ID           int
	Name         string
	DisplayName  string
	Hardness     *float64
	StackSize    int
	Diggable     bool
	BoundingBox  string
	Material     string
	Transparent  bool
	EmitLight    int
	FilterLight  int
	Resistance   float64
	Drops        []Drop
	HarvestTools map[int]bool
	Variations   []Variation
}

type Drop struct {
	ID       int
	Metadata int
	MinCount int
	MaxCount int
}

type Variation struct {
	Metadata    int
	DisplayName string
}
