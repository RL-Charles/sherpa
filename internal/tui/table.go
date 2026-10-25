package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Markdown tables are drawn with lipgloss instead of Glamour, which only
// draws inner separators. This gives a fully boxed grid:
//
//	╭────────┬───────╮
//	│ Header │ Head  │
//	├────────┼───────┤
//	│ cell   │ cell  │
//	├────────┼───────┤
//	│ cell   │ cell  │
//	╰────────┴───────╯
//
// Cells keep inline formatting (bold, italic, code, strikethrough) and wrap
// when the table is wider than the pane.

var (
	reTableSep = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)

	tableBorder = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "250", Dark: "240"})
	tableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"})
	cellCode    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"}).Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
	cellBold    = lipgloss.NewStyle().Bold(true)
	cellItalic  = lipgloss.NewStyle().Italic(true)
	cellStrike  = lipgloss.NewStyle().Strikethrough(true)

	reCode   = regexp.MustCompile("``\\s?(.+?)\\s?``|`([^`]+)`")
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reItalic = regexp.MustCompile(`(^|[^*\w])\*([^*]+)\*|(^|[^_\w])_([^_]+)_`)
	reStrike = regexp.MustCompile(`~~([^~]+)~~`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
)

// splitRow splits "| a | b \| c |" into cells, honouring escaped pipes and
// pipes inside code spans.
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			cur.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// inline renders the inline markdown a table cell may contain.
func inline(s string) string {
	// Protect code spans so emphasis markers inside them stay literal.
	var codes []string
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		sub := reCode.FindStringSubmatch(m)
		codes = append(codes, sub[1]+sub[2])
		return "\x00" + string(rune('A'+len(codes)-1)) + "\x00"
	})
	s = reLink.ReplaceAllString(s, "$1")
	s = reBold.ReplaceAllStringFunc(s, func(m string) string {
		return cellBold.Render(strings.Trim(m, "*_"))
	})
	s = reStrike.ReplaceAllStringFunc(s, func(m string) string {
		return cellStrike.Render(strings.Trim(m, "~"))
	})
	s = reItalic.ReplaceAllStringFunc(s, func(m string) string {
		sub := reItalic.FindStringSubmatch(m)
		lead, body := sub[1]+sub[3], sub[2]+sub[4]
		return lead + cellItalic.Render(body)
	})
	for i, c := range codes {
		s = strings.Replace(s, "\x00"+string(rune('A'+i))+"\x00", cellCode.Render(c), 1)
	}
	return s
}

// renderTable draws a markdown table at most width cells wide. ok is false
// when the block is not a well-formed table (no separator row), so the
// caller can fall back to Glamour.
func renderTable(src []string, width int) (lines []string, ok bool) {
	if len(src) < 2 || !reTableSep.MatchString(src[1]) {
		return nil, false
	}
	header := splitRow(src[0])
	cols := len(header)
	aligns := make([]lipgloss.Position, cols)
	for i, spec := range splitRow(src[1]) {
		if i >= cols {
			break
		}
		left, right := strings.HasPrefix(spec, ":"), strings.HasSuffix(spec, ":")
		switch {
		case left && right:
			aligns[i] = lipgloss.Center
		case right:
			aligns[i] = lipgloss.Right
		default:
			aligns[i] = lipgloss.Left
		}
	}
	for i := range header {
		header[i] = inline(header[i])
	}
	var rows [][]string
	for _, l := range src[2:] {
		cells := splitRow(l)
		row := make([]string, cols)
		for i := range row {
			if i < len(cells) {
				row[i] = inline(cells[i])
			}
		}
		rows = append(rows, row)
	}

	widths := fitColumns(header, rows, width)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorder).
		BorderRow(true).
		Headers(header...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			st := lipgloss.NewStyle().Padding(0, 1)
			if col < len(aligns) {
				st = st.Align(aligns[col])
			}
			if widths != nil && col < len(widths) {
				st = st.Width(widths[col] + 2) // + padding
			}
			if row == table.HeaderRow {
				st = st.Inherit(tableHeader)
			}
			return st
		})
	return strings.Split(t.Render(), "\n"), true
}

// fitColumns picks column content widths so the table fits in width. It
// shrinks the widest column one cell at a time, first never below a column's
// longest word, then (only if still too wide) below it, so short columns
// like "Key" never get their words broken to make room. It returns nil when
// the natural layout already fits.
func fitColumns(header []string, rows [][]string, width int) []int {
	cols := len(header)
	widths := make([]int, cols)
	minw := make([]int, cols)
	measure := func(cells []string) {
		for i, c := range cells {
			widths[i] = max(widths[i], lipgloss.Width(c))
			for _, w := range strings.Fields(c) {
				minw[i] = max(minw[i], lipgloss.Width(w))
			}
		}
	}
	measure(header)
	for _, r := range rows {
		measure(r)
	}
	avail := width - (cols + 1) - 2*cols // borders and cell padding
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= avail {
		return nil
	}
	shrink := func(floor func(i int) int) {
		for total > avail {
			best := -1
			for i := range widths {
				if widths[i] > floor(i) && (best < 0 || widths[i] > widths[best]) {
					best = i
				}
			}
			if best < 0 {
				return
			}
			widths[best]--
			total--
		}
	}
	shrink(func(i int) int { return minw[i] }) // keep words whole
	shrink(func(int) int { return 3 })         // break the longest words
	return widths
}
