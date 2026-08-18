// Package v775 renders a command set as the brigadier tree protocol 775 sends.
//
// It is the second rendering of the same Signature. Protocol 47 has no command
// packet at all — a 1.8 client learns what a server takes by being told, one
// tab-complete at a time — and 775 sends the whole tree at login. One
// declaration producing both is the claim the command design makes, and a claim
// with only one implementation is not a boundary.
//
// # What is not proved
//
// Nothing in this repository speaks protocol 775. The tree below is checked
// against the generated codec and against the vanilla command data for that
// version; it has never been sent to a client, and a green test here is not
// evidence that a 775 client would draw the right suggestions. That is the same
// limit M11.2 records for its 775 block-state registry round-trip, and it is
// worth reading twice before anybody treats this as a working 775 server.
package v775

import (
	"fmt"

	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/server/server"
)

// Brigadier parser names, one per ParamType.
//
// This table is the whole version boundary for commands, which is why it is one
// table in one place a reviewer can read in full rather than a switch spread
// through a renderer. Eight entries, one per type; adding a ParamType without
// adding a line here fails ParserFor rather than emitting a parser the client
// does not know.
var parsers = map[server.ParamType]string{
	server.ParamWord:        "brigadier:string",
	server.ParamMessage:     "minecraft:message",
	server.ParamInt:         "brigadier:integer",
	server.ParamFloat:       "brigadier:double",
	server.ParamPlayer:      "minecraft:entity",
	server.ParamCoordinates: "brigadier:double",
	server.ParamDuration:    "minecraft:time",
	// A command name is a word to brigadier. Vanilla's own /help takes
	// minecraft:message for the same argument, and a string is the closer of
	// the two to what this server actually accepts.
	server.ParamCommand: "brigadier:string",
}

// The string-parser property values 775 encodes. A word is one token, a
// message is the rest of the line, and the client uses this to decide where an
// argument ends.
const (
	stringSingleWord  = "SINGLE_WORD"
	stringGreedyPhase = "GREEDY_PHRASE"
)

// ParserFor is the brigadier parser a parameter type renders as.
func ParserFor(t server.ParamType) (string, error) {
	parser, ok := parsers[t]
	if !ok {
		return "", fmt.Errorf("v775: no brigadier parser for parameter type %q", t)
	}

	return parser, nil
}

// node is one tree node before it is flattened into the packet's index-based
// form.
type node struct {
	kind       uint8 // 0 root, 1 literal, 2 argument
	name       string
	parser     string
	stringKind string
	executable bool
	children   []*node
}

// The node type values the flags byte carries.
const (
	nodeRoot     uint8 = 0
	nodeLiteral  uint8 = 1
	nodeArgument uint8 = 2
)

// RenderCommands turns a set into the packet 775 sends at login.
//
// Every command becomes a literal child of the root. Every overload becomes a
// branch below that literal, so two shapes of /tp are two paths rather than one
// path with an ambiguous middle. An unimplemented command is still rendered:
// the client should show it and be told it does nothing when it is run, which
// is the same answer vanilla.Stubs() gives on protocol 47.
func RenderCommands(set server.Set) (*v26_1.PlayClientboundDeclareCommands, error) {
	root := &node{kind: nodeRoot}

	for _, cmd := range set.All() {
		literal := &node{kind: nodeLiteral, name: cmd.Name}
		root.children = append(root.children, literal)

		if len(cmd.Signature.Overloads) == 0 {
			literal.executable = true
		}

		for _, overload := range cmd.Signature.Overloads {
			if err := attach(literal, overload); err != nil {
				return nil, fmt.Errorf("render /%s: %w", cmd.Name, err)
			}
		}

		// An alias is its own literal that redirects to the real one in
		// vanilla's tree. This renders it as a second literal with the same
		// children instead: a redirect is a graph edge and the tree below is
		// built as a tree, and duplicating a handful of small subtrees is
		// cheaper than carrying a graph through the flattening.
		for _, alias := range cmd.Aliases {
			root.children = append(root.children, &node{
				kind:       nodeLiteral,
				name:       alias,
				executable: literal.executable,
				children:   literal.children,
			})
		}
	}

	return flatten(root), nil
}

// attach hangs one overload's parameters off a command's literal node.
func attach(parent *node, o server.Overload) error {
	if len(o.Params) == 0 {
		parent.executable = true

		return nil
	}

	current := parent
	for _, p := range o.Params {
		parser, err := ParserFor(p.Type)
		if err != nil {
			return err
		}

		child := &node{kind: nodeArgument, name: p.Name, parser: parser}
		if parser == "brigadier:string" {
			child.stringKind = stringSingleWord
		}
		if p.Type == server.ParamMessage {
			child.stringKind = stringGreedyPhase
		}

		// An optional parameter means the command may end before it, so the
		// node before it is executable too.
		if p.Optional {
			current.executable = true
		}

		current.children = append(current.children, child)
		current = child
	}
	current.executable = true

	return nil
}

// flatten walks the tree depth-first and writes it as the packet's flat node
// array, where a child is an index rather than a pointer.
func flatten(root *node) *v26_1.PlayClientboundDeclareCommands {
	var order []*node
	index := map[*node]int32{}

	var walk func(n *node)
	walk = func(n *node) {
		if _, seen := index[n]; seen {
			return
		}
		index[n] = int32(len(order))
		order = append(order, n)
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)

	nodes := make([]v26_1.PlayClientboundDeclareCommandsNodesItem, len(order))
	for i, n := range order {
		item := v26_1.PlayClientboundDeclareCommandsNodesItem{
			Flags: v26_1.PlayClientboundDeclareCommandsNodesItemFlagsBits{
				CommandNodeType: n.kind,
				HasCommand:      boolBit(n.executable),
			},
		}
		for _, child := range n.children {
			item.Children = append(item.Children, index[child])
		}

		switch n.kind {
		case nodeLiteral:
			item.ExtraNodeData.Case1.Name = n.name
		case nodeArgument:
			item.ExtraNodeData.Case2.Name = n.name
			item.ExtraNodeData.Case2.Parser = n.parser
			item.ExtraNodeData.Case2.Properties.BrigadierString = n.stringKind
		}

		nodes[i] = item
	}

	return &v26_1.PlayClientboundDeclareCommands{Nodes: nodes, RootIndex: 0}
}

func boolBit(b bool) uint8 {
	if b {
		return 1
	}

	return 0
}
