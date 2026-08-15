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

	if err := os.MkdirAll(outPath, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	// Only the packet structs are generated here now. Every registry this
	// tool used to emit — blocks, items, recipes, and the rest — comes from
	// minecraft-protocol's data package instead. M6 deletes what is left,
	// when the packet structs move to generated types too.
	raw, err := os.ReadFile(filepath.Join(cfg.SchemeDir, "protocol.json"))
	if err != nil {
		return fmt.Errorf("read protocol.json: %w", err)
	}

	packets, err := loadPacketStructs(raw)
	if err != nil {
		return fmt.Errorf("parse protocol.json: %w", err)
	}

	outFile := filepath.Join(outPath, "packets.go")
	rendered := templateData{Package: cfg.Package, Version: cfg.Version, Data: packets}
	if err := renderToFile(tmpl, "packets.go.tmpl", outFile, rendered); err != nil {
		return fmt.Errorf("generate packets.go: %w", err)
	}

	fmt.Printf("  generated packets.go\n")

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

// Version

// Language

// Materials

// Recipes

// Collision Shapes

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
