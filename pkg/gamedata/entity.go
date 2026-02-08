package gamedata

type Entity struct {
	ID          int
	InternalID  int
	Name        string
	DisplayName string
	Type        string
	Width       *float64
	Height      *float64
	Category    string
}
