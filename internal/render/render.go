// Package render draws items for the terminal: status-coloured trees and
// Glamour-rendered markdown.
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

var (
	dim   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	bold  = lipgloss.NewStyle().Bold(true)
	idCol = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "244"})
)

// Age is a compact, human duration since t: "just now", "5m", "3h", "2d",
// "4w", "3mo", "1y". It is used wherever an absolute timestamp would make
// the reader do arithmetic.
func Age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// Ago is Age with a suffix, except for "just now".
func Ago(t time.Time) string {
	a := Age(t)
	if a == "" || a == "just now" {
		return a
	}
	return a + " ago"
}

// statusColor keeps the seven states visually distinct. The palette is
// adaptive so light terminals keep contrast, and red is reserved for blocked.
var statusColor = map[model.Status]lipgloss.TerminalColor{
	model.Backlog:     lipgloss.AdaptiveColor{Light: "245", Dark: "240"},
	model.Ready:       lipgloss.AdaptiveColor{Light: "25", Dark: "39"},
	model.InProgress:  lipgloss.AdaptiveColor{Light: "136", Dark: "214"},
	model.AgentReview: lipgloss.AdaptiveColor{Light: "91", Dark: "141"},
	model.Review:      lipgloss.AdaptiveColor{Light: "162", Dark: "205"},
	model.Blocked:     lipgloss.AdaptiveColor{Light: "124", Dark: "203"},
	model.Done:        lipgloss.AdaptiveColor{Light: "28", Dark: "78"},
}

// StatusStyle colours text by status.
func StatusStyle(s model.Status) lipgloss.Style {
	c, ok := statusColor[s]
	if !ok {
		c = lipgloss.AdaptiveColor{Light: "245", Dark: "240"}
	}
	return lipgloss.NewStyle().Foreground(c)
}

// statusWord is the human label used in CLI output and the labelled tree.
func statusWord(s model.Status) string { return strings.ReplaceAll(string(s), "_", " ") }

// Line is a one-line summary: glyph, ID, title, status, assignee.
func Line(it *model.Item) string {
	st := StatusStyle(it.Status)
	title := it.Title
	if it.Kind == model.Epic {
		title = bold.Render(title)
	}
	s := fmt.Sprintf("%s %s %s %s", st.Render(it.Status.Glyph()), idCol.Render(it.ID), title, st.Render(statusWord(it.Status)))
	if it.Assignee != "" {
		s += dim.Render(" @" + it.Assignee)
	}
	return s
}

// Tree prints epics → stories → tickets with box-drawing connectors.
// Items whose parent is missing are printed at the root so nothing is hidden.
func Tree(w io.Writer, items []*model.Item) {
	byID := map[string]bool{}
	for _, it := range items {
		byID[it.ID] = true
	}
	var roots []*model.Item
	for _, it := range items {
		if it.Parent == "" || !byID[it.Parent] {
			roots = append(roots, it)
		}
	}
	var walk func(it *model.Item, prefix string, last bool, root bool)
	walk = func(it *model.Item, prefix string, last bool, root bool) {
		branch, next := "├─ ", "│  "
		if last {
			branch, next = "└─ ", "   "
		}
		if root {
			branch, next = "", ""
		}
		fmt.Fprintln(w, dim.Render(prefix+branch)+Line(it))
		kids := store.Children(items, it.ID)
		for i, k := range kids {
			walk(k, prefix+next, i == len(kids)-1, false)
		}
	}
	for _, r := range roots {
		walk(r, "", true, true)
	}
}

// Markdown renders md with Glamour when stdout is a terminal, raw otherwise.
func Markdown(md string) string {
	if !IsTTY() {
		return md
	}
	// Cap the measure: 120-column prose is tiring to read even on a wide
	// terminal.
	width := 100
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		width = min(w-2, 100)
	}
	out, err := MarkdownWidth(md, width, "")
	if err != nil {
		return md
	}
	return out
}

// MarkdownWidth renders md wrapped to width. style is "dark" or "light";
// empty means detect from the terminal, which must not be done while a TUI
// owns stdin.
func MarkdownWidth(md string, width int, style string) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithStyles(Style(style)), glamour.WithWordWrap(width))
	if err != nil {
		return "", err
	}
	return r.Render(md)
}

// Style is Glamour's standard style with heading markers ("## ") removed;
// heading levels are told apart by colour, weight and underline instead.
func Style(name string) gansi.StyleConfig {
	if name == "" {
		name = styles.LightStyle
		if lipgloss.HasDarkBackground() {
			name = styles.DarkStyle
		}
	}
	cfg, h2, h3, minor := styles.DarkStyleConfig, "39", "75", "245"
	if name == styles.LightStyle {
		cfg, h2, h3, minor = styles.LightStyleConfig, "27", "33", "240"
	}
	heading := func(color string, underline bool) gansi.StyleBlock {
		return gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: &color, Bold: &yes, Underline: &underline}}
	}
	cfg.H2 = heading(h2, true)
	cfg.H2.BlockPrefix = "\n" + strings.Repeat("─", 28) + "\n"
	cfg.H3 = heading(h3, false)
	cfg.H4 = heading(minor, false)
	cfg.H5 = heading(minor, false)
	cfg.H6 = heading(minor, false)
	return cfg
}

var yes = true

// Document turns an item into a markdown page: header, body, then the
// generated sections and children. all is every item, for rollups.
func Document(it *model.Item, parent *model.Item, kids []*model.Item, all ...*model.Item) string {
	return DocHeader(it, parent, all...) + it.Body + Sections(it, all, kids)
}

// DocHeader is the read-only title, metadata, badges and Now line shown
// above an item's body.
func DocHeader(it *model.Item, parent *model.Item, all ...*model.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n\n", it.ID, it.Title)
	meta := []string{"`" + string(it.Kind) + "`", it.Status.Glyph() + " **" + statusWord(it.Status) + "**"}
	if it.Template != "" {
		meta = append(meta, "**"+it.Template+"**")
	}
	if it.Priority != "" {
		meta = append(meta, "**"+it.Priority+"**")
	}
	if it.Assignee != "" {
		meta = append(meta, "@"+it.Assignee)
	}
	for _, l := range it.Labels {
		meta = append(meta, "#"+l)
	}
	meta = append(meta, "_updated "+Ago(it.Updated)+"_")
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	if badges := Badges(it, all); badges != "" {
		b.WriteString(badges + "\n\n")
	}
	if it.Now != "" {
		b.WriteString("**Now:** " + it.Now + "\n\n")
	}
	if parent != nil {
		fmt.Fprintf(&b, "> in **%s** · %s\n\n", parent.ID, parent.Title)
	}
	return b.String()
}

// PreviewHeader is the compact, pinned strip above the TUI document: title,
// one status/metadata line, and Now. The full DocHeader (badges, parent quote)
// stays for `shp show`; the TUI keeps the essential identity on screen and
// lets the rest scroll.
func PreviewHeader(it *model.Item, parent *model.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n\n", it.ID, it.Title)
	meta := []string{it.Status.Glyph() + " **" + statusWord(it.Status) + "**"}
	if it.Template != "" {
		meta = append(meta, it.Template)
	}
	if it.Assignee != "" {
		meta = append(meta, "@"+it.Assignee)
	}
	if parent != nil {
		meta = append(meta, parent.ID)
	}
	if r, t := it.CheckCounts(); t > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d checks", r, t))
	}
	if d, t := it.DeliverableCounts(); t > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d deliverables", d, t))
	}
	if len(it.Code) > 0 {
		meta = append(meta, fmt.Sprintf("%d files", len(it.Code)))
	}
	if l := it.Links; l != nil && l.PRNumber > 0 {
		meta = append(meta, fmt.Sprintf("⇡ #%d", l.PRNumber))
	}
	if age := Ago(it.Updated); age != "" {
		meta = append(meta, age)
	}
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	if it.Now != "" {
		b.WriteString("**Now:** " + it.Now + "\n")
	}
	return b.String()
}

// DocFooter lists an item's children; empty when there are none.
func DocFooter(children []*model.Item) string {
	if len(children) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Children\n\n")
	for _, c := range children {
		fmt.Fprintf(&b, "- %s **%s** %s · _%s_\n", c.Status.Glyph(), c.ID, c.Title, c.Status)
	}
	return b.String()
}
