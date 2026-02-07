package pc_1_8_test

import (
	"testing"

	"github.com/OCharnyshevich/minecraft-server/internal/gamedata"
	pc18 "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
)

func newGameData(t *testing.T) *gamedata.GameData {
	t.Helper()
	gd := pc18.New()
	if gd == nil {
		t.Fatal("New() returned nil")
	}
	return gd
}

func TestInitRegistration(t *testing.T) {
	gd, err := gamedata.Load("pc-1.8")
	if err != nil {
		t.Fatalf("pc-1.8 should be registered via init(), got error: %v", err)
	}
	if gd == nil {
		t.Fatal("expected non-nil GameData from Load")
	}
}

func TestBlocks_ByID(t *testing.T) {
	gd := newGameData(t)

	stone, ok := gd.Blocks.ByID(1)
	if !ok {
		t.Fatal("expected to find block with ID 1 (stone)")
	}
	if stone.Name != "stone" {
		t.Errorf("expected name 'stone', got %q", stone.Name)
	}
	if stone.DisplayName != "Stone" {
		t.Errorf("expected display name 'Stone', got %q", stone.DisplayName)
	}
	if stone.Hardness == nil || *stone.Hardness != 1.5 {
		t.Errorf("expected hardness 1.5, got %v", stone.Hardness)
	}
	if stone.StackSize != 64 {
		t.Errorf("expected stack size 64, got %d", stone.StackSize)
	}
}

func TestBlocks_ByName(t *testing.T) {
	gd := newGameData(t)

	air, ok := gd.Blocks.ByName("air")
	if !ok {
		t.Fatal("expected to find block 'air'")
	}
	if air.ID != 0 {
		t.Errorf("expected air ID 0, got %d", air.ID)
	}
	if air.Transparent != true {
		t.Error("expected air to be transparent")
	}
}

func TestBlocks_All(t *testing.T) {
	gd := newGameData(t)

	all := gd.Blocks.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty block list")
	}
	if len(all) < 100 {
		t.Errorf("expected at least 100 blocks, got %d", len(all))
	}
}

func TestBlocks_NotFound(t *testing.T) {
	gd := newGameData(t)

	_, ok := gd.Blocks.ByID(99999)
	if ok {
		t.Error("expected not found for non-existent block ID")
	}

	_, ok = gd.Blocks.ByName("nonexistent_block")
	if ok {
		t.Error("expected not found for non-existent block name")
	}
}

func TestItems_ByID(t *testing.T) {
	gd := newGameData(t)

	stone, ok := gd.Items.ByID(1)
	if !ok {
		t.Fatal("expected to find item with ID 1")
	}
	if stone.Name != "stone" {
		t.Errorf("expected name 'stone', got %q", stone.Name)
	}
	if stone.StackSize != 64 {
		t.Errorf("expected stack size 64, got %d", stone.StackSize)
	}
}

func TestItems_All(t *testing.T) {
	gd := newGameData(t)

	all := gd.Items.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty item list")
	}
}

func TestEntities_ByName(t *testing.T) {
	gd := newGameData(t)

	creeper, ok := gd.Entities.ByName("Creeper")
	if !ok {
		t.Fatal("expected to find entity 'Creeper'")
	}
	if creeper.ID != 50 {
		t.Errorf("expected Creeper ID 50, got %d", creeper.ID)
	}
	if creeper.Type != "mob" {
		t.Errorf("expected type 'mob', got %q", creeper.Type)
	}
}

func TestEntities_All(t *testing.T) {
	gd := newGameData(t)

	all := gd.Entities.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty entity list")
	}
}

func TestBiomes_ByName(t *testing.T) {
	gd := newGameData(t)

	plains, ok := gd.Biomes.ByName("plains")
	if !ok {
		t.Fatal("expected to find biome 'plains'")
	}
	if plains.Category != "plains" {
		t.Errorf("expected category 'plains', got %q", plains.Category)
	}
}

func TestEffects_ByID(t *testing.T) {
	gd := newGameData(t)

	speed, ok := gd.Effects.ByID(1)
	if !ok {
		t.Fatal("expected to find effect with ID 1 (Speed)")
	}
	if speed.Name != "Speed" {
		t.Errorf("expected name 'Speed', got %q", speed.Name)
	}
	if speed.Type != "good" {
		t.Errorf("expected type 'good', got %q", speed.Type)
	}
}

func TestEnchantments_ByName(t *testing.T) {
	gd := newGameData(t)

	prot, ok := gd.Enchantments.ByName("protection")
	if !ok {
		t.Fatal("expected to find enchantment 'protection'")
	}
	if prot.MaxLevel != 4 {
		t.Errorf("expected max level 4, got %d", prot.MaxLevel)
	}
	if prot.Category != "armor" {
		t.Errorf("expected category 'armor', got %q", prot.Category)
	}
}

func TestFoods_ByName(t *testing.T) {
	gd := newGameData(t)

	apple, ok := gd.Foods.ByName("apple")
	if !ok {
		t.Fatal("expected to find food 'apple'")
	}
	if apple.FoodPoints == 0 {
		t.Error("expected non-zero food points for apple")
	}
}

func TestParticles_ByID(t *testing.T) {
	gd := newGameData(t)

	all := gd.Particles.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty particle list")
	}

	first, ok := gd.Particles.ByID(all[0].ID)
	if !ok {
		t.Fatal("expected to find particle by ID")
	}
	if first.Name == "" {
		t.Error("expected non-empty particle name")
	}
}

func TestInstruments_ByName(t *testing.T) {
	gd := newGameData(t)

	harp, ok := gd.Instruments.ByName("harp")
	if !ok {
		t.Fatal("expected to find instrument 'harp'")
	}
	if harp.ID != 0 {
		t.Errorf("expected harp ID 0, got %d", harp.ID)
	}

	all := gd.Instruments.All()
	if len(all) != 5 {
		t.Errorf("expected 5 instruments, got %d", len(all))
	}
}

func TestAttributes_ByName(t *testing.T) {
	gd := newGameData(t)

	hp, ok := gd.Attributes.ByName("maxHealth")
	if !ok {
		t.Fatal("expected to find attribute 'maxHealth'")
	}
	if hp.Default != 20 {
		t.Errorf("expected default 20, got %f", hp.Default)
	}
	if hp.Resource != "generic.maxHealth" {
		t.Errorf("expected resource 'generic.maxHealth', got %q", hp.Resource)
	}
}

func TestAttributes_ByResource(t *testing.T) {
	gd := newGameData(t)

	speed, ok := gd.Attributes.ByResource("generic.movementSpeed")
	if !ok {
		t.Fatal("expected to find attribute by resource 'generic.movementSpeed'")
	}
	if speed.Name != "movementSpeed" {
		t.Errorf("expected name 'movementSpeed', got %q", speed.Name)
	}
}

func TestWindows_ByName(t *testing.T) {
	gd := newGameData(t)

	furnace, ok := gd.Windows.ByName("Furnace")
	if !ok {
		t.Fatal("expected to find window 'Furnace'")
	}
	if len(furnace.Slots) == 0 {
		t.Error("expected furnace to have slots")
	}
	if len(furnace.Properties) == 0 {
		t.Error("expected furnace to have properties")
	}

	all := gd.Windows.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty window list")
	}
}

func TestMaterials_ByName(t *testing.T) {
	gd := newGameData(t)

	rock, ok := gd.Materials.ByName("rock")
	if !ok {
		t.Fatal("expected to find material 'rock'")
	}
	if len(rock.ToolSpeeds) == 0 {
		t.Error("expected rock to have tool speeds")
	}

	all := gd.Materials.All()
	if len(all) != 8 {
		t.Errorf("expected 8 materials, got %d", len(all))
	}
}

func TestRecipes_ByID(t *testing.T) {
	gd := newGameData(t)

	recipes := gd.Recipes.ByID(1)
	if len(recipes) == 0 {
		t.Fatal("expected recipes for ID 1 (stone variants)")
	}

	allRecipes := gd.Recipes.All()
	if len(allRecipes) == 0 {
		t.Fatal("expected non-empty recipe map")
	}
}

func TestLanguage_Get(t *testing.T) {
	gd := newGameData(t)

	name, ok := gd.Language.Get("language.name")
	if !ok {
		t.Fatal("expected to find 'language.name' key")
	}
	if name == "" {
		t.Error("expected non-empty language name")
	}

	all := gd.Language.All()
	if len(all) == 0 {
		t.Fatal("expected non-empty language map")
	}
}

func TestCollisionShapes(t *testing.T) {
	gd := newGameData(t)

	cs := gd.CollisionShapes
	if cs == nil {
		t.Fatal("expected non-nil CollisionShapes")
	}
	if len(cs.Blocks) == 0 {
		t.Fatal("expected non-empty collision blocks map")
	}
	if len(cs.Shapes) == 0 {
		t.Fatal("expected non-empty collision shapes map")
	}

	stoneShapes, ok := cs.Blocks["stone"]
	if !ok {
		t.Fatal("expected collision data for 'stone'")
	}
	if len(stoneShapes) == 0 {
		t.Fatal("expected stone to have shape IDs")
	}
}

func TestProtocol(t *testing.T) {
	gd := newGameData(t)

	proto := gd.Protocol
	if proto == nil {
		t.Fatal("expected non-nil Protocol")
	}
	if len(proto.Types) == 0 {
		t.Fatal("expected non-empty protocol types")
	}
	if len(proto.Phases) == 0 {
		t.Fatal("expected non-empty protocol phases")
	}

	playPhase, ok := proto.Phases["play"]
	if !ok {
		t.Fatal("expected 'play' phase in protocol")
	}
	if len(playPhase.ToClient.Packets) == 0 {
		t.Fatal("expected play phase to have toClient packets")
	}
	if len(playPhase.ToServer.Packets) == 0 {
		t.Fatal("expected play phase to have toServer packets")
	}
}

func TestVersion(t *testing.T) {
	gd := newGameData(t)

	v := gd.Version
	if v == nil {
		t.Fatal("expected non-nil Version")
	}
	if v.Protocol != 47 {
		t.Errorf("expected protocol version 47, got %d", v.Protocol)
	}
	if v.MinecraftVersion != "1.8.8" {
		t.Errorf("expected minecraft version '1.8.8', got %q", v.MinecraftVersion)
	}
	if v.MajorVersion != "1.8" {
		t.Errorf("expected major version '1.8', got %q", v.MajorVersion)
	}
}
