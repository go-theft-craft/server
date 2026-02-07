package gamedata

type Window struct {
	ID         string
	Name       string
	Slots      []WindowSlot
	Properties []string
	OpenedWith []WindowOpener
}

type WindowSlot struct {
	Name  string
	Index int
	Size  int
}

type WindowOpener struct {
	Type string
	ID   int
}
