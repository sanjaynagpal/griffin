package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const registryFilename = "griffin-registry.yaml"

// RegistryEntry is one service in griffin-registry.yaml.
// Fields with dots in their yaml tags (run.unix, pid.file) are standard YAML
// string keys — yaml.v3 treats the tag value as a literal key name.
type RegistryEntry struct {
	Name       string `yaml:"name,omitempty"`
	Port       string `yaml:"port,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
	RunUnix    string `yaml:"run.unix"`
	RunWindows string `yaml:"run.windows"`
	PIDFile    string `yaml:"pid.file,omitempty"`
}

// LoadServiceList parses a user-supplied YAML file whose keys are service
// names and whose values are RegistryEntry fields. Operators use this as the
// preferred input to griffin init --file.
func LoadServiceList(path string) (map[string]RegistryEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading service list %q: %w", path, err)
	}
	var entries map[string]RegistryEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing service list %q: %w", path, err)
	}
	return entries, nil
}

// ScanCandidates returns the names of immediate child directories of appRoot
// that contain both a bin/ and a cfg/ subdirectory. Results are sorted
// alphabetically.
func ScanCandidates(appRoot string) ([]string, error) {
	dirs, err := os.ReadDir(appRoot)
	if err != nil {
		return nil, fmt.Errorf("scanning %q: %w", appRoot, err)
	}
	var names []string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if dirExists(filepath.Join(appRoot, d.Name(), "bin")) &&
			dirExists(filepath.Join(appRoot, d.Name(), "cfg")) {
			names = append(names, d.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// BuildStubEntries creates registry entries with '# TODO' placeholders for
// all required fields. The operator must replace these before running griffin.
func BuildStubEntries(names []string) map[string]RegistryEntry {
	entries := make(map[string]RegistryEntry, len(names))
	for _, name := range names {
		entries[name] = RegistryEntry{
			RunUnix:    "# TODO",
			RunWindows: "# TODO",
		}
	}
	return entries
}

// WriteRegistry appends new service entries to $appRoot/griffin-registry.yaml,
// skipping any name that is already present (idempotent). It returns the number
// of entries written and the number skipped.
func WriteRegistry(appRoot string, entries map[string]RegistryEntry) (written, skipped int, err error) {
	path := filepath.Join(appRoot, registryFilename)

	existing, err := loadExistingNames(path)
	if err != nil {
		return 0, 0, err
	}

	// Collect new names in sorted order so the registry is deterministic.
	var newNames []string
	for name := range entries {
		if existing[name] {
			skipped++
		} else {
			newNames = append(newNames, name)
		}
	}
	sort.Strings(newNames)

	if len(newNames) == 0 {
		return 0, skipped, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, skipped, fmt.Errorf("opening registry %q: %w", path, err)
	}
	defer f.Close()

	for _, name := range newNames {
		if _, werr := fmt.Fprint(f, formatEntry(name, entries[name])); werr != nil {
			return written, skipped, fmt.Errorf("writing entry %q: %w", name, werr)
		}
		written++
	}
	return written, skipped, nil
}

// loadExistingNames reads the registry file (if it exists) and returns the set
// of service names already present. A missing file is not an error.
func loadExistingNames(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}
	// Parse only the top-level keys; we don't need field values here.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}
	names := make(map[string]bool, len(raw))
	for k := range raw {
		names[k] = true
	}
	return names, nil
}

// formatEntry serialises a single service entry as a YAML block. Fields are
// emitted in the canonical spec order. run.unix and run.windows are always
// written; optional fields are omitted when empty.
func formatEntry(name string, e RegistryEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", name)
	if e.Name != "" {
		fmt.Fprintf(&b, "  name: %s\n", yamlScalar(e.Name))
	}
	if e.Port != "" {
		// Ports are quoted strings so they are never interpreted as numbers.
		fmt.Fprintf(&b, "  port: %q\n", e.Port)
	}
	if e.TLS {
		fmt.Fprintf(&b, "  tls: true\n")
	}
	fmt.Fprintf(&b, "  run.unix: %s\n", yamlScalar(e.RunUnix))
	fmt.Fprintf(&b, "  run.windows: %s\n", yamlScalar(e.RunWindows))
	if e.PIDFile != "" {
		fmt.Fprintf(&b, "  pid.file: %s\n", yamlScalar(e.PIDFile))
	}
	b.WriteByte('\n') // blank line between entries for readability
	return b.String()
}

// yamlScalar returns a YAML-safe representation of s. Strings that start with
// a character that YAML would interpret as a comment or structural marker are
// wrapped in single quotes. Single quotes within the string are escaped by
// doubling them per the YAML specification.
func yamlScalar(s string) string {
	if s == "" {
		return "''"
	}
	// Characters that have special meaning when they open a plain scalar.
	const specialFirst = "#%&*?|>{[@!"
	if strings.ContainsRune(specialFirst, rune(s[0])) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
