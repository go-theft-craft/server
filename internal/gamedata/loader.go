package gamedata

import "fmt"

var versions = map[string]func() *GameData{}

func Register(name string, factory func() *GameData) {
	versions[name] = factory
}

func Load(name string) (*GameData, error) {
	f, ok := versions[name]
	if !ok {
		return nil, fmt.Errorf("unknown version: %s", name)
	}
	return f(), nil
}

func RegisteredVersions() []string {
	names := make([]string, 0, len(versions))
	for name := range versions {
		names = append(names, name)
	}
	return names
}
