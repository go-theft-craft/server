package gen

import (
	"encoding/json"
	"fmt"

	"github.com/go-theft-craft/server/pkg/world"
)

// FlatName is the registered name of the superflat generator.
const FlatName = "flat"

// FlatVersion is this generator's version.
const FlatVersion = 1

// FlatParams configures the superflat generator: a list of layers from the
// bottom up, and one biome everywhere.
type FlatParams struct {
	Layers []FlatLayer `json:"layers"`
	Biome  string      `json:"biome"`
}

// Type implements Params.
func (FlatParams) Type() string { return FlatName }

// FlatLayer is one band of a superflat world.
type FlatLayer struct {
	Block     string `json:"block"`
	Thickness int    `json:"thickness"`
}

// Height is the y of the top block, which is the sum of the thicknesses minus
// one because the first layer sits at y=0.
func (p FlatParams) Height() int {
	total := 0
	for _, layer := range p.Layers {
		total += layer.Thickness
	}
	if total == 0 {
		return 0
	}

	return total - 1
}

// FlatDefaults is the classic superflat: bedrock, two stone, dirt, grass.
func FlatDefaults() FlatParams {
	return FlatParams{
		Layers: []FlatLayer{
			{Block: "minecraft:bedrock", Thickness: 1},
			{Block: "minecraft:stone", Thickness: 2},
			{Block: "minecraft:dirt", Thickness: 1},
			{Block: "minecraft:grass", Thickness: 1},
		},
		Biome: "minecraft:plains",
	}
}

// flatFactory registers the superflat generator.
type flatFactory struct{}

func (flatFactory) Name() string     { return FlatName }
func (flatFactory) Version() int     { return FlatVersion }
func (flatFactory) Defaults() Params { return FlatDefaults() }

func (flatFactory) Parse(raw json.RawMessage) (Params, error) {
	p := FlatDefaults()
	if err := parseParams(raw, &p); err != nil {
		return nil, err
	}
	// A layer list is replaced wholesale rather than merged, so parameters
	// that name no layers mean an empty world rather than the defaults
	// silently reappearing under them.
	for i, layer := range p.Layers {
		if layer.Thickness < 0 {
			return nil, fmt.Errorf("gen: flat layer %d has thickness %d", i, layer.Thickness)
		}
		if layer.Block == "" {
			return nil, fmt.Errorf("gen: flat layer %d names no block", i)
		}
	}

	return p, nil
}

func (flatFactory) New(seed int64, p Params, reg world.StateRegistry) (Generator, error) {
	params, ok := p.(FlatParams)
	if !ok {
		if p == nil {
			params = FlatDefaults()
		} else {
			return nil, invalidParams(FlatName, p)
		}
	}

	return NewFlatGeneratorWith(seed, params, reg)
}
