package kobaapp

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeFileAtomically(filename string, contents []byte) error {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("koba/kobaapp: create output directory %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".functional-app-*.tmp")
	if err != nil {
		return fmt.Errorf("koba/kobaapp: create temporary artifact: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: set artifact permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: write temporary artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: sync temporary artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("koba/kobaapp: close temporary artifact: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("koba/kobaapp: replace artifact %q: %w", filename, err)
	}
	return nil
}
