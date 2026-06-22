package main

import "github.com/charmbracelet/lipgloss"

// theme is a named colour scheme. Colours are lipgloss colour specs (ANSI index,
// 256-colour index, or hex) and degrade gracefully on limited terminals.
type theme struct {
	name    string
	running lipgloss.Color // RUNNING state, CPU graph
	stopped lipgloss.Color // STOPPED state
	dim     lipgloss.Color // secondary text, separators
	warn    lipgloss.Color // errors, critical thresholds
	caution lipgloss.Color // warning thresholds
	heading lipgloss.Color // section headings
	pointer lipgloss.Color // selection marker, RSS graph
	border  lipgloss.Color // info-panel border
}

// themes are cycled at runtime with the 't' key.
var themes = []theme{
	{name: "default", running: "10", stopped: "11", dim: "8", warn: "9", caution: "11", heading: "12", pointer: "14", border: "8"},
	{name: "nord", running: "#a3be8c", stopped: "#ebcb8b", dim: "#4c566a", warn: "#bf616a", caution: "#d08770", heading: "#81a1c1", pointer: "#88c0d0", border: "#4c566a"},
	{name: "solarized", running: "#859900", stopped: "#b58900", dim: "#586e75", warn: "#dc322f", caution: "#cb4b16", heading: "#268bd2", pointer: "#2aa198", border: "#586e75"},
	{name: "mono", running: "#e4e4e4", stopped: "#9e9e9e", dim: "#6c6c6c", warn: "#ffffff", caution: "#bcbcbc", heading: "#ffffff", pointer: "#ffffff", border: "#6c6c6c"},
}

// Theme-independent styles.
var (
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleInverse = lipgloss.NewStyle().Reverse(true)
)

// Theme-dependent styles, (re)built by applyTheme.
var (
	styleRunning    lipgloss.Style
	styleStopped    lipgloss.Style
	styleDim        lipgloss.Style
	styleWarn       lipgloss.Style
	styleCaution    lipgloss.Style
	styleHeading    lipgloss.Style
	stylePointer    lipgloss.Style
	infoPanelBorder lipgloss.Style
)

func init() { applyTheme(0) }

// applyTheme rebuilds the theme-dependent styles from themes[i] (i is wrapped
// into range). Safe to call from Update — styles are only read in View, on the
// same goroutine.
func applyTheme(i int) {
	t := themes[wrapTheme(i)]
	styleRunning = lipgloss.NewStyle().Foreground(t.running)
	styleStopped = lipgloss.NewStyle().Foreground(t.stopped)
	styleDim = lipgloss.NewStyle().Foreground(t.dim)
	styleWarn = lipgloss.NewStyle().Foreground(t.warn)
	styleCaution = lipgloss.NewStyle().Foreground(t.caution)
	styleHeading = lipgloss.NewStyle().Bold(true).Foreground(t.heading)
	stylePointer = lipgloss.NewStyle().Bold(true).Foreground(t.pointer)
	infoPanelBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.border).
		Padding(0, 1)
}

// themeName returns the name of theme i (wrapped into range).
func themeName(i int) string { return themes[wrapTheme(i)].name }

func wrapTheme(i int) int { return ((i % len(themes)) + len(themes)) % len(themes) }

// selectionStyle controls how the selected row is marked across every table.
type selectionStyle int

const (
	selPointer selectionStyle = iota // ▸ row ◂
	selSidebar                       // │ row │
)

// gutters returns the left/right gutter cells for a row. Each cell is two
// display columns wide; a non-selected row gets blank cells so all rows stay
// column-aligned. The selected row gets either a bold > … < pointer pair or a
// thick solid sidebar bar on each edge.
func (st selectionStyle) gutters(selected bool) (left, right string) {
	if !selected {
		return "  ", "  "
	}
	if st == selSidebar {
		bar := stylePointer.Render("██")
		return bar, bar
	}
	return " " + stylePointer.Render(">"), stylePointer.Render("<") + " "
}
