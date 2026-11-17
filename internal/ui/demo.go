package ui

import "charm.land/lipgloss/v2"

var demoSt = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB454"))

const (
	demoTag  = "  DEMO"
	demoHelp = "DEMO: simulated data, no server or GPU needed · q quit"
	demoShort = "DEMO: simulated data · q quit"
)

// demoTitle is the suffix pane titles carry in demo mode.
func (a *App) demoTitle() string {
	if a.demo {
		return demoSt.Render(demoTag)
	}
	return ""
}
