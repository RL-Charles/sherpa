package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Mouse ---------------------------------------------------------------------
//
// Left-click mirrors the keyboard: in the tree it selects a row, and clicking
// a row's arrow (or the already-selected row) expands or collapses it. In the
// preview it starts editing with the cursor on the clicked word, moves the
// cursor while editing, and toggles worklog sessions from their headers.

// contentTop is the screen row of the first line inside either panel.
const contentTop = headerHeight + 1

func (m *Model) click(x, y int) {
	if !m.conflict {
		m.flash = ""
	}
	if m.pick != nil && m.clickPicker(x, y) {
		return
	}
	if x < m.treeWidth() {
		m.clickTree(x, y)
		return
	}
	m.clickPreview(x-m.treeWidth()-1-padX, y-contentTop)
}

func (m *Model) clickTree(x, y int) {
	if y < contentTop || y >= contentTop+m.treeHeight() {
		return
	}
	if m.ed.editing {
		if m.ed.dirty {
			m.flash = "⚠ save (ctrl+s) or discard (:q!) your changes to " + m.editID + " first"
			return
		}
		m.stopEdit()
	}
	i := m.offset + y - contentTop
	if i < 0 || i >= len(m.rows) {
		return
	}
	r := m.rows[i]
	col := x - 1 - padX
	onArrow := col >= 2*r.depth && col < 2*r.depth+2
	again := i == m.cursor && m.focus == focusTree
	m.focus, m.cursor = focusTree, i
	if r.hasKids && (onArrow || again) {
		m.expanded[r.key()] = !m.expanded[r.key()]
		m.rebuild(r.key())
	} else {
		m.clampOffset()
		m.refreshPreview()
	}
	m.layout()
}

// clickPreview handles a click at cell col of line screen inside the preview.
func (m *Model) clickPreview(col, screen int) {
	if screen < 0 || screen >= m.ed.height || m.selected() == nil {
		return
	}
	out, _, srcAt := m.ed.layout()
	d := m.ed.offset + screen
	if d >= len(out) {
		return
	}
	m.focus = focusPreview
	if srcAt[d] < 0 { // header, footer or worklog
		if !m.ed.editing && d >= len(out)-len(m.ed.tail) && isSessionHeader(out[d]) {
			m.expandLog = !m.expandLog
			m.refreshPreview()
		}
		m.layout()
		return
	}
	row, c := m.ed.sourcePos(out, srcAt, d, col)
	if !m.ed.editing {
		m.startEdit()
	}
	m.ed.placeCursor(row, c, screen)
}

func isSessionHeader(l string) bool {
	s := strings.TrimLeft(ansi.Strip(l), " ")
	return strings.HasPrefix(s, "▸ ") || strings.HasPrefix(s, "▾ ")
}

// sourcePos maps a click on display line d at cell col to a source position.
// Raw (editing) lines map exactly; rendered lines map by finding the clicked
// word in the block's source.
func (e *editor) sourcePos(out []string, srcAt []int, d, col int) (row, c int) {
	row = srcAt[d]
	if strings.HasPrefix(out[d], gutter) {
		k := 0
		for d-k-1 >= 0 && srcAt[d-k-1] == row && strings.HasPrefix(out[d-k-1], gutter) {
			k++
		}
		runes := []rune(strings.ReplaceAll(e.lines[row], "\t", " "))
		chunks, _ := wrapRunes(runes, e.wrapWidth())
		ch := chunks[min(k, len(chunks)-1)]
		return row, runeAtCell(runes[ch.from:ch.to], col-runewidth.StringWidth("▎ ")) + ch.from
	}
	shown := []rune(ansi.Strip(out[d]))
	at := runeAtCell(shown, col)
	word, wordAt := wordAround(shown, at)
	if word == "" {
		return row, 0
	}
	// Search the clicked line first, then the rest of its block (wrapped
	// paragraphs map several display lines onto one source line).
	from, to := row, row+1
	for _, b := range splitBlocks(e.lines) {
		if row >= b.start && row < b.end {
			from, to = b.start, b.end
		}
	}
	lines := []int{row}
	for r := from; r < to; r++ {
		if r != row {
			lines = append(lines, r)
		}
	}
	for _, r := range lines {
		if i := nearestIndex([]rune(e.lines[r]), []rune(word), wordAt); i >= 0 {
			return r, i + (at - wordAt)
		}
	}
	return row, 0
}

// runeAtCell returns the index of the rune covering cell col, clamped to
// len(runes) past the end.
func runeAtCell(runes []rune, col int) int {
	cells := 0
	for i, r := range runes {
		cells += runewidth.RuneWidth(r)
		if cells > col {
			return i
		}
	}
	return len(runes)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// wordAround returns the word containing index at and where it starts.
func wordAround(runes []rune, at int) (string, int) {
	if at >= len(runes) || !isWordRune(runes[at]) {
		return "", 0
	}
	s, t := at, at
	for s > 0 && isWordRune(runes[s-1]) {
		s--
	}
	for t < len(runes) && isWordRune(runes[t]) {
		t++
	}
	return string(runes[s:t]), s
}

// nearestIndex finds the occurrence of word in line closest to near, or -1.
func nearestIndex(line, word []rune, near int) int {
	best := -1
	for i := 0; i+len(word) <= len(line); i++ {
		if string(line[i:i+len(word)]) != string(word) {
			continue
		}
		if best < 0 || abs(i-near) < abs(best-near) {
			best = i
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// placeCursor moves the edit cursor to (row, col), keeping that line at
// screen row screen so the text does not jump under the mouse.
func (e *editor) placeCursor(row, col, screen int) {
	if e.vim.mode == vimCommand {
		e.vim.mode, e.vim.cmd = vimNormal, ""
	}
	e.vim.reset()
	e.row = max(0, min(row, len(e.lines)-1))
	e.col = max(0, min(col, len(e.line())))
	e.goal, e.lastOp = -1, ""
	if e.vim.mode == vimNormal {
		e.clampNormal()
	}
	out, cur, _ := e.layout()
	if cur >= 0 {
		e.offset = max(0, min(cur-screen, len(out)-e.height))
	}
	e.follow()
}
