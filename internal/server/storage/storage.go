// Package storage holds the file primitives the stores are built from and the
// one-way migration off the JSON world files.
//
// The stores themselves live in the server package: they hand back public
// value types — PlayerData, LevelData, Sidecar — and an internal package
// cannot name them.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
)

// EnsureDir creates a directory and everything above it.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	return nil
}

// WriteJSONAtomic marshals v and replaces path with it in one rename, so a
// reader sees either the whole previous file or the whole new one.
func WriteJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)

		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// ReadJSON decodes path into v. It reports false, and no error, for a file
// that is not there, which is how a caller tells "never saved" from "broken".
func ReadJSON(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}

	return true, nil
}
