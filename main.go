package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Config holds runtime configuration resolved from the environment.
type Config struct {
	AppRoot string // resolved from $APP_ROOT
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "griffin:", err)
		os.Exit(1)
	}

	args := os.Args[1:]

	switch {
	case len(args) == 0:
		runTUI(cfg)

	case args[0] == "init":
		file := ""
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--file" {
				file = args[i+1]
				break
			}
		}
		runInit(cfg, file)

	default:
		printUsage()
		os.Exit(1)
	}
}

// loadConfig reads and validates the runtime configuration from the environment.
func loadConfig() (Config, error) {
	root := os.Getenv("APP_ROOT")
	if root == "" {
		return Config{}, fmt.Errorf("APP_ROOT environment variable is not set")
	}

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("APP_ROOT directory does not exist: %s", root)
		}
		return Config{}, fmt.Errorf("cannot access APP_ROOT %s: %w", root, err)
	}
	if !info.IsDir() {
		return Config{}, fmt.Errorf("APP_ROOT is not a directory: %s", root)
	}

	return Config{AppRoot: root}, nil
}

// runTUI starts the interactive terminal UI.
func runTUI(cfg Config) {
	p := tea.NewProgram(initialModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "griffin:", err)
		os.Exit(1)
	}
}

// runInit runs the griffin init command. When file is non-empty the service
// list is read from that path; otherwise APP_ROOT is scanned for candidates.
func runInit(cfg Config, file string) {
	var (
		entries map[string]RegistryEntry
		source  string
		err     error
	)

	if file != "" {
		entries, err = LoadServiceList(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "griffin init:", err)
			os.Exit(1)
		}
		source = fmt.Sprintf("service list %q", file)
	} else {
		names, scanErr := ScanCandidates(cfg.AppRoot)
		if scanErr != nil {
			fmt.Fprintln(os.Stderr, "griffin init:", scanErr)
			os.Exit(1)
		}
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "griffin init: no service candidates found in %s\n", cfg.AppRoot)
			fmt.Fprintf(os.Stderr, "  (a service candidate is a subdirectory containing both bin/ and cfg/)\n")
			os.Exit(1)
		}
		entries = BuildStubEntries(names)
		source = fmt.Sprintf("scan of %s", cfg.AppRoot)
	}

	written, skipped, err := WriteRegistry(cfg.AppRoot, entries)
	if err != nil {
		fmt.Fprintln(os.Stderr, "griffin init:", err)
		os.Exit(1)
	}

	registryPath := filepath.Join(cfg.AppRoot, registryFilename)
	fmt.Printf("source:   %s\n", source)
	fmt.Printf("registry: %s\n", registryPath)
	fmt.Printf("written:  %d   skipped: %d\n", written, skipped)

	if written > 0 && file == "" {
		fmt.Println()
		fmt.Println("Next: open the registry and replace every '# TODO' with the actual run command path.")
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  griffin                    start the TUI supervisor")
	fmt.Fprintln(os.Stderr, "  griffin init               scan APP_ROOT and create registry stubs")
	fmt.Fprintln(os.Stderr, "  griffin init --file <path> create registry from a service list file")
}
