package tui

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/RL-Charles/sherpa/internal/render"
)

// editor is the preview pane. Outside edit mode it shows the whole item
// rendered; in edit mode the block under the cursor switches to raw,
// editable markdown while every other block stays rendered (Obsidian-style
// live preview).
type editor struct {
	header, footer string   // read-only markdown around the body
	tail           []string // pre-rendered lines after the footer (the worklog)
	lines          []string // the body, one entry per source line

	editing  bool
	dirty    bool
	row, col int // cursor: source line, rune index
	goal     int // preferred column for vertical moves, -1 when unset
	undo     []snapshot
	redo     []snapshot
	lastOp   string
	group    string // when set, every edit joins one undo step (a vim change or insert session)
	seq      int
	vim      vimState

	width, height int
	offset        int
	style         string
	cache         map[string][]string
	renderers     map[int]*glamour.TermRenderer
}

type snapshot struct {
	lines    []string
	row, col int
}

const maxUndo = 200

var (
	gutter            = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("▎ ")
	cursorStyle       = lipgloss.NewStyle().Reverse(true)
	insertCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("3")).Foreground(lipgloss.Color("0"))
)

func newEditor(style string) *editor {
	return &editor{style: style, goal: -1, cache: map[string][]string{}, renderers: map[int]*glamour.TermRenderer{}}
}

// setDoc replaces the content (outside edit mode). reset scrolls to the top.
func (e *editor) setDoc(header, body, footer string, reset bool) {
	e.header, e.footer = header, footer
	e.lines = strings.Split(strings.TrimRight(body, "\n"), "\n")
	if reset {
		e.offset, e.row, e.col = 0, 0, 0
	}
}

func (e *editor) body() string { return strings.TrimRight(strings.Join(e.lines, "\n"), "\n") + "\n" }

func (e *editor) resize(w, h int) {
	if w != e.width {
		e.cache = map[string][]string{}
	}
	e.width, e.height = w, h
}

func (e *editor) wrapWidth() int { return max(10, e.width-4) }

// startEditing enters edit mode with the cursor on the first source line visible.
func (e *editor) startEditing() {
	e.editing, e.dirty, e.undo, e.redo, e.lastOp, e.goal, e.group = true, false, nil, nil, "", -1, ""
	e.vim = vimState{}
	_, _, srcAt := e.layout()
	e.row = 0
	for i, src := range srcAt {
		if i >= e.offset && src >= 0 {
			e.row = src
			break
		}
	}
	e.col = 0
	e.clampNormal()
	e.follow()
}

// Rendering -----------------------------------------------------------------

// preWidth is the wrap width for code fences and diagrams: they are clipped
// at the pane edge rather than wrapped, which would break their structure.
const preWidth = 1000

func (e *editor) renderer(w int) *glamour.TermRenderer {
	if r, ok := e.renderers[w]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(glamour.WithStyles(render.Style(e.style)), glamour.WithWordWrap(w))
	if err != nil {
		return nil
	}
	e.renderers[w] = r
	return r
}

// renderMD renders markdown to display lines, trimming Glamour's blank
// padding so blocks can be stacked exactly.
func (e *editor) renderMD(md string) []string { return e.renderMDWidth(md, e.wrapWidth()) }

func (e *editor) renderMDWidth(md string, w int) []string {
	key := strconv.Itoa(w) + "\x00" + md
	if out, ok := e.cache[key]; ok {
		return out
	}
	var text string
	if r := e.renderer(w); r != nil {
		text, _ = r.Render(md)
	}
	if text == "" {
		text = md
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	e.cache[key] = lines
	return lines
}

// renderBlock renders one block. Indented list items are dedented before
// rendering and re-indented after so nesting survives rendering in isolation.
func (e *editor) renderBlock(b block) []string { return e.renderLines(e.lines[b.start:b.end], b.kind) }

// renderStatic renders read-only markdown block by block, like the body, so
// tables are boxed and diagrams stay preformatted.
func (e *editor) renderStatic(md string) []string {
	lines := strings.Split(strings.Trim(md, "\n"), "\n")
	var out []string
	blocks := splitBlocks(lines)
	bi := 0
	for i := 0; i < len(lines); {
		if bi < len(blocks) && blocks[bi].start == i {
			b := blocks[bi]
			bi++
			out = append(out, e.renderLines(lines[b.start:b.end], b.kind)...)
			i = b.end
			continue
		}
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		i++
	}
	return out
}

func (e *editor) renderLines(src []string, kind blockKind) []string {
	if kind == bParagraph && isDiagram(src) {
		// Text trees and arrow diagrams written without a code fence would be
		// reflowed into one paragraph; show them preformatted instead.
		return e.renderMDWidth("```text\n"+strings.Join(src, "\n")+"\n```", preWidth)
	}
	if kind == bFence {
		return e.renderMDWidth(strings.Join(src, "\n"), preWidth)
	}
	if kind == bTable {
		if out, ok := e.renderTableCached(src); ok {
			return out
		}
		// Not a well-formed table: let Glamour make what it can of it.
		return e.renderMDWidth(strings.Join(src, "\n"), e.width)
	}
	indent := 0
	if kind == bList {
		indent = len(src[0]) - len(strings.TrimLeft(src[0], " "))
	}
	if indent == 0 {
		return e.renderMD(strings.Join(src, "\n"))
	}
	ded := make([]string, len(src))
	for i, l := range src {
		n := min(indent, len(l)-len(strings.TrimLeft(l, " ")))
		ded[i] = l[n:]
	}
	out := e.renderMD(strings.Join(ded, "\n"))
	pad := strings.Repeat(" ", indent)
	res := make([]string, len(out))
	for i, l := range out {
		res[i] = pad + l
	}
	return res
}

// renderTableCached draws a boxed table indented by Glamour's 2-cell margin.
func (e *editor) renderTableCached(src []string) ([]string, bool) {
	key := "table\x00" + strconv.Itoa(e.width) + "\x00" + strings.Join(src, "\n")
	if out, ok := e.cache[key]; ok {
		return out, true
	}
	lines, ok := renderTable(src, max(10, e.width-2))
	if !ok {
		return nil, false
	}
	for i, l := range lines {
		lines[i] = "  " + l
	}
	e.cache[key] = lines
	return lines, true
}

// layout builds every display line. cursorLine is the display line holding
// the cursor (-1 outside edit mode); srcAt maps display line → source line
// (-1 for header/footer lines).
func (e *editor) layout() (out []string, cursorLine int, srcAt []int) {
	cursorLine = -1
	add := func(src int, ls ...string) {
		for _, l := range ls {
			out = append(out, l)
			srcAt = append(srcAt, src)
		}
	}
	if e.header != "" {
		add(-1, e.renderMD(e.header)...)
		add(-1, "")
	}
	blocks := splitBlocks(e.lines)
	bi := 0
	for i := 0; i < len(e.lines); {
		if bi < len(blocks) && blocks[bi].start == i {
			b := blocks[bi]
			bi++
			if e.editing && e.row >= b.start && e.row < b.end {
				for r := b.start; r < b.end; r++ {
					raw, cur := e.rawLine(r)
					if cur >= 0 {
						cursorLine = len(out) + cur
					}
					add(r, raw...)
				}
			} else {
				for k, l := range e.renderBlock(b) {
					add(min(b.start+k, b.end-1), l)
				}
			}
			i = b.end
			continue
		}
		if e.editing && e.row == i {
			raw, cur := e.rawLine(i)
			cursorLine = len(out) + cur
			add(i, raw...)
		} else {
			add(i, "")
		}
		i++
	}
	if e.footer != "" {
		add(-1, "")
		add(-1, e.renderStatic(e.footer)...)
	}
	if len(e.tail) > 0 {
		add(-1, "")
		add(-1, e.tail...)
	}
	return out, cursorLine, srcAt
}

// rawLine soft-wraps source line r for editing, drawing the cursor when it
// is on this line. cur is the wrapped row holding the cursor, or -1.
func (e *editor) rawLine(r int) (lines []string, cur int) {
	w := e.wrapWidth()
	runes := []rune(strings.ReplaceAll(e.lines[r], "\t", " "))
	chunks, cells := wrapRunes(runes, w)
	if r == e.row && e.col == len(runes) && cells >= w {
		chunks = append(chunks, chunk{len(runes), len(runes)}) // room for the end-of-line cursor
	}
	cur = -1
	for k, c := range chunks {
		text := string(runes[c.from:c.to])
		onCursor := r == e.row && e.editing && e.col >= c.from &&
			(e.col < c.to || (e.col == c.to && k == len(chunks)-1))
		if onCursor {
			cur = k
			at := e.col - c.from
			seg := runes[c.from:c.to]
			under := " "
			after := ""
			if at < len(seg) {
				under = string(seg[at])
				after = string(seg[at+1:])
			}
			cs := cursorStyle
			if e.vim.mode == vimInsert {
				cs = insertCursorStyle
			}
			text = string(seg[:at]) + cs.Render(under) + after
		}
		lines = append(lines, gutter+text)
	}
	return lines, cur
}

type chunk struct{ from, to int }

// wrapRunes splits runes into soft-wrapped rows at most w cells wide. cells
// is the width of the last row.
func wrapRunes(runes []rune, w int) (chunks []chunk, cells int) {
	start := 0
	for i, ch := range runes {
		cw := runewidth.RuneWidth(ch)
		if cells+cw > w && i > start {
			chunks = append(chunks, chunk{start, i})
			start, cells = i, 0
		}
		cells += cw
	}
	return append(chunks, chunk{start, len(runes)}), cells
}

// View renders the visible window.
func (e *editor) View() string {
	out, _, _ := e.layout()
	e.offset = max(0, min(e.offset, len(out)-e.height))
	end := min(len(out), e.offset+e.height)
	view := make([]string, 0, e.height)
	for _, l := range out[e.offset:end] {
		view = append(view, ansi.Truncate(l, e.width, ""))
	}
	return strings.Join(view, "\n")
}

// follow scrolls so the cursor stays visible, with a little context.
func (e *editor) follow() {
	out, cur, _ := e.layout()
	if cur < 0 || e.height <= 0 {
		return
	}
	margin := min(2, e.height/4)
	if cur < e.offset+margin {
		e.offset = cur - margin
	}
	if cur >= e.offset+e.height-margin {
		e.offset = cur - e.height + margin + 1
	}
	e.offset = max(0, min(e.offset, len(out)-e.height))
}

func (e *editor) scroll(delta int) {
	out, _, _ := e.layout()
	e.offset = max(0, min(e.offset+delta, len(out)-e.height))
}

func (e *editor) scrollTo(top bool) {
	if top {
		e.offset = 0
		return
	}
	out, _, _ := e.layout()
	e.offset = max(0, len(out)-e.height)
}

// Editing -------------------------------------------------------------------

func (e *editor) line() []rune { return []rune(e.lines[e.row]) }

func (e *editor) setLine(r []rune) { e.lines[e.row] = string(r) }

// snap records an undo point. Consecutive edits of the same kind share one,
// so a typed word undoes in one step; inside a group (a vim change or insert
// session) everything shares one.
func (e *editor) snap(op string) {
	e.dirty = true
	if e.group != "" {
		op = e.group
	}
	if op == e.lastOp && op != "newline" {
		return
	}
	e.lastOp = op
	e.undo = append(e.undo, e.state())
	if len(e.undo) > maxUndo {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *editor) state() snapshot {
	return snapshot{lines: append([]string(nil), e.lines...), row: e.row, col: e.col}
}

func (e *editor) restore(s snapshot) {
	e.lines, e.row, e.col, e.lastOp = s.lines, s.row, s.col, ""
	e.dirty = true
}

func (e *editor) undoLast() bool {
	if len(e.undo) == 0 {
		return false
	}
	e.redo = append(e.redo, e.state())
	e.restore(e.undo[len(e.undo)-1])
	e.undo = e.undo[:len(e.undo)-1]
	return true
}

func (e *editor) redoLast() bool {
	if len(e.redo) == 0 {
		return false
	}
	e.undo = append(e.undo, e.state())
	e.restore(e.redo[len(e.redo)-1])
	e.redo = e.redo[:len(e.redo)-1]
	return true
}

func (e *editor) insert(s string) {
	if s == "" {
		return
	}
	op := "insert"
	if s == " " {
		op = "space" // break undo groups at word boundaries
	}
	e.snap(op)
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	l := e.line()
	before, after := string(l[:e.col]), string(l[e.col:])
	if len(parts) == 1 {
		e.lines[e.row] = before + s + after
		e.col += len([]rune(s))
		return
	}
	newLines := make([]string, 0, len(parts))
	newLines = append(newLines, before+parts[0])
	newLines = append(newLines, parts[1:len(parts)-1]...)
	last := parts[len(parts)-1]
	newLines = append(newLines, last+after)
	e.lines = append(e.lines[:e.row], append(newLines, e.lines[e.row+1:]...)...)
	e.row += len(parts) - 1
	e.col = len([]rune(last))
}

var reListPrefix = regexp.MustCompile(`^(\s*)([-*+]|(\d{1,9})([.)]))(\s+)(\[[ xX]\]\s+)?`)

// continuation returns the list marker that should start the line after l
// ("- [ ] " after a checklist item, "3. " after "2. "), and the length of l's
// own marker. ok is false when l is not a list item.
func continuation(l string) (prefix string, markerLen int, ok bool) {
	m := reListPrefix.FindStringSubmatch(l)
	if m == nil {
		return "", 0, false
	}
	marker := m[2]
	if m[3] != "" {
		n, _ := strconv.Atoi(m[3])
		marker = strconv.Itoa(n+1) + m[4]
	}
	prefix = m[1] + marker + m[5]
	if m[6] != "" {
		prefix += "[ ] "
	}
	return prefix, len([]rune(m[0])), true
}

// newline splits the line, continuing lists and checklists like Obsidian:
// Enter on an empty item ends the list instead.
func (e *editor) newline() {
	e.snap("newline")
	l := e.line()
	prefix := ""
	if p, n, ok := continuation(string(l)); ok && e.col >= n {
		if strings.TrimSpace(string(l[n:])) == "" {
			e.setLine(nil) // empty item: leave the list
			e.col = 0
			return
		}
		prefix = p
	}
	before, after := string(l[:e.col]), string(l[e.col:])
	e.lines[e.row] = before
	e.lines = append(e.lines[:e.row+1], append([]string{prefix + after}, e.lines[e.row+1:]...)...)
	e.row++
	e.col = len([]rune(prefix))
}

func (e *editor) backspace() {
	if e.col > 0 {
		e.snap("delete")
		l := e.line()
		e.setLine(append(l[:e.col-1:e.col-1], l[e.col:]...))
		e.col--
		return
	}
	if e.row == 0 {
		return
	}
	e.snap("delete")
	prev := []rune(e.lines[e.row-1])
	e.lines[e.row-1] = string(prev) + e.lines[e.row]
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
	e.col = len(prev)
}

func (e *editor) deleteForward() {
	l := e.line()
	if e.col < len(l) {
		e.snap("delete")
		e.setLine(append(l[:e.col:e.col], l[e.col+1:]...))
		return
	}
	if e.row == len(e.lines)-1 {
		return
	}
	e.snap("delete")
	e.lines[e.row] += e.lines[e.row+1]
	e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
}

func (e *editor) killToEnd() {
	l := e.line()
	if e.col >= len(l) {
		e.deleteForward()
		return
	}
	e.snap("kill")
	e.setLine(l[:e.col])
}

// indent shifts a list item by two spaces, or inserts two spaces elsewhere.
func (e *editor) indent(out bool) {
	l := string(e.line())
	if !reList.MatchString(l) {
		if !out {
			e.insert("  ")
		}
		return
	}
	e.snap("indent")
	if out {
		n := min(2, len(l)-len(strings.TrimLeft(l, " ")))
		e.lines[e.row] = l[n:]
		e.col = max(0, e.col-n)
		return
	}
	e.lines[e.row] = "  " + l
	e.col += 2
}

var reCheckbox = regexp.MustCompile(`^(\s*(?:[-*+]|\d{1,9}[.)])\s+)\[([ xX])\]`)

// toggleCheckbox flips "- [ ]" ↔ "- [x]" on the cursor line.
func (e *editor) toggleCheckbox() {
	l := e.lines[e.row]
	m := reCheckbox.FindStringSubmatchIndex(l)
	if m == nil {
		return
	}
	e.snap("toggle")
	mark := "x"
	if l[m[4]:m[5]] != " " {
		mark = " "
	}
	e.lines[e.row] = l[:m[4]] + mark + l[m[5]:]
}

func (e *editor) moveVert(delta int) {
	if e.goal < 0 {
		e.goal = e.col
	}
	e.row = max(0, min(e.row+delta, len(e.lines)-1))
	e.col = min(e.goal, len(e.line()))
}

func (e *editor) moveWord(dir int) {
	l := e.line()
	isWord := func(i int) bool { return unicode.IsLetter(l[i]) || unicode.IsDigit(l[i]) }
	if dir < 0 {
		if e.col == 0 && e.row > 0 {
			e.row--
			e.col = len(e.line())
			return
		}
		i := e.col
		for i > 0 && !isWord(i-1) {
			i--
		}
		for i > 0 && isWord(i-1) {
			i--
		}
		e.col = i
		return
	}
	if e.col == len(l) && e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
		return
	}
	i := e.col
	for i < len(l) && !isWord(i) {
		i++
	}
	for i < len(l) && isWord(i) {
		i++
	}
	e.col = i
}

// key applies one editing keystroke. Mode keys (esc, ctrl+s, ctrl+r) are
// handled by the Model before this is called.
func (e *editor) key(msg tea.KeyMsg) {
	vertical := false
	switch msg.Type {
	case tea.KeyRunes:
		if msg.Alt {
			return
		}
		e.insert(string(msg.Runes))
	case tea.KeySpace:
		e.insert(" ")
	case tea.KeyEnter:
		e.newline()
	case tea.KeyBackspace, tea.KeyCtrlH:
		e.backspace()
	case tea.KeyDelete:
		e.deleteForward()
	case tea.KeyCtrlK:
		e.killToEnd()
	case tea.KeyTab:
		e.indent(false)
	case tea.KeyShiftTab:
		e.indent(true)
	case tea.KeyCtrlX:
		e.toggleCheckbox()
	case tea.KeyCtrlZ:
		e.undoLast()
	case tea.KeyLeft:
		e.lastOp = ""
		if e.col > 0 {
			e.col--
		} else if e.row > 0 {
			e.row--
			e.col = len(e.line())
		}
	case tea.KeyRight:
		e.lastOp = ""
		if e.col < len(e.line()) {
			e.col++
		} else if e.row < len(e.lines)-1 {
			e.row++
			e.col = 0
		}
	case tea.KeyCtrlLeft:
		e.lastOp = ""
		e.moveWord(-1)
	case tea.KeyCtrlRight:
		e.lastOp = ""
		e.moveWord(1)
	case tea.KeyUp:
		e.lastOp, vertical = "", true
		e.moveVert(-1)
	case tea.KeyDown:
		e.lastOp, vertical = "", true
		e.moveVert(1)
	case tea.KeyPgUp:
		e.lastOp, vertical = "", true
		e.moveVert(-max(1, e.height/2))
	case tea.KeyPgDown:
		e.lastOp, vertical = "", true
		e.moveVert(max(1, e.height/2))
	case tea.KeyHome, tea.KeyCtrlA:
		e.lastOp = ""
		e.col = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		e.lastOp = ""
		e.col = len(e.line())
	default:
		return
	}
	if !vertical {
		e.goal = -1
	}
	e.follow()
}
