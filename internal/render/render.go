// Package render draws items for the terminal: status-coloured trees and
// Glamour-rendered markdown.
package render

import (
	"fmt"
	"io"
	"os"
	"strings"

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
	dim   = lipgloss.NewStyle().Faint(true)
	bold  = lipgloss.NewStyle().Bold(true)
	idCol = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// StatusStyle colours text by status.
func StatusStyle(s model.Status) lipgloss.Style {
	color := map[model.Status]string{
		model.Backlog:    "8",
		model.Ready:      "4",
		model.InProgress: "3",
		model.Review:     "5",
		model.Blocked:    "1",
		model.Done:       "2",
	}[s]
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// Line is a one-line summary: glyph, ID, title, status, assignee.
func Line(it *model.Item) string {
	st := StatusStyle(it.Status)
	title := it.Title
	if it.Kind == model.Epic {
		title = bold.Render(title)
	}
	s := fmt.Sprintf("%s %s %s %s", st.Render(it.Status.Glyph()), idCol.Render(it.ID), title, st.Render(string(it.Status)))
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
	width := 100
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		width = min(w-2, 120)
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
	cfg.H3 = heading(h3, false)
	cfg.H4 = heading(minor, false)
	cfg.H5 = heading(minor, false)
	cfg.H6 = heading(minor, false)
	return cfg
}

var yes = true

// Document turns an item into a markdown page: header, body, children.
func Document(it *model.Item, parent *model.Item, children []*model.Item) string {
	return DocHeader(it, parent) + it.Body + DocFooter(children)
}

// DocHeader is the read-only title and metadata shown above an item's body.
func DocHeader(it *model.Item, parent *model.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n\n", it.ID, it.Title)
	meta := []string{"`" + string(it.Kind) + "`", it.Status.Glyph() + " **" + string(it.Status) + "**"}
	if it.Priority != "" {
		meta = append(meta, "**"+it.Priority+"**")
	}
	if it.Assignee != "" {
		meta = append(meta, "@"+it.Assignee)
	}
	for _, l := range it.Labels {
		meta = append(meta, "#"+l)
	}
	meta = append(meta, "_updated "+it.Updated.Local().Format("2006-01-02 15:04")+"_")
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	if parent != nil {
		fmt.Fprintf(&b, "> in **%s** · %s\n\n", parent.ID, parent.Title)
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
