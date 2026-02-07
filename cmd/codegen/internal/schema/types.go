package schema

import (
	"encoding/json"
	"fmt"
)

type Block struct {
	ID           int             `json:"id"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName"`
	Hardness     *float64        `json:"hardness"`
	StackSize    int             `json:"stackSize"`
	Diggable     bool            `json:"diggable"`
	BoundingBox  string          `json:"boundingBox"`
	Material     string          `json:"material"`
	Transparent  bool            `json:"transparent"`
	EmitLight    int             `json:"emitLight"`
	FilterLight  int             `json:"filterLight"`
	Resistance   float64         `json:"resistance"`
	Drops        []RawDrop       `json:"drops"`
	HarvestTools map[string]bool `json:"harvestTools"`
	Variations   []RawVariation  `json:"variations"`
}

type RawDrop struct {
	Drop     json.RawMessage `json:"drop"`
	MinCount float64         `json:"minCount"`
	MaxCount float64         `json:"maxCount"`
}

type DropObject struct {
	ID       int `json:"id"`
	Metadata int `json:"metadata"`
}

func (d *RawDrop) Parse() (id, metadata, minCount, maxCount int) {
	minCount = int(d.MinCount)
	maxCount = int(d.MaxCount)

	var plainID int
	if err := json.Unmarshal(d.Drop, &plainID); err == nil {
		return plainID, 0, minCount, maxCount
	}

	var obj DropObject
	if err := json.Unmarshal(d.Drop, &obj); err == nil {
		return obj.ID, obj.Metadata, minCount, maxCount
	}

	return 0, 0, minCount, maxCount
}

type RawVariation struct {
	Metadata    int    `json:"metadata"`
	DisplayName string `json:"displayName"`
}

type Item struct {
	ID                int            `json:"id"`
	Name              string         `json:"name"`
	DisplayName       string         `json:"displayName"`
	StackSize         int            `json:"stackSize"`
	MaxDurability     int            `json:"maxDurability"`
	EnchantCategories []string       `json:"enchantCategories"`
	RepairWith        []string       `json:"repairWith"`
	Variations        []RawVariation `json:"variations"`
}

type Entity struct {
	ID          int      `json:"id"`
	InternalID  int      `json:"internalId"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Type        string   `json:"type"`
	Width       *float64 `json:"width"`
	Height      *float64 `json:"height"`
	Category    string   `json:"category"`
}

type Biome struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	NameLegacy    string  `json:"name_legacy"`
	DisplayName   string  `json:"displayName"`
	Category      string  `json:"category"`
	Temperature   float64 `json:"temperature"`
	Precipitation string  `json:"precipitation"`
	Depth         float64 `json:"depth"`
	Dimension     string  `json:"dimension"`
	Color         int     `json:"color"`
	Rainfall      float64 `json:"rainfall"`
}

type Effect struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
}

type Enchantment struct {
	ID           int         `json:"id"`
	Name         string      `json:"name"`
	DisplayName  string      `json:"displayName"`
	MaxLevel     int         `json:"maxLevel"`
	MinCost      EnchantCost `json:"minCost"`
	MaxCost      EnchantCost `json:"maxCost"`
	Exclude      []string    `json:"exclude"`
	Category     string      `json:"category"`
	Weight       int         `json:"weight"`
	TreasureOnly bool        `json:"treasureOnly"`
	Curse        bool        `json:"curse"`
	Tradeable    bool        `json:"tradeable"`
	Discoverable bool        `json:"discoverable"`
}

type EnchantCost struct {
	A int `json:"a"`
	B int `json:"b"`
}

type Food struct {
	ID               int            `json:"id"`
	Name             string         `json:"name"`
	DisplayName      string         `json:"displayName"`
	StackSize        int            `json:"stackSize"`
	FoodPoints       float64        `json:"foodPoints"`
	Saturation       float64        `json:"saturation"`
	EffectiveQuality float64        `json:"effectiveQuality"`
	SaturationRatio  float64        `json:"saturationRatio"`
	Variations       []RawVariation `json:"variations"`
}

type Particle struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Attribute struct {
	Name     string  `json:"name"`
	Resource string  `json:"resource"`
	Default  float64 `json:"default"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
}

type Window struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Slots      []WindowSlot   `json:"slots"`
	Properties []string       `json:"properties"`
	OpenedWith []WindowOpener `json:"openedWith"`
}

type WindowSlot struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
	Size  int    `json:"size"`
}

type WindowOpener struct {
	Type string `json:"type"`
	ID   int    `json:"id"`
}

type VersionInfo struct {
	Version          int    `json:"version"`
	MinecraftVersion string `json:"minecraftVersion"`
	MajorVersion     string `json:"majorVersion"`
}

type RawRecipe struct {
	Ingredients []json.RawMessage   `json:"ingredients"`
	InShape     [][]json.RawMessage `json:"inShape"`
	Result      RecipeResult        `json:"result"`
}

type RecipeResult struct {
	ID       int `json:"id"`
	Count    int `json:"count"`
	Metadata int `json:"metadata"`
}

type RecipeIngredient struct {
	ID       int `json:"id"`
	Metadata int `json:"metadata"`
}

func ParseIngredient(raw json.RawMessage) RecipeIngredient {
	var plainID int
	if err := json.Unmarshal(raw, &plainID); err == nil {
		return RecipeIngredient{ID: plainID}
	}

	var obj RecipeIngredient
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}

	return RecipeIngredient{}
}

type RawCollisionShapes struct {
	Blocks map[string]json.RawMessage `json:"blocks"`
	Shapes map[string][][]float64     `json:"shapes"`
}

func LoadJSON[T any](data []byte) ([]T, error) {
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return items, nil
}
