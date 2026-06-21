package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// model is the top-level Bubble Tea model. It owns the active view and all
// shared state (service list, metrics, history). Individual views (statusview,
// infopanel, metricsview, logview) are embedded as sub-models and delegated
// to from Update and View.
type model struct {
	cfg Config
}

// initialModel constructs the root model from the resolved configuration.
func initialModel(cfg Config) model {
	return model{cfg: cfg}
}

// Init is called once when the program starts. Commands returned here run
// before the first Update.
func (m model) Init() tea.Cmd {
	return nil
}

// Update handles all incoming messages and delegates to the active sub-model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders the current active view to a string. The Bubble Tea runtime
// writes this string to the terminal on each frame.
func (m model) View() string {
	return "Griffin — press q to quit\n"
}
