package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ParseNoteSuggestions extracts the process-local package flag and leaves other launcher arguments intact.
func ParseNoteSuggestions(args []string) (string, []string, error) {
	var path string
	var remaining []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, value, inline := strings.Cut(arg, "=")
		if flag != "--note-suggestions" {
			remaining = append(remaining, arg)
			continue
		}
		if !inline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			value = args[i]
		}
		if value == "" || path != "" {
			return "", nil, errors.New("--note-suggestions requires a single local package path")
		}
		path = value
	}
	return path, remaining, nil
}

// noteSuggestionsPath resolves the optional pack only after explicit opt-in.
func resolveNoteSuggestionsPath(explicit string, enabled bool) string {
	if explicit != "" || !enabled {
		return explicit
	}
	dir, _ := ConfigDir()
	executable, _ := os.Executable()
	return findNoteSuggestionsPath(dir, executable)
}

func findNoteSuggestionsPath(configDir, executable string) string {
	const filename = "bert-agreement.bin.gz"
	path := filepath.Join(configDir, "note-suggestions", filename)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	if executable != "" {
		dir := filepath.Dir(executable)
		if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
			bundled := filepath.Join(filepath.Dir(dir), "Resources", "note-suggestions", filename)
			if info, err := os.Stat(bundled); err == nil && info.Mode().IsRegular() {
				return bundled
			}
		}
		beside := filepath.Join(dir, "note-suggestions", filename)
		if info, err := os.Stat(beside); err == nil && info.Mode().IsRegular() {
			return beside
		}
	}
	return path // Service reports this missing path while keeping review available.
}
