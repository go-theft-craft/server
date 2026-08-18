package v775_test

import (
	"github.com/go-theft-craft/minecraft-protocol/data"
)

// dataNode is the shape this test walks the vanilla tree in, so the walk does
// not depend on the generated type's field names beyond the two it reads.
type dataNode struct {
	parser   string
	children []dataNode
}

func collect(set *data.Set) []dataNode {
	return convert(set.Commands().Root.Children)
}

func convert(nodes data.CommandNodes) []dataNode {
	out := make([]dataNode, 0, len(nodes))
	for _, n := range nodes {
		node := dataNode{children: convert(n.Children)}
		if n.Parser != nil {
			node.parser = n.Parser.Name
		}
		out = append(out, node)
	}

	return out
}
