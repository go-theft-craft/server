package generator

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/go-theft-craft/server/cmd/codegen/internal/schema"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

type Config struct {
	SchemeDir string
	OutDir    string
	Package   string
	Version   string
}

type templateData struct {
	Package string
	Version string
	Data    any
}

func Run(cfg Config) error {
	outPath := filepath.Join(cfg.OutDir, cfg.Package)

	if err := os.RemoveAll(outPath); err != nil {
		return fmt.Errorf("failed to remove output directory: %w", err)
	}

	if err := os.MkdirAll(outPath, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	// Generate helpers.go
	if err := renderToFile(tmpl, "helpers.go.tmpl", filepath.Join(outPath, "helpers.go"), templateData{
		Package: cfg.Package,
	}); err != nil {
		return err
	}

	// Generate each registry from JSON arrays
	generators := []struct {
		jsonFile     string
		templateName string
		outputFile   string
		loadFn       func([]byte) (any, error)
	}{
		{"blocks.json", "blocks.go.tmpl", "blocks.go", loadBlocks},
		{"items.json", "items.go.tmpl", "items.go", loadItems},
		{"entities.json", "entities.go.tmpl", "entities.go", loadEntities},
		{"biomes.json", "biomes.go.tmpl", "biomes.go", loadBiomes},
		{"effects.json", "effects.go.tmpl", "effects.go", loadEffects},
		{"enchantments.json", "enchantments.go.tmpl", "enchantments.go", loadEnchantments},
		{"foods.json", "foods.go.tmpl", "foods.go", loadFoods},
		{"particles.json", "particles.go.tmpl", "particles.go", loadParticles},
		{"instruments.json", "instruments.go.tmpl", "instruments.go", loadInstruments},
		{"attributes.json", "attributes.go.tmpl", "attributes.go", loadAttributes},
		{"windows.json", "windows.go.tmpl", "windows.go", loadWindows},
	}

	for _, g := range generators {
		jsonPath := filepath.Join(cfg.SchemeDir, g.jsonFile)

		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", g.jsonFile, err)
		}

		data, err := g.loadFn(raw)
		if err != nil {
			return fmt.Errorf("parse %s: %w", g.jsonFile, err)
		}

		td := templateData{
			Package: cfg.Package,
			Version: cfg.Version,
			Data:    data,
		}

		outFile := filepath.Join(outPath, g.outputFile)
		if err := renderToFile(tmpl, g.templateName, outFile, td); err != nil {
			return fmt.Errorf("generate %s: %w", g.outputFile, err)
		}

		fmt.Printf("  generated %s\n", g.outputFile)
	}

	// Generate files with special loading (non-array JSON)
	specialGenerators := []struct {
		jsonFile     string
		templateName string
		outputFile   string
		loadFn       func([]byte) (templateData, error)
	}{
		{"version.json", "version.go.tmpl", "version.go", func(raw []byte) (templateData, error) {
			data, err := loadVersion(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"language.json", "language.go.tmpl", "language.go", func(raw []byte) (templateData, error) {
			data, err := loadLanguage(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"materials.json", "materials.go.tmpl", "materials.go", func(raw []byte) (templateData, error) {
			data, err := loadMaterials(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"recipes.json", "recipes.go.tmpl", "recipes.go", func(raw []byte) (templateData, error) {
			data, err := loadRecipes(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"blockCollisionShapes.json", "collision_shapes.go.tmpl", "collision_shapes.go", func(raw []byte) (templateData, error) {
			data, err := loadCollisionShapes(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"protocol.json", "protocol.go.tmpl", "protocol.go", func(raw []byte) (templateData, error) {
			data, err := loadProtocol(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
		{"protocol.json", "packets.go.tmpl", "packets.go", func(raw []byte) (templateData, error) {
			data, err := loadPacketStructs(raw)
			if err != nil {
				return templateData{}, err
			}
			return templateData{Package: cfg.Package, Version: cfg.Version, Data: data}, nil
		}},
	}

	for _, g := range specialGenerators {
		jsonPath := filepath.Join(cfg.SchemeDir, g.jsonFile)

		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", g.jsonFile, err)
		}

		td, err := g.loadFn(raw)
		if err != nil {
			return fmt.Errorf("parse %s: %w", g.jsonFile, err)
		}

		outFile := filepath.Join(outPath, g.outputFile)
		if err := renderToFile(tmpl, g.templateName, outFile, td); err != nil {
			return fmt.Errorf("generate %s: %w", g.outputFile, err)
		}

		fmt.Printf("  generated %s\n", g.outputFile)
	}

	// Generate gamedata.go (factory + init)
	if err := renderToFile(tmpl, "gamedata.go.tmpl", filepath.Join(outPath, "gamedata.go"), templateData{
		Package: cfg.Package,
		Version: cfg.Version,
	}); err != nil {
		return fmt.Errorf("generate gamedata.go: %w", err)
	}

	fmt.Printf("  generated gamedata.go\n")

	return nil
}

func renderToFile(tmpl *template.Template, name, outFile string, data any) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute template %s: %w", name, err)
	}
	if err := os.WriteFile(outFile, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outFile, err)
	}
	return nil
}

// Intermediate types for template rendering that have pre-processed fields.

type blockTmpl struct {
	schema.Block
	Drops        []dropTmpl
	HarvestTools map[int]bool
}

type dropTmpl struct {
	ID       int
	Metadata int
	MinCount int
	MaxCount int
}

func loadBlocks(raw []byte) (any, error) {
	blocks, err := schema.LoadJSON[schema.Block](raw)
	if err != nil {
		return nil, err
	}

	result := make([]blockTmpl, len(blocks))
	for i, b := range blocks {
		drops := make([]dropTmpl, len(b.Drops))
		for j, d := range b.Drops {
			id, meta, minC, maxC := d.Parse()
			drops[j] = dropTmpl{ID: id, Metadata: meta, MinCount: minC, MaxCount: maxC}
		}

		harvestTools := make(map[int]bool, len(b.HarvestTools))
		for k, v := range b.HarvestTools {
			id, _ := strconv.Atoi(k)
			harvestTools[id] = v
		}

		result[i] = blockTmpl{
			Block:        b,
			Drops:        drops,
			HarvestTools: harvestTools,
		}
	}

	return result, nil
}

func loadItems(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Item](raw)
}

func loadEntities(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Entity](raw)
}

func loadBiomes(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Biome](raw)
}

func loadEffects(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Effect](raw)
}

func loadEnchantments(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Enchantment](raw)
}

func loadFoods(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Food](raw)
}

func loadParticles(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Particle](raw)
}

func loadInstruments(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Instrument](raw)
}

func loadAttributes(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Attribute](raw)
}

func loadWindows(raw []byte) (any, error) {
	return schema.LoadJSON[schema.Window](raw)
}

// Version

type versionTmpl struct {
	Protocol         int
	MinecraftVersion string
	MajorVersion     string
	MetadataEnd      byte // entity metadata terminator: 0x7F for <1.9, 0xFF for >=1.9
}

func loadVersion(raw []byte) (*versionTmpl, error) {
	var v schema.VersionInfo
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("unmarshal version: %w", err)
	}
	// Entity metadata terminator changed in 1.9 (protocol 110).
	var metadataEnd byte = 0x7F
	if v.Version >= 110 {
		metadataEnd = 0xFF
	}

	return &versionTmpl{
		Protocol:         v.Version,
		MinecraftVersion: v.MinecraftVersion,
		MajorVersion:     v.MajorVersion,
		MetadataEnd:      metadataEnd,
	}, nil
}

// Language

func loadLanguage(raw []byte) (map[string]string, error) {
	var lang map[string]string
	if err := json.Unmarshal(raw, &lang); err != nil {
		return nil, fmt.Errorf("unmarshal language: %w", err)
	}
	return lang, nil
}

// Materials

type materialTmpl struct {
	Name       string
	ToolSpeeds map[int]float64
}

func loadMaterials(raw []byte) ([]materialTmpl, error) {
	var rawMats map[string]map[string]float64
	if err := json.Unmarshal(raw, &rawMats); err != nil {
		return nil, fmt.Errorf("unmarshal materials: %w", err)
	}

	result := make([]materialTmpl, 0, len(rawMats))
	for name, tools := range rawMats {
		toolSpeeds := make(map[int]float64, len(tools))
		for k, v := range tools {
			id, _ := strconv.Atoi(k)
			toolSpeeds[id] = v
		}
		result = append(result, materialTmpl{Name: name, ToolSpeeds: toolSpeeds})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// Recipes

type recipeTmplEntry struct {
	ID      int
	Recipes []recipeTmpl
}

type recipeTmpl struct {
	Ingredients []ingredientTmpl
	InShape     [][]ingredientTmpl
	Result      schema.RecipeResult
}

type ingredientTmpl struct {
	ID       int
	Metadata int
}

func loadRecipes(raw []byte) ([]recipeTmplEntry, error) {
	var rawRecipes map[string][]schema.RawRecipe
	if err := json.Unmarshal(raw, &rawRecipes); err != nil {
		return nil, fmt.Errorf("unmarshal recipes: %w", err)
	}

	result := make([]recipeTmplEntry, 0, len(rawRecipes))
	for idStr, recipes := range rawRecipes {
		id, _ := strconv.Atoi(idStr)

		tmplRecipes := make([]recipeTmpl, len(recipes))
		for i, r := range recipes {
			ingredients := make([]ingredientTmpl, len(r.Ingredients))
			for j, ing := range r.Ingredients {
				parsed := schema.ParseIngredient(ing)
				ingredients[j] = ingredientTmpl{ID: parsed.ID, Metadata: fixLogMeta(parsed)}
			}

			inShape := make([][]ingredientTmpl, len(r.InShape))
			for j, row := range r.InShape {
				inShape[j] = make([]ingredientTmpl, len(row))
				for k, cell := range row {
					parsed := schema.ParseIngredient(cell)
					inShape[j][k] = ingredientTmpl{ID: parsed.ID, Metadata: fixLogMeta(parsed)}
				}
			}

			tmplRecipes[i] = recipeTmpl{
				Ingredients: ingredients,
				InShape:     inShape,
				Result:      r.Result,
			}
		}

		result = append(result, recipeTmplEntry{ID: id, Recipes: tmplRecipes})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// fixLogMeta strips log orientation bits from PrismarineJS recipe metadata.
// PrismarineJS uses bark-variant metadata (12-15) for log ingredients, but
// dropped logs in gameplay have simple type metadata (0-3). Strip the upper
// orientation bits so recipes match items players actually have.
func fixLogMeta(ing schema.RecipeIngredient) int {
	if (ing.ID == 17 || ing.ID == 162) && ing.Metadata >= 4 {
		return ing.Metadata & 0x3
	}
	return ing.Metadata
}

// Collision Shapes

type collisionShapesTmpl struct {
	Blocks []collisionBlockTmpl
	Shapes []collisionShapeTmpl
}

type collisionBlockTmpl struct {
	Name     string
	ShapeIDs []int
}

type collisionShapeTmpl struct {
	ID    int
	Boxes [][]float64
}

func loadCollisionShapes(raw []byte) (*collisionShapesTmpl, error) {
	var rawData schema.RawCollisionShapes
	if err := json.Unmarshal(raw, &rawData); err != nil {
		return nil, fmt.Errorf("unmarshal collision shapes: %w", err)
	}

	blocks := make([]collisionBlockTmpl, 0, len(rawData.Blocks))
	for name, rawVal := range rawData.Blocks {
		var shapeIDs []int

		var singleID int
		if err := json.Unmarshal(rawVal, &singleID); err == nil {
			shapeIDs = []int{singleID}
		} else {
			if err := json.Unmarshal(rawVal, &shapeIDs); err != nil {
				return nil, fmt.Errorf("parse block shapes for %s: %w", name, err)
			}
		}

		blocks = append(blocks, collisionBlockTmpl{Name: name, ShapeIDs: shapeIDs})
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Name < blocks[j].Name
	})

	shapes := make([]collisionShapeTmpl, 0, len(rawData.Shapes))
	for idStr, boxes := range rawData.Shapes {
		id, _ := strconv.Atoi(idStr)
		shapes = append(shapes, collisionShapeTmpl{ID: id, Boxes: boxes})
	}

	sort.Slice(shapes, func(i, j int) bool {
		return shapes[i].ID < shapes[j].ID
	})

	return &collisionShapesTmpl{Blocks: blocks, Shapes: shapes}, nil
}

// Protocol

type protocolTmpl struct {
	Types  map[string]string
	Phases []protocolPhaseTmpl
}

type protocolPhaseTmpl struct {
	Name     string
	ToClient []packetTmpl
	ToServer []packetTmpl
}

type packetTmpl struct {
	Name   string
	ID     int
	Fields []packetFieldTmpl
}

type packetFieldTmpl struct {
	Name string
	Type string
}

func loadProtocol(raw []byte) (*protocolTmpl, error) {
	var rawProto map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawProto); err != nil {
		return nil, fmt.Errorf("unmarshal protocol: %w", err)
	}

	// Parse type definitions
	types := make(map[string]string)
	if typesRaw, ok := rawProto["types"]; ok {
		var rawTypes map[string]json.RawMessage
		if err := json.Unmarshal(typesRaw, &rawTypes); err != nil {
			return nil, fmt.Errorf("unmarshal protocol types: %w", err)
		}
		for name, val := range rawTypes {
			var native string
			if err := json.Unmarshal(val, &native); err == nil {
				types[name] = native
			} else {
				types[name] = "complex"
			}
		}
	}

	// Parse phases
	phaseNames := []string{"handshaking", "status", "login", "play"}
	var phases []protocolPhaseTmpl

	for _, phaseName := range phaseNames {
		phaseRaw, ok := rawProto[phaseName]
		if !ok {
			continue
		}

		var phase struct {
			ToClient struct {
				Types map[string]json.RawMessage `json:"types"`
			} `json:"toClient"`
			ToServer struct {
				Types map[string]json.RawMessage `json:"types"`
			} `json:"toServer"`
		}
		if err := json.Unmarshal(phaseRaw, &phase); err != nil {
			return nil, fmt.Errorf("unmarshal phase %s: %w", phaseName, err)
		}

		toClient := extractPackets(phase.ToClient.Types)
		toServer := extractPackets(phase.ToServer.Types)

		phases = append(phases, protocolPhaseTmpl{
			Name:     phaseName,
			ToClient: toClient,
			ToServer: toServer,
		})
	}

	return &protocolTmpl{Types: types, Phases: phases}, nil
}

func extractPackets(types map[string]json.RawMessage) []packetTmpl {
	// Find the "packet" entry which contains mappings
	packetRaw, ok := types["packet"]
	if !ok {
		return nil
	}

	// Parse the packet container to find mappings
	var packetDef []json.RawMessage
	if err := json.Unmarshal(packetRaw, &packetDef); err != nil {
		return nil
	}
	if len(packetDef) < 2 {
		return nil
	}

	var fields []struct {
		Name string          `json:"name"`
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(packetDef[1], &fields); err != nil {
		return nil
	}

	// Find the mapper in the "name" field to get packet ID mappings
	mappings := map[string]int{}
	for _, f := range fields {
		if f.Name != "name" {
			continue
		}
		var mapper []json.RawMessage
		if err := json.Unmarshal(f.Type, &mapper); err != nil {
			continue
		}
		if len(mapper) < 2 {
			continue
		}
		var mapperDef struct {
			Mappings map[string]string `json:"mappings"`
		}
		if err := json.Unmarshal(mapper[1], &mapperDef); err != nil {
			continue
		}
		for hexID, name := range mapperDef.Mappings {
			id, err := strconv.ParseInt(hexID, 0, 64)
			if err != nil {
				continue
			}
			mappings[name] = int(id)
		}
	}

	// Extract packet definitions
	var packets []packetTmpl
	for typeName, typeRaw := range types {
		if typeName == "packet" {
			continue
		}
		if len(typeName) < 8 || typeName[:7] != "packet_" {
			continue
		}

		packetName := typeName[7:] // strip "packet_" prefix
		packetID := mappings[packetName]

		packetFields := extractPacketFields(typeRaw)

		packets = append(packets, packetTmpl{
			Name:   packetName,
			ID:     packetID,
			Fields: packetFields,
		})
	}

	sort.Slice(packets, func(i, j int) bool {
		return packets[i].ID < packets[j].ID
	})

	return packets
}

func isBufferVarInt(raw json.RawMessage) bool {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 2 {
		return false
	}
	var typeName string
	if err := json.Unmarshal(arr[0], &typeName); err != nil || typeName != "buffer" {
		return false
	}
	var opts struct {
		CountType string `json:"countType"`
	}
	if err := json.Unmarshal(arr[1], &opts); err != nil || opts.CountType != "varint" {
		return false
	}
	return true
}

func extractPacketFields(raw json.RawMessage) []packetFieldTmpl {
	var def []json.RawMessage
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil
	}
	if len(def) < 2 {
		return nil
	}

	var fields []struct {
		Name string          `json:"name"`
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(def[1], &fields); err != nil {
		return nil
	}

	var result []packetFieldTmpl
	for _, f := range fields {
		if f.Name == "" {
			continue
		}
		typeName := "complex"
		var simpleType string
		if err := json.Unmarshal(f.Type, &simpleType); err == nil {
			typeName = simpleType
		} else if isBufferVarInt(f.Type) {
			typeName = "ByteArray"
		}
		result = append(result, packetFieldTmpl{Name: f.Name, Type: typeName})
	}

	return result
}

// Packet Structs — generates Go struct definitions with mc tags.

type packetStructsTmpl struct {
	Packets []packetStructDef
}

type packetStructDef struct {
	StructName string
	PacketID   int
	Fields     []packetStructFieldDef
}

type packetStructFieldDef struct {
	GoName string
	GoType string
	McTag  string
}

type typeMapping struct {
	goType string
	mcTag  string
}

var marshalableTypes = map[string]typeMapping{
	"varint":     {"int32", "varint"},
	"varlong":    {"int64", "varlong"},
	"i8":         {"int8", "i8"},
	"u8":         {"uint8", "u8"},
	"i16":        {"int16", "i16"},
	"u16":        {"uint16", "u16"},
	"i32":        {"int32", "i32"},
	"i64":        {"int64", "i64"},
	"f32":        {"float32", "f32"},
	"f64":        {"float64", "f64"},
	"bool":       {"bool", "bool"},
	"string":     {"string", "string"},
	"UUID":       {"[16]byte", "uuid"},
	"position":   {"int64", "position"},
	"ByteArray":  {"[]byte", "bytearray"},
	"restBuffer": {"[]byte", "rest"},
}

func loadPacketStructs(raw []byte) (*packetStructsTmpl, error) {
	proto, err := loadProtocol(raw)
	if err != nil {
		return nil, err
	}

	var allPackets []packetStructDef

	for _, phase := range proto.Phases {
		clientNames := make(map[string]bool)
		for _, p := range phase.ToClient {
			clientNames[p.Name] = true
		}
		serverNames := make(map[string]bool)
		for _, p := range phase.ToServer {
			serverNames[p.Name] = true
		}

		for _, p := range phase.ToClient {
			suffix := ""
			if serverNames[p.Name] {
				suffix = "CB"
			}
			allPackets = append(allPackets, buildPacketStructDef(p, suffix))
		}

		for _, p := range phase.ToServer {
			suffix := ""
			if clientNames[p.Name] {
				suffix = "SB"
			}
			allPackets = append(allPackets, buildPacketStructDef(p, suffix))
		}
	}

	sort.Slice(allPackets, func(i, j int) bool {
		return allPackets[i].StructName < allPackets[j].StructName
	})

	return &packetStructsTmpl{Packets: allPackets}, nil
}

func buildPacketStructDef(p packetTmpl, suffix string) packetStructDef {
	structName := snakeToPascal(p.Name) + suffix

	var fields []packetStructFieldDef
	allMarshalable := true

	for _, f := range p.Fields {
		tm, ok := marshalableTypes[f.Type]
		if !ok {
			allMarshalable = false
			break
		}
		fields = append(fields, packetStructFieldDef{
			GoName: camelToPascal(f.Name),
			GoType: tm.goType,
			McTag:  tm.mcTag,
		})
	}

	if !allMarshalable {
		fields = []packetStructFieldDef{
			{GoName: "Data", GoType: "[]byte", McTag: "rest"},
		}
	}

	return packetStructDef{
		StructName: structName,
		PacketID:   p.ID,
		Fields:     fields,
	}
}

func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return fixAbbreviations(strings.Join(parts, ""))
}

func camelToPascal(s string) string {
	if s == "" {
		return s
	}
	return fixAbbreviations(strings.ToUpper(s[:1]) + s[1:])
}

func fixAbbreviations(s string) string {
	s = strings.ReplaceAll(s, "Uuid", "UUID")
	s = strings.ReplaceAll(s, "Nbt", "NBT")
	s = strings.ReplaceAll(s, "Url", "URL")

	// Fix "Id" at word boundaries (end of string or before uppercase letter).
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == 'I' && s[i+1] == 'd' {
			atEnd := i+2 >= len(s)
			beforeUpper := !atEnd && s[i+2] >= 'A' && s[i+2] <= 'Z'
			if atEnd || beforeUpper {
				b.WriteString("ID")
				i++ // skip 'd'
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
