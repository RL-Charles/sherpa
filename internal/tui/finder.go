package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/search"
)

// Finder ------------------------------------------------------------------------
//
// `/` turns the left pane into a fuzzy finder:
//
//	⌕ oauth lo▏                 3 of 59
//	◐ T-0030 Add OAuth login
//	○ T-0031 Rotate refresh tokens
//
// Typing filters as you go (see package search for the syntax), ↑/↓ or
// ctrl+p/ctrl+n move, the preview follows the highlighted item, enter jumps
// to it in the tree (expanding its parents) and esc goes back.

type finder struct {
	query   string
	results []search.Result
	idx     int
	offset  int
}

var (
	findPrompt = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "45"}).Bold(true)
	findHit    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "214"}).Bold(true)
)

func (m *Model) openFinder() {
	m.find = &finder{}
	m.focus = focusTree
	m.runFind()
	m.layout()
}

func (m *Model) closeFinder() {
	m.find = nil
	m.layout()
	m.refreshPreview()
}

func (m *Model) runFind() {
	m.find.results = search.Search(m.items, m.find.query, 0)
	m.find.idx, m.find.offset = 0, 0
	m.refreshPreview()
}

func (m *Model) found() *model.Item {
	if f := m.find; f != nil && f.idx < len(f.results) {
		return f.results[f.idx].Item
	}
	return nil
}

func (m *Model) findMove(d int) {
	f := m.find
	if len(f.results) == 0 {
		return
	}
	f.idx = max(0, min(f.idx+d, len(f.results)-1))
	h := m.treeHeight() - 1 // first line is the prompt
	if f.idx < f.offset {
		f.offset = f.idx
	}
	if f.idx >= f.offset+h {
		f.offset = f.idx - h + 1
	}
	m.refreshPreview()
}

func (m *Model) findKey(msg tea.KeyMsg) {
	f := m.find
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.closeFinder()
	case tea.KeyEnter:
		if it := m.found(); it != nil {
			m.find = nil
			m.layout()
			m.jumpTo(it)
		}
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyCtrlK, tea.KeyShiftTab:
		m.findMove(-1)
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyCtrlJ, tea.KeyTab:
		m.findMove(1)
	case tea.KeyPgUp:
		m.findMove(-m.treeHeight() / 2)
	case tea.KeyPgDown:
		m.findMove(m.treeHeight() / 2)
	case tea.KeyBackspace:
		if r := []rune(f.query); len(r) > 0 {
			f.query = string(r[:len(r)-1])
			m.runFind()
		}
	case tea.KeyCtrlU:
		f.query = ""
		m.runFind()
	case tea.KeySpace:
		f.query += " "
		m.runFind()
	case tea.KeyRunes:
		f.query += string(msg.Runes)
		m.runFind()
	}
}

// jumpTo selects it in the left pane, expanding whatever hides it.
func (m *Model) jumpTo(it *model.Item) {
	if m.board {
		m.expanded[groupKey(it.Status)] = true
	} else {
		for p := m.findItem(it.Parent); p != nil; p = m.findItem(p.Parent) {
			m.expanded[p.ID] = true
		}
	}
	m.focus = focusTree
	m.rebuild(it.ID)
}

func (m *Model) finderView() string {
	f := m.find
	h, w := m.treeHeight(), m.innerTreeW()
	count := fmt.Sprintf("%d of %d", len(f.results), len(m.items))
	prompt := findPrompt.Render("⌕ ") + f.query + "▏"
	gap := max(1, w-ansi.StringWidth(prompt)-len(count))
	lines := []string{ansi.Truncate(prompt+strings.Repeat(" ", gap)+faint.Render(count), w, "…")}
	if len(f.results) == 0 {
		lines = append(lines, faint.Render("No matches. Try fewer letters, is:<status> or @name."))
	}
	for i := f.offset; i < len(f.results) && len(lines) < h; i++ {
		r := f.results[i]
		it := r.Item
		if i == f.idx {
			plain := it.Status.Glyph() + " " + it.ID + " " + it.Title
			if r.Snippet != "" {
				plain += "  · " + r.Snippet
			}
			plain = ansi.Truncate(plain, w, "…")
			lines = append(lines, selectedFocused.Render(plain+strings.Repeat(" ", max(0, w-ansi.StringWidth(plain)))))
			continue
		}
		title := highlight(it.Title, r.Hits)
		if r.Snippet != "" {
			title += faint.Render("  · " + r.Snippet)
		}
		line := render.StatusStyle(it.Status).Render(it.Status.Glyph()) + " " + idStyle.Render(it.ID) + " " + title
		lines = append(lines, ansi.Truncate(line, w, "…"))
	}
	return m.panel(focusedBorder, m.treeWidth()).Render(strings.Join(lines, "\n"))
}

// highlight marks the matched runes of s.
func highlight(s string, hits []int) string {
	if len(hits) == 0 {
		return s
	}
	at := map[int]bool{}
	for _, h := range hits {
		at[h] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if at[i] {
			b.WriteString(findHit.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clickFinder handles a click while the finder is open: a result row jumps
// to it, anything else closes the finder.
func (m *Model) clickFinder(x, y int) {
	row := y - contentTop - 1 // first line is the prompt
	if x < m.treeWidth() && row >= 0 && m.find.offset+row < len(m.find.results) {
		it := m.find.results[m.find.offset+row].Item
		m.find = nil
		m.layout()
		m.jumpTo(it)
		return
	}
	m.closeFinder()
}

// finderHelp is the help bar while searching.
type finderHelp struct{}

func (finderHelp) ShortHelp() []key.Binding {
	return []key.Binding{
		hint("type", "filter"), hint("↑↓", "move"), hint("enter", "go to item"),
		hint("is:ready", "status/kind"), hint("@name", "assignee"), hint("esc", "back"),
	}
}

func (f finderHelp) FullHelp() [][]key.Binding { return [][]key.Binding{f.ShortHelp()} }
