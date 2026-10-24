package launcher

import (
	"fmt"
	"os"
	"path/filepath"

	"track-tagger/internal/manual"
)

// openHelp refreshes the standalone manual for each opening. The caller
// supplies the cache location and shell opener so failures can be tested.
func openHelp(cacheDir func() (string, error), open func(string) error) error {
	root, err := cacheDir()
	if err != nil {
		return fmt.Errorf("find user cache: %w", err)
	}
	dir := filepath.Join(root, "track-tagger", "help")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("prepare help cache: %w", err)
	}
	content, err := manual.Render(false)
	if err != nil {
		return fmt.Errorf("render help: %w", err)
	}
	f, err := os.CreateTemp(dir, "manual-*.html")
	if err != nil {
		return fmt.Errorf("create help file: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("write help file: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close help file: %w", err)
	}
	path := filepath.Join(dir, "manual.html")
	if err := replaceSettings(name, path); err != nil {
		return fmt.Errorf("refresh help file: %w", err)
	}
	if err := open(path); err != nil {
		return fmt.Errorf("open help file: %w", err)
	}
	return nil
}
