package gamedata

type Enchantment struct {
	ID           int
	Name         string
	DisplayName  string
	MaxLevel     int
	MinCost      EnchantCost
	MaxCost      EnchantCost
	Exclude      []string
	Category     string
	Weight       int
	TreasureOnly bool
	Curse        bool
	Tradeable    bool
	Discoverable bool
}

type EnchantCost struct {
	A int
	B int
}
