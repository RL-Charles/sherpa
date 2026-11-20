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
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"

	"github.com/RL-Charles/sherpa/internal/palette"
)

func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

var (
	dim   = lipgloss.NewStyle().Foreground(palette.Muted)
	bold  = lipgloss.NewStyle().Bold(true)
	idCol = lipgloss.NewStyle().Foreground(palette.ID)
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
	model.Backlog:     palette.Subtle,
	model.Ready:       palette.Secondary,
	model.InProgress:  palette.Yellow,
	model.AgentReview: palette.Purple,
	model.Review:      palette.Orange,
	model.Blocked:     palette.Red,
	model.Done:        palette.Green,
}

// StatusStyle colours text by status.
func StatusStyle(s model.Status) lipgloss.Style {
	c, ok := statusColor[s]
	if !ok {
		c = palette.Subtle
	}
	return lipgloss.NewStyle().Foreground(c)
}

var (
	staleWarnStyle = lipgloss.NewStyle().Foreground(palette.Yellow)
	staleCritStyle = lipgloss.NewStyle().Foreground(palette.Red).Bold(true)
	nowLabelStyle  = lipgloss.NewStyle().Foreground(palette.Primary).Bold(true)
	nextLabelStyle = lipgloss.NewStyle().Foreground(palette.Green).Bold(true)
	blkLabelStyle  = lipgloss.NewStyle().Foreground(palette.Red).Bold(true)
	chipStyle      = lipgloss.NewStyle().Foreground(palette.Muted)
)

// StaleAgeStyle highlights aging in-progress and review work so stale tickets
// stand out in long-horizon repositories.
func StaleAgeStyle(s model.Status, updated time.Time) lipgloss.Style {
	if updated.IsZero() {
		return dim
	}
	age := time.Since(updated)
	switch s {
	case model.InProgress:
		if age >= 7*24*time.Hour {
			return staleCritStyle
		}
		if age >= 3*24*time.Hour {
			return staleWarnStyle
		}
	case model.Review, model.AgentReview:
		if age >= 7*24*time.Hour {
			return staleCritStyle
		}
		if age >= 3*24*time.Hour {
			return staleWarnStyle
		}
	case model.Blocked:
		return staleCritStyle
	}
	return dim
}

// IsStale reports whether an active ticket has been untouched past its aging threshold.
func IsStale(s model.Status, updated time.Time) bool {
	if updated.IsZero() {
		return false
	}
	age := time.Since(updated)
	return (s == model.InProgress || s == model.Review || s == model.AgentReview) && age >= 7*24*time.Hour
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
	// Glamour's standard layout, recoloured with Nord (internal/palette).
	pick := func(c lipgloss.AdaptiveColor) string {
		if name == styles.LightStyle {
			return c.Light
		}
		return c.Dark
	}
	cfg := styles.DarkStyleConfig
	if name == styles.LightStyle {
		cfg = styles.LightStyleConfig
	}
	str := func(v string) *string { return &v }
	h2, h3, minor := pick(palette.Primary), pick(palette.Secondary), pick(palette.Muted)
	heading := func(color string, underline bool) gansi.StyleBlock {
		return gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: &color, Bold: &yes, Underline: &underline}}
	}
	cfg.Document.StylePrimitive.Color = str(pick(palette.Text))
	cfg.Heading.StylePrimitive.Color = str(pick(palette.Primary))
	cfg.H1.StylePrimitive.Color = str(pick(palette.OnAccent))
	cfg.H1.StylePrimitive.BackgroundColor = str(pick(palette.Primary))
	cfg.H2 = heading(h2, true)
	cfg.H2.BlockPrefix = "\n" + strings.Repeat("─", 28) + "\n"
	cfg.H3 = heading(h3, false)
	cfg.H4 = heading(minor, false)
	cfg.H5 = heading(minor, false)
	cfg.H6 = heading(minor, false)
	cfg.BlockQuote.StylePrimitive.Color = str(pick(palette.Muted))
	cfg.HorizontalRule.Color = str(pick(palette.Subtle))
	cfg.Link.Color = str(pick(palette.Secondary))
	cfg.LinkText.Color = str(pick(palette.Primary))
	cfg.Strong.Color = str(pick(palette.Bright))
	cfg.Code.StylePrimitive.Color = str(pick(palette.Accent))
	cfg.Code.StylePrimitive.BackgroundColor = str(pick(palette.SelectedDimBg))
	cfg.CodeBlock.Theme = "nord"
	cfg.CodeBlock.Chroma = nil // use the theme above, not Glamour's own token colours
	cfg.Table.StylePrimitive.Color = str(pick(palette.Text))
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

// CompactPreviewHeader renders a tight 3–4 line lipgloss pinned strip above the
// TUI scroll region: ID + primary title, full Epic › Story + status/aging/PR/deps
// metadata, a capped Now line, and Handoff Next/Blocker when present.
func CompactPreviewHeader(it *model.Item, all []*model.Item, width int, briefMode bool) []string {
	if width < 20 {
		width = 20
	}
	byID := make(map[string]*model.Item, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	modeTag := "[full · B brief]"
	if briefMode {
		modeTag = "[brief · B full]"
	}
	tag := chipStyle.Render(modeTag)
	tagW := ansi.StringWidth(tag)
	titleAvail := max(12, width-tagW-2)
	titleLeft := idCol.Render(it.ID) + dim.Render(" · ") + bold.Render(it.Title)
	if ansi.StringWidth(titleLeft) > titleAvail {
		titleLeft = ansi.Truncate(titleLeft, titleAvail, "…")
	}
	gap := max(1, width-ansi.StringWidth(titleLeft)-tagW)
	line1 := titleLeft + strings.Repeat(" ", gap) + tag

	// Line 2: semantic status + staleness + hierarchy breadcrumb + counts + PR + deps.
	stLabel := it.Status.Glyph() + " " + statusWord(it.Status)
	meta := []string{StatusStyle(it.Status).Bold(true).Render(stLabel)}
	if age := Age(it.Updated); age != "" {
		ageStr := age
		if IsStale(it.Status, it.Updated) {
			ageStr += " stale"
		}
		meta = append(meta, StaleAgeStyle(it.Status, it.Updated).Render(ageStr))
	}
	if it.Assignee != "" {
		meta = append(meta, dim.Render("@"+it.Assignee))
	}
	if crumb := breadcrumbIDs(it, byID); crumb != "" {
		meta = append(meta, dim.Render(crumb))
	}
	if d, t := it.DeliverableCounts(); t > 0 {
		meta = append(meta, dim.Render(fmt.Sprintf("%d/%d deliv", d, t)))
	}
	if r, t := it.CheckCounts(); t > 0 {
		mark := ""
		if r == t {
			mark = " ✓"
		}
		meta = append(meta, dim.Render(fmt.Sprintf("%d/%d checks%s", r, t, mark)))
	}
	if l := it.Links; l != nil && l.PRNumber > 0 {
		pr := fmt.Sprintf("⇡ #%d", l.PRNumber)
		if l.PRState != "" {
			pr += " " + l.PRState
		}
		meta = append(meta, dim.Render(pr))
	}
	if len(it.Depends) > 0 {
		var deps []string
		for i, d := range it.Depends {
			if i == 3 {
				deps = append(deps, fmt.Sprintf("+%d", len(it.Depends)-3))
				break
			}
			g := "?"
			if x := byID[d]; x != nil {
				g = x.Status.Glyph()
			}
			deps = append(deps, d+g)
		}
		meta = append(meta, dim.Render("needs "+strings.Join(deps, ", ")))
	}
	line2 := ansi.Truncate(strings.Join(meta, dim.Render(" · ")), width, "…")

	out := []string{line1, line2}
	if it.Now != "" {
		nowLines := wrapCapped(nowLabelStyle.Render("Now")+"  ", strings.Join(strings.Fields(it.Now), " "), width, 2)
		out = append(out, nowLines...)
	}
	if h := it.Handoff; !h.Empty() {
		if h.Blocker != "" {
			out = append(out, ansi.Truncate(blkLabelStyle.Render("Blk")+"  ⛔ "+strings.Join(strings.Fields(h.Blocker), " "), width, "…"))
		}
		if h.Next != "" {
			out = append(out, ansi.Truncate(nextLabelStyle.Render("Next")+" "+strings.Join(strings.Fields(h.Next), " "), width, "…"))
		}
	}
	return out
}

func breadcrumbIDs(it *model.Item, byID map[string]*model.Item) string {
	var parts []string
	seen := map[string]bool{it.ID: true}
	for pid := it.Parent; pid != "" && !seen[pid]; {
		seen[pid] = true
		parts = append([]string{pid}, parts...)
		if p := byID[pid]; p != nil {
			pid = p.Parent
		} else {
			break
		}
	}
	return strings.Join(parts, " › ")
}

func wrapCapped(prefix, text string, width, maxLines int) []string {
	pw := ansi.StringWidth(prefix)
	avail := max(16, width-pw)
	wrapped := strings.Split(ansi.Wordwrap(text, avail, ""), "\n")
	if len(wrapped) > maxLines {
		wrapped = wrapped[:maxLines]
		last := strings.TrimRight(wrapped[maxLines-1], " .,:;")
		if ansi.StringWidth(last) >= avail {
			last = ansi.Truncate(last, max(4, avail-1), "")
		}
		wrapped[maxLines-1] = strings.TrimRight(last, " ") + "…"
	}
	indent := strings.Repeat(" ", pw)
	out := make([]string, len(wrapped))
	for i, l := range wrapped {
		if i == 0 {
			out[i] = prefix + l
		} else {
			out[i] = indent + l
		}
	}
	return out
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
