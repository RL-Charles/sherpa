// Package palette is Sherpa's colour scheme: Nord, as in OpenCode's "nord"
// theme. Every colour the TUI and the rendered markdown use comes from here,
// by role, so retuning the scheme is one file. Dark terminals get Nord's own
// values; light terminals get darker shades of the same hues for contrast.
package palette

import "github.com/charmbracelet/lipgloss"

// The Nord palette (nordtheme.com).
const (
	Nord0  = "#2E3440" // polar night
	Nord1  = "#3B4252"
	Nord2  = "#434C5E"
	Nord3  = "#4C566A"
	Nord4  = "#D8DEE9" // snow storm
	Nord5  = "#E5E9F0"
	Nord6  = "#ECEFF4"
	Nord7  = "#8FBCBB" // frost
	Nord8  = "#88C0D0"
	Nord9  = "#81A1C1"
	Nord10 = "#5E81AC"
	Nord11 = "#BF616A" // aurora
	Nord12 = "#D08770"
	Nord13 = "#EBCB8B"
	Nord14 = "#A3BE8C"
	Nord15 = "#B48EAD"

	// OpenCode's nord theme mutes secondary text with this grey.
	MutedHex = "#8B95A7"
)

func c(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	// Roles.
	Primary   = c(Nord10, Nord8)    // titles, focus, headings
	Secondary = c(Nord10, Nord9)    // ready, section headers, links
	Accent    = c("#4C8584", Nord7) // live agent activity
	Muted     = c(Nord3, MutedHex)  // faint text, metadata
	Subtle    = c("#7B88A1", "#616E88")
	ID        = c(Nord3, "#7B88A1")
	Text      = c(Nord0, Nord4)
	Bright    = c(Nord0, Nord6)

	// Aurora, as status and signal colours.
	Red    = c(Nord11, Nord11)
	Orange = c("#A35C3A", Nord12)
	Yellow = c("#8E6A12", Nord13)
	Green  = c("#5E7F4A", Nord14)
	Purple = c("#8A5F84", Nord15)

	// Surfaces.
	OnAccent      = c(Nord6, Nord0) // text on a filled badge
	SelectedBg    = c(Nord4, Nord2)
	SelectedFg    = c(Nord0, Nord6)
	SelectedDimBg = c(Nord5, Nord1)
	SelectedDimFg = c(Nord1, Nord4)
	BorderDim     = c(Nord4, Nord3)
)
