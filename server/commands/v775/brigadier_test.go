package v775_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	java "github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/server"
	"github.com/go-theft-craft/server/server/commands/v775"
	"github.com/go-theft-craft/server/server/commands/vanilla"
)

// Brigadier rendering.
//
// Read the package doc before reading these: nothing here speaks protocol 775,
// so the tree is checked against a codec and a data set and never against a
// client. These tests say the rendering is well formed and uses parser names
// vanilla also uses. They do not say a client would draw it.

func literal(nodes []v26_1.PlayClientboundDeclareCommandsNodesItem, root int32, name string) (int32, bool) {
	for _, child := range nodes[root].Children {
		if nodes[child].ExtraNodeData.Case1.Name == name {
			return child, true
		}
	}

	return 0, false
}

func TestASetRendersToARootWithOneChildPerCommand(t *testing.T) {
	set := server.BuiltinCommands()

	packet, err := v775.RenderCommands(set)
	if err != nil {
		t.Fatalf("RenderCommands: %v", err)
	}
	if packet.RootIndex != 0 {
		t.Errorf("root index is %d, want 0", packet.RootIndex)
	}
	if got := packet.Nodes[0].Flags.CommandNodeType; got != 0 {
		t.Errorf("node 0 is type %d, want the root type 0", got)
	}

	for _, cmd := range set.All() {
		if _, ok := literal(packet.Nodes, packet.RootIndex, cmd.Name); !ok {
			t.Errorf("/%s has no literal under the root", cmd.Name)
		}
	}
	if got := len(packet.Nodes[0].Children); got != len(set.All()) {
		t.Errorf("the root has %d children for %d commands", got, len(set.All()))
	}
}

func TestAnOverloadBecomesABranch(t *testing.T) {
	set := server.BuiltinCommands()

	packet, err := v775.RenderCommands(set)
	if err != nil {
		t.Fatalf("RenderCommands: %v", err)
	}

	tp, ok := literal(packet.Nodes, packet.RootIndex, "tp")
	if !ok {
		t.Fatal("/tp is not in the tree")
	}
	// Two shapes, two branches: <player> and <x> <y> <z> are separate paths
	// rather than one path with an ambiguous middle.
	if got := len(packet.Nodes[tp].Children); got != 2 {
		t.Fatalf("/tp has %d branches, want 2", got)
	}

	for _, child := range packet.Nodes[tp].Children {
		arg := packet.Nodes[child]
		if arg.Flags.CommandNodeType != 2 {
			t.Errorf("/tp's branch %q is not an argument node", arg.ExtraNodeData.Case2.Name)
		}
		switch arg.ExtraNodeData.Case2.Name {
		case "player":
			if arg.Flags.HasCommand != 1 {
				t.Error("/tp <player> is not executable")
			}
		case "x":
			// x is not executable on its own; the command ends at z.
			if arg.Flags.HasCommand == 1 {
				t.Error("/tp <x> is executable on its own")
			}
			if len(arg.Children) != 1 {
				t.Fatalf("/tp <x> has %d children, want <y>", len(arg.Children))
			}
		default:
			t.Errorf("/tp has an unexpected branch %q", arg.ExtraNodeData.Case2.Name)
		}
	}

	// A no-argument command is executable at its literal.
	seed, ok := literal(packet.Nodes, packet.RootIndex, "seed")
	if !ok {
		t.Fatal("/seed is not in the tree")
	}
	if packet.Nodes[seed].Flags.HasCommand != 1 {
		t.Error("/seed takes no arguments and is not executable at its own node")
	}

	// An optional trailing parameter makes the node before it executable, so
	// /help runs with or without a command name.
	help, ok := literal(packet.Nodes, packet.RootIndex, "help")
	if !ok {
		t.Fatal("/help is not in the tree")
	}
	if packet.Nodes[help].Flags.HasCommand != 1 {
		t.Error("/help's argument is optional and /help alone is not executable")
	}
}

func TestTheTreeRoundTripsThroughTheGeneratedCodec(t *testing.T) {
	// The whole set, stubs included, because that is the tree a real server
	// would send and it is far larger than the built-ins alone.
	packet, err := v775.RenderCommands(server.Merge(vanilla.Stubs(), server.BuiltinCommands()))
	if err != nil {
		t.Fatalf("RenderCommands: %v", err)
	}

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}

	out, err := java.NewWriteBuffer(limits)
	if err != nil {
		t.Fatalf("NewWriteBuffer: %v", err)
	}
	if err := packet.Encode(out); err != nil {
		t.Fatalf("encode the tree: %v", err)
	}

	in, err := java.NewReadBuffer(out.Bytes(), limits)
	if err != nil {
		t.Fatalf("NewReadBuffer: %v", err)
	}
	var back v26_1.PlayClientboundDeclareCommands
	if err := back.Decode(in); err != nil {
		t.Fatalf("decode the tree: %v", err)
	}

	if len(back.Nodes) != len(packet.Nodes) {
		t.Fatalf("decoded %d nodes, encoded %d", len(back.Nodes), len(packet.Nodes))
	}
	if back.RootIndex != packet.RootIndex {
		t.Errorf("root index came back %d, want %d", back.RootIndex, packet.RootIndex)
	}
	for i := range back.Nodes {
		if back.Nodes[i].ExtraNodeData.Case1.Name != packet.Nodes[i].ExtraNodeData.Case1.Name {
			t.Errorf("node %d literal name came back %q, want %q", i,
				back.Nodes[i].ExtraNodeData.Case1.Name, packet.Nodes[i].ExtraNodeData.Case1.Name)
		}
		if back.Nodes[i].ExtraNodeData.Case2.Parser != packet.Nodes[i].ExtraNodeData.Case2.Parser {
			t.Errorf("node %d parser came back %q, want %q", i,
				back.Nodes[i].ExtraNodeData.Case2.Parser, packet.Nodes[i].ExtraNodeData.Case2.Parser)
		}
	}
}

func TestEmittedParserNamesAreOnesVanillaAlsoUses(t *testing.T) {
	// Weaker than equality, deliberately: these are stubs and their signatures
	// are not vanilla's, so their trees will not match. What can be checked is
	// that every parser name this renderer emits is one the real 26.1 command
	// tree also uses — a name vanilla has never heard of is a client that
	// cannot decode the packet.
	set, err := v26_1.Data()
	if err != nil {
		t.Fatalf("v26_1 data: %v", err)
	}

	known := map[string]bool{}
	var walk func(nodes []dataNode)
	walk = func(nodes []dataNode) {
		for _, n := range nodes {
			if n.parser != "" {
				known[n.parser] = true
			}
			walk(n.children)
		}
	}
	walk(collect(set))

	if len(known) == 0 {
		t.Fatal("the 26.1 command data names no parsers; the check would pass vacuously")
	}

	for _, pt := range []server.ParamType{
		server.ParamWord, server.ParamMessage, server.ParamInt, server.ParamFloat,
		server.ParamPlayer, server.ParamCoordinates, server.ParamDuration, server.ParamCommand,
	} {
		parser, err := v775.ParserFor(pt)
		if err != nil {
			t.Errorf("no parser for %q: %v", pt, err)

			continue
		}
		if !known[parser] {
			t.Errorf("parameter type %q renders as %q, which the vanilla 26.1 tree never uses", pt, parser)
		}
	}
}

func TestAnUnmappedParameterTypeIsAnErrorRatherThanAGuess(t *testing.T) {
	// The table is the version boundary. A ParamType added without a line in
	// it must fail here rather than emit a parser name the client does not
	// know, which would be a packet the client cannot decode.
	if _, err := v775.ParserFor(server.ParamType("teleport-destination")); err == nil {
		t.Fatal("an unmapped parameter type produced a parser")
	}

	set, err := server.NewSet(server.Command{
		Name: "invent",
		Signature: server.Signature{Overloads: []server.Overload{{Params: []server.Param{
			{Name: "thing", Type: server.ParamType("nonesuch")},
		}}}},
	})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	if _, err := v775.RenderCommands(set); err == nil {
		t.Error("a command with an unmapped parameter type rendered anyway")
	}
}
