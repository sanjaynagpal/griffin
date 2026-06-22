package main

import (
	"fmt"
	"path/filepath"
	"sort"
)

// ServiceEntry is the resolved, runtime view of one service.
// All paths are absolute so callers never need to join with AppRoot.
type ServiceEntry struct {
	Name          string // registry key, matches folder name
	ComponentRoot string // absolute path: $APP_ROOT/<Name>
	DisplayName   string // from "name" field; falls back to Name
	Port          string
	RunUnix       string // absolute path to run script
	RunWindows    string // absolute path to run script
	PIDFile       string // "" → auto-resolve at runtime via ResolvePIDFile
	TLS           bool
}

// LoadRegistry parses griffin-registry.yaml from appRoot.
// Delegates to LoadServiceList which already handles the file format.
func LoadRegistry(appRoot string) (map[string]RegistryEntry, error) {
	path := filepath.Join(appRoot, registryFilename)
	entries, err := LoadServiceList(path)
	if err != nil {
		return nil, fmt.Errorf("loading registry: %w", err)
	}
	return entries, nil
}

// BuildServiceEntries loads the registry and resolves all paths to absolute
// form. Entries are returned sorted alphabetically by service name.
func BuildServiceEntries(appRoot string) ([]ServiceEntry, error) {
	reg, err := LoadRegistry(appRoot)
	if err != nil {
		return nil, err
	}
	if len(reg) == 0 {
		return nil, fmt.Errorf("registry is empty — run 'griffin init' first")
	}

	entries := make([]ServiceEntry, 0, len(reg))
	for key, r := range reg {
		root := filepath.Join(appRoot, key)
		e := ServiceEntry{
			Name:          key,
			ComponentRoot: root,
			DisplayName:   r.Name,
			Port:          r.Port,
			TLS:           r.TLS,
		}
		if e.DisplayName == "" {
			e.DisplayName = key
		}
		// Resolve run scripts relative to ComponentRoot.
		if r.RunUnix != "" {
			e.RunUnix = filepath.Join(root, r.RunUnix)
		}
		if r.RunWindows != "" {
			e.RunWindows = filepath.Join(root, r.RunWindows)
		}
		// PIDFile: resolve relative paths; keep absolute unchanged.
		if r.PIDFile != "" {
			if filepath.IsAbs(r.PIDFile) {
				e.PIDFile = r.PIDFile
			} else {
				e.PIDFile = filepath.Join(root, r.PIDFile)
			}
		}
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}
