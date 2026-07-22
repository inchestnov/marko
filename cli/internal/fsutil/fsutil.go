// Package fsutil provides file discovery helpers (find marko.yaml,
// templates/*.yaml).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ConfigFileName is the canonical name of the Marko configuration file.
const ConfigFileName = "marko.yaml"

// FindConfig searches upward from startDir (inclusive) for a file named
// marko.yaml, returning its absolute path. If startDir is empty, the
// current working directory is used. Returns an error if no marko.yaml is
// found before reaching the filesystem root.
func FindConfig(startDir string) (string, error) {
	dir := startDir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("fsutil: getwd: %w", err)
		}
		dir = wd
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("fsutil: resolving %q: %w", dir, err)
	}

	for {
		candidate := filepath.Join(abs, ConfigFileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}

	return "", fmt.Errorf("fsutil: could not find %s in %q or any parent directory", ConfigFileName, dir)
}

// DefaultTemplatesDir returns the default templates directory for a given
// marko.yaml path: "<dir of marko.yaml>/templates".
func DefaultTemplatesDir(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "templates")
}

// DiscoverTemplateFiles returns the sorted list of *.yaml / *.yml files
// directly under templatesDir. If templatesDir does not exist, an empty
// slice (no error) is returned, since a templates/ dir is optional.
func DiscoverTemplateFiles(templatesDir string) ([]string, error) {
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fsutil: reading templates dir %q: %w", templatesDir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, filepath.Join(templatesDir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
