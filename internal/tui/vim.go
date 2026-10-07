package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Vim-style modal editing for the live-preview editor.
//
//	normal ──i a o O I A s S c──▶ insert ──esc──▶ normal
//	normal ──:──▶ command ──enter/esc──▶ normal
//
// Insert mode reuses the editor's plain editing keys, so list continuation,
// tab indent and checkbox toggling work the same in both.

type vimMode int

const (
	vimNormal vimMode = iota
	vimInsert
	vimCommand
)

type vimState struct {
	mode     vimMode
	count    int    // count typed so far (0 = none)
	op       string // pending operator: d c y > <
	opCount  int
	prefix   string // pending multi-key command: g f F t T r Z, or i/a text object
	cmd      string // ':' command line
	reg      string // unnamed register
	regLine  bool   // register holds whole lines
	lastFind string // last f/F/t/T + char, for ; and ,
	msg      string // warning for the status bar
}

// editAction tells the Model what a key asked for beyond editing text.
type editAction int

const (
	actNone editAction = iota
	actSave
	actForceSave
	actSaveExit
	actForceSaveExit
	actExit
	actDiscard
)

type pos struct{ row, col int }

type motionKind int

const (
	exclusive motionKind = iota
	inclusive
	linewise
)

// pending is what the status bar shows while a command is being typed.
func (v *vimState) pending() string {
	s := ""
	if v.opCount > 0 {
		s += strconv.Itoa(v.opCount)
	}
	s += v.op
	if v.count > 0 {
		s += strconv.Itoa(v.count)
	}
	return s + v.prefix
}

func (v *vimState) reset() { v.count, v.op, v.opCount, v.prefix = 0, "", 0, "" }

// vimKey handles one key in edit mode.
func (e *editor) vimKey(msg tea.KeyMsg) editAction {
	v := &e.vim
	v.msg = ""
	switch v.mode {
	case vimInsert:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			e.leaveInsert()
		case tea.KeyCtrlW:
			e.deleteWordBack()
		default:
			e.key(msg)
		}
		return actNone
	case vimCommand:
		return e.commandKey(msg)
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		// Coalesced input ("dd", "ihello"): replay rune by rune, and once a key
		// switches to insert or command mode, the rest is text.
		act := actNone
		for i, r := range msg.Runes {
			switch v.mode {
			case vimInsert:
				e.insert(string(msg.Runes[i:]))
				e.follow()
				return act
			case vimCommand:
				v.cmd += string(msg.Runes[i:])
				return act
			}
			if a := e.normalKey(string(r)); a != actNone {
				act = a
			}
		}
		return act
	}
	k := msg.String()
	if msg.Type == tea.KeySpace {
		k = " "
	}
	act := e.normalKey(k)
	e.follow()
	return act
}

func (e *editor) leaveInsert() {
	e.vim.mode = vimNormal
	e.group = ""
	if e.col > 0 {
		e.col--
	}
	e.goal = -1
	e.clampNormal()
}

// beginChange starts one undoable normal-mode change.
func (e *editor) beginChange() {
	e.seq++
	e.group = fmt.Sprintf("change-%d", e.seq)
	e.snap("")
}

func (e *editor) endChange() {
	if e.vim.mode != vimInsert {
		e.group = ""
	}
}

// enterInsert switches to insert mode; edits until esc form one undo step.
func (e *editor) enterInsert() {
	if e.group == "" {
		e.seq++
		e.group = fmt.Sprintf("insert-%d", e.seq)
	}
	e.vim.mode = vimInsert
	e.goal = -1
}

// clampNormal keeps the cursor on a character, as normal mode requires.
func (e *editor) clampNormal() {
	if n := len(e.line()); e.col >= n {
		e.col = max(0, n-1)
	}
}

var normalAliases = map[string]string{
	"left": "h", "right": "l", "up": "k", "down": "j", "backspace": "h", " ": "l",
	"home": "0", "end": "$", "enter": "+", "pgdown": "ctrl+d", "pgup": "ctrl+u",
}

func (e *editor) normalKey(k string) editAction {
	v := &e.vim
	if a, ok := normalAliases[k]; ok && v.prefix == "" {
		k = a
	}
	if k == "esc" || k == "ctrl+c" {
		v.reset()
		if k == "ctrl+c" {
			v.msg = "type :wq to save and close, :q! to discard"
		}
		return actNone
	}
	if v.prefix != "" {
		return e.prefixKey(k)
	}
	if len(k) == 1 && (k[0] >= '1' && k[0] <= '9' || k == "0" && v.count > 0) {
		v.count = v.count*10 + int(k[0]-'0')
		return actNone
	}
	if v.op != "" {
		e.operatorKey(k)
		return actNone
	}
	n := max(1, v.count)
	hasCount := v.count > 0
	switch k {
	case "d", "c", "y", ">", "<":
		v.op, v.opCount, v.count = k, v.count, 0
		return actNone
	case "g", "f", "F", "t", "T", "r", "Z":
		v.prefix = k
		return actNone
	case ":":
		v.mode, v.cmd = vimCommand, ""
	case "i":
		e.enterInsert()
	case "a":
		e.col = min(e.col+1, len(e.line()))
		e.enterInsert()
	case "I":
		e.col = firstNonBlank(e.lines[e.row])
		e.enterInsert()
	case "A":
		e.col = len(e.line())
		e.enterInsert()
	case "o":
		e.beginChange()
		e.openLine(true)
	case "O":
		e.beginChange()
		e.openLine(false)
	case "x":
		e.charOp("d", e.col, e.col+n)
	case "X":
		e.charOp("d", e.col-n, e.col)
	case "s":
		e.charOp("c", e.col, e.col+n)
	case "S":
		e.lineOp("c", e.row, e.row+n-1)
	case "C":
		e.charOp("c", e.col, len(e.line()))
	case "D":
		e.charOp("d", e.col, len(e.line()))
	case "Y":
		e.lineOp("y", e.row, e.row+n-1)
	case "p", "P":
		e.paste(k == "p", n)
	case "J":
		e.join(max(1, n-1))
	case "~":
		e.toggleCase(n)
	case "u":
		for i := 0; i < n && e.undoLast(); i++ {
		}
		e.clampNormal()
	case "ctrl+r":
		for i := 0; i < n && e.redoLast(); i++ {
		}
		e.clampNormal()
	case "ctrl+x":
		e.beginChange()
		e.toggleCheckbox()
		e.endChange()
	default:
		if p, kind, ok := e.motion(k, n, hasCount); ok {
			e.moveTo(p, kind, k)
		}
	}
	v.count = 0
	return actNone
}

func (e *editor) prefixKey(k string) editAction {
	v := &e.vim
	prefix := v.prefix
	v.prefix = ""
	n := max(1, v.count)
	switch prefix {
	case "Z":
		v.reset()
		switch k {
		case "Z":
			return actSaveExit
		case "Q":
			return actDiscard
		}
		return actNone
	case "r":
		v.reset()
		l := e.line()
		if len([]rune(k)) == 1 && e.col+n <= len(l) {
			e.beginChange()
			for i := 0; i < n; i++ {
				l[e.col+i] = []rune(k)[0]
			}
			e.setLine(l)
			e.col += n - 1
			e.endChange()
		}
		return actNone
	case "i", "a":
		if v.op != "" {
			if from, to, ok := e.textObject(prefix == "a", k); ok {
				e.applyOp(v.op, from, to, exclusive)
			}
		}
		v.reset()
		return actNone
	}
	motion := prefix + k // gg, fx, Tx…
	if v.op != "" {
		e.operatorKey(motion)
		return actNone
	}
	if p, kind, ok := e.motion(motion, n, v.count > 0); ok {
		e.moveTo(p, kind, motion)
	}
	v.count = 0
	return actNone
}

// operatorKey completes "d", "c", "y", ">", "<" with a motion or by doubling.
func (e *editor) operatorKey(k string) {
	v := &e.vim
	op := v.op
	n := max(1, v.opCount) * max(1, v.count)
	hasCount := v.opCount > 0 || v.count > 0
	switch {
	case k == op:
		e.lineOp(op, e.row, e.row+n-1)
		v.reset()
		return
	case k == "i" || k == "a" || k == "g" || len(k) == 1 && strings.Contains("fFtT", k):
		v.prefix = k
		return
	}
	cur := e.line()
	if op == "c" && (k == "w" || k == "W") && e.col < len(cur) && !unicode.IsSpace(cur[e.col]) {
		k = map[string]string{"w": "e", "W": "E"}[k] // vim: cw acts like ce
	}
	p, kind, ok := e.motion(k, n, hasCount)
	if !ok {
		v.reset()
		return
	}
	if (k == "w" || k == "W") && p.row > e.row {
		p = pos{e.row, len(cur)} // dw on the last word stops at end of line
	}
	e.applyOp(op, pos{e.row, e.col}, p, kind)
	v.reset()
}

func (e *editor) moveTo(p pos, kind motionKind, k string) {
	vertical := k == "j" || k == "k" || k == "ctrl+d" || k == "ctrl+u"
	if vertical {
		if e.goal < 0 {
			e.goal = e.col
		}
		e.row = p.row
		e.col = min(e.goal, len(e.line()))
	} else {
		e.row, e.col = p.row, p.col
		e.goal = -1
	}
	e.clampNormal()
}

// Motions ---------------------------------------------------------------------

func firstNonBlank(l string) int {
	r := []rune(l)
	i := 0
	for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
		i++
	}
	return i
}

// motion computes where motion k would move the cursor, without moving it.
func (e *editor) motion(k string, n int, hasCount bool) (pos, motionKind, bool) {
	row, col := e.row, e.col
	last := len(e.lines) - 1
	lineLen := len(e.line())
	switch k {
	case "h":
		return pos{row, max(0, col-n)}, exclusive, col > 0
	case "l":
		return pos{row, min(lineLen, col+n)}, exclusive, col < lineLen
	case "j", "+":
		r := min(last, row+n)
		return pos{r, firstNonBlank(e.lines[r])}, linewise, r != row
	case "k", "-":
		r := max(0, row-n)
		return pos{r, firstNonBlank(e.lines[r])}, linewise, r != row
	case "ctrl+d":
		r := min(last, row+max(1, e.height/2))
		return pos{r, col}, linewise, true
	case "ctrl+u":
		r := max(0, row-max(1, e.height/2))
		return pos{r, col}, linewise, true
	case "0":
		return pos{row, 0}, exclusive, true
	case "^":
		return pos{row, firstNonBlank(e.lines[row])}, exclusive, true
	case "$":
		r := min(last, row+n-1)
		return pos{r, len([]rune(e.lines[r]))}, exclusive, true
	case "gg", "G":
		r := last
		if k == "gg" {
			r = 0
		}
		if hasCount {
			r = min(last, n-1)
		}
		return pos{r, firstNonBlank(e.lines[r])}, linewise, true
	case "w", "W", "e", "E", "b", "B":
		t := e.text()
		o := e.off(pos{row, col})
		big := k == "W" || k == "E" || k == "B"
		for i := 0; i < n; i++ {
			switch strings.ToLower(k) {
			case "w":
				o = wordForward(t, o, big)
			case "e":
				o = wordEnd(t, o, big)
			case "b":
				o = wordBack(t, o, big)
			}
		}
		kind := exclusive
		if k == "e" || k == "E" {
			kind = inclusive
		}
		return e.posAt(o), kind, true
	case "}", "{":
		r := row
		for i := 0; i < n; i++ {
			step := 1
			if k == "{" {
				step = -1
			}
			r += step
			for r > 0 && r < last && !isBlank(e.lines[r]) {
				r += step
			}
			r = max(0, min(last, r))
		}
		c := 0
		if r == last && k == "}" {
			c = len([]rune(e.lines[r]))
		}
		return pos{r, c}, exclusive, true
	case ";", ",":
		if e.vim.lastFind == "" {
			return pos{}, exclusive, false
		}
		f := e.vim.lastFind
		if k == "," {
			f = map[byte]string{'f': "F", 'F': "f", 't': "T", 'T': "t"}[f[0]] + f[1:]
		}
		return e.find(f, n)
	}
	if len(k) >= 2 && strings.Contains("fFtT", k[:1]) {
		e.vim.lastFind = k
		return e.find(k, n)
	}
	return pos{}, exclusive, false
}

// find handles f/F/t/T{char} on the current line.
func (e *editor) find(k string, n int) (pos, motionKind, bool) {
	target := []rune(k[1:])
	if len(target) != 1 {
		return pos{}, exclusive, false
	}
	l := e.line()
	c := e.col
	for i := 0; i < n; i++ {
		next := -1
		switch k[0] {
		case 'f', 't':
			for j := c + 1; j < len(l); j++ {
				if l[j] == target[0] {
					next = j
					break
				}
			}
		case 'F', 'T':
			for j := c - 1; j >= 0; j-- {
				if l[j] == target[0] {
					next = j
					break
				}
			}
		}
		if next < 0 {
			return pos{}, exclusive, false
		}
		c = next
	}
	switch k[0] {
	case 't':
		return pos{e.row, c - 1}, inclusive, true
	case 'T':
		return pos{e.row, c + 1}, exclusive, true
	case 'f':
		return pos{e.row, c}, inclusive, true
	}
	return pos{e.row, c}, exclusive, true
}

func wordClass(r rune, big bool) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case big || unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	}
	return 2
}

func wordForward(t []rune, o int, big bool) int {
	if o >= len(t) {
		return len(t)
	}
	if c := wordClass(t[o], big); c != 0 {
		for o < len(t) && wordClass(t[o], big) == c {
			o++
		}
	}
	for o < len(t) && wordClass(t[o], big) == 0 {
		if t[o] == '\n' && o+1 < len(t) && t[o+1] == '\n' {
			return o + 1 // an empty line is a word
		}
		o++
	}
	return o
}

func wordEnd(t []rune, o int, big bool) int {
	o++
	for o < len(t) && wordClass(t[o], big) == 0 {
		o++
	}
	if o >= len(t) {
		return max(0, len(t)-1)
	}
	c := wordClass(t[o], big)
	for o+1 < len(t) && wordClass(t[o+1], big) == c {
		o++
	}
	return o
}

func wordBack(t []rune, o int, big bool) int {
	if o <= 0 {
		return 0
	}
	o--
	for o > 0 && wordClass(t[o], big) == 0 {
		o--
	}
	c := wordClass(t[o], big)
	for o > 0 && wordClass(t[o-1], big) == c {
		o--
	}
	return o
}

// textObject returns the range of iw/aw/i"/a(… on the current line.
func (e *editor) textObject(around bool, k string) (pos, pos, bool) {
	l := e.line()
	if len(l) == 0 {
		return pos{}, pos{}, false
	}
	c := min(e.col, len(l)-1)
	switch k {
	case "w", "W":
		big := k == "W"
		cls := wordClass(l[c], big)
		s, t := c, c+1
		for s > 0 && wordClass(l[s-1], big) == cls {
			s--
		}
		for t < len(l) && wordClass(l[t], big) == cls {
			t++
		}
		if around {
			for t < len(l) && unicode.IsSpace(l[t]) {
				t++
			}
		}
		return pos{e.row, s}, pos{e.row, t}, true
	}
	pairs := map[string][2]rune{
		`"`: {'"', '"'}, "'": {'\'', '\''}, "`": {'`', '`'},
		"(": {'(', ')'}, ")": {'(', ')'}, "b": {'(', ')'},
		"[": {'[', ']'}, "]": {'[', ']'}, "{": {'{', '}'}, "}": {'{', '}'}, "B": {'{', '}'},
	}
	pr, ok := pairs[k]
	if !ok {
		return pos{}, pos{}, false
	}
	open, close := -1, -1
	depth := 0
	for i := c; i >= 0; i-- {
		if l[i] == pr[1] && i != c && pr[0] != pr[1] {
			depth++
		} else if l[i] == pr[0] {
			if depth == 0 {
				open = i
				break
			}
			depth--
		}
	}
	if open < 0 {
		return pos{}, pos{}, false
	}
	depth = 0
	for i := open + 1; i < len(l); i++ {
		if l[i] == pr[0] && pr[0] != pr[1] {
			depth++
		} else if l[i] == pr[1] {
			if depth == 0 {
				close = i
				break
			}
			depth--
		}
	}
	if close < 0 {
		return pos{}, pos{}, false
	}
	if around {
		return pos{e.row, open}, pos{e.row, close + 1}, true
	}
	return pos{e.row, open + 1}, pos{e.row, close}, true
}

// Buffer as one rune slice ------------------------------------------------------

func (e *editor) text() []rune { return []rune(strings.Join(e.lines, "\n")) }

func (e *editor) off(p pos) int {
	o := 0
	for r := 0; r < p.row; r++ {
		o += len([]rune(e.lines[r])) + 1
	}
	return o + p.col
}

func (e *editor) posAt(o int) pos {
	for r, l := range e.lines {
		n := len([]rune(l))
		if o <= n {
			return pos{r, o}
		}
		o -= n + 1
	}
	last := len(e.lines) - 1
	return pos{last, len([]rune(e.lines[last]))}
}

func (e *editor) setText(t []rune) {
	e.lines = strings.Split(string(t), "\n")
}

// Operators -------------------------------------------------------------------

func (e *editor) applyOp(op string, from, to pos, kind motionKind) {
	if kind == linewise || op == ">" || op == "<" {
		e.lineOp(op, min(from.row, to.row), max(from.row, to.row))
		return
	}
	a, b := e.off(from), e.off(to)
	if a > b {
		a, b = b, a
	}
	if kind == inclusive {
		b++
	}
	t := e.text()
	b = min(b, len(t))
	e.vim.reg, e.vim.regLine = string(t[a:b]), false
	switch op {
	case "y":
		p := e.posAt(a)
		e.row, e.col = p.row, p.col
	case "d", "c":
		e.beginChange()
		e.setText(append(t[:a:a], t[b:]...))
		p := e.posAt(a)
		e.row, e.col = p.row, p.col
		if op == "c" {
			e.enterInsert()
			return
		}
		e.endChange()
	}
	e.clampNormal()
}

// charOp applies op to columns [from, to) of the current line.
func (e *editor) charOp(op string, from, to int) {
	n := len(e.line())
	from, to = max(0, from), min(n, to)
	if from >= to && op != "c" {
		return
	}
	e.applyOp(op, pos{e.row, from}, pos{e.row, to}, exclusive)
}

// lineOp applies op to whole lines r1..r2.
func (e *editor) lineOp(op string, r1, r2 int) {
	r2 = min(r2, len(e.lines)-1)
	switch op {
	case "y":
		e.vim.reg, e.vim.regLine = strings.Join(e.lines[r1:r2+1], "\n"), true
		e.row = r1
	case "d":
		e.vim.reg, e.vim.regLine = strings.Join(e.lines[r1:r2+1], "\n"), true
		e.beginChange()
		e.lines = append(e.lines[:r1:r1], e.lines[r2+1:]...)
		if len(e.lines) == 0 {
			e.lines = []string{""}
		}
		e.row = min(r1, len(e.lines)-1)
		e.col = firstNonBlank(e.lines[e.row])
		e.endChange()
	case "c":
		e.vim.reg, e.vim.regLine = strings.Join(e.lines[r1:r2+1], "\n"), true
		e.beginChange()
		indent := e.lines[r1][:firstNonBlank(e.lines[r1])]
		e.lines = append(e.lines[:r1:r1], append([]string{indent}, e.lines[r2+1:]...)...)
		e.row, e.col = r1, len([]rune(indent))
		e.enterInsert()
		return
	case ">", "<":
		e.beginChange()
		for r := r1; r <= r2; r++ {
			if op == ">" && e.lines[r] != "" {
				e.lines[r] = "  " + e.lines[r]
			} else if op == "<" {
				e.lines[r] = e.lines[r][min(2, firstNonBlank(e.lines[r])):]
			}
		}
		e.row, e.col = r1, firstNonBlank(e.lines[r1])
		e.endChange()
	}
	e.clampNormal()
}

func (e *editor) paste(after bool, n int) {
	if e.vim.reg == "" {
		return
	}
	e.beginChange()
	defer e.endChange()
	if e.vim.regLine {
		block := strings.Split(e.vim.reg, "\n")
		var ins []string
		for i := 0; i < n; i++ {
			ins = append(ins, block...)
		}
		at := e.row
		if after {
			at++
		}
		e.lines = append(e.lines[:at:at], append(ins, e.lines[at:]...)...)
		e.row, e.col = at, firstNonBlank(e.lines[at])
		return
	}
	ins := []rune(strings.Repeat(e.vim.reg, n))
	col := e.col
	if after && len(e.line()) > 0 {
		col++
	}
	t := e.text()
	o := e.off(pos{e.row, col})
	e.setText(append(t[:o:o], append(ins, t[o:]...)...))
	p := e.posAt(o + len(ins) - 1)
	e.row, e.col = p.row, p.col
	e.clampNormal()
}

// openLine starts a new line below (o) or above (O) and enters insert mode.
// Below a list item it continues the list, like Enter does.
func (e *editor) openLine(below bool) {
	cur := e.lines[e.row]
	prefix := cur[:firstNonBlank(cur)]
	at := e.row
	if below {
		if p, _, ok := continuation(cur); ok {
			prefix = p
		}
		at++
	}
	e.lines = append(e.lines[:at:at], append([]string{prefix}, e.lines[at:]...)...)
	e.row, e.col = at, len([]rune(prefix))
	e.enterInsert()
}

func (e *editor) join(times int) {
	if e.row >= len(e.lines)-1 {
		return
	}
	e.beginChange()
	for i := 0; i < times && e.row < len(e.lines)-1; i++ {
		cur := strings.TrimRight(e.lines[e.row], " ")
		next := strings.TrimLeft(e.lines[e.row+1], " \t")
		sep := ""
		if cur != "" && next != "" {
			sep = " "
		}
		e.col = len([]rune(cur))
		e.lines[e.row] = cur + sep + next
		e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	}
	e.endChange()
}

func (e *editor) toggleCase(n int) {
	l := e.line()
	if len(l) == 0 {
		return
	}
	e.beginChange()
	for i := e.col; i < len(l) && i < e.col+n; i++ {
		if unicode.IsUpper(l[i]) {
			l[i] = unicode.ToLower(l[i])
		} else {
			l[i] = unicode.ToUpper(l[i])
		}
	}
	e.setLine(l)
	e.col = min(e.col+n, len(l)-1)
	e.endChange()
}

// deleteWordBack is insert-mode ctrl+w.
func (e *editor) deleteWordBack() {
	if e.col == 0 {
		e.backspace()
		return
	}
	l := e.line()
	i := e.col
	for i > 0 && unicode.IsSpace(l[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(l[i-1]) {
		i--
	}
	e.snap("delete")
	e.setLine(append(l[:i:i], l[e.col:]...))
	e.col = i
	e.follow()
}

// Command line ----------------------------------------------------------------

func (e *editor) commandKey(msg tea.KeyMsg) editAction {
	v := &e.vim
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		v.mode, v.cmd = vimNormal, ""
	case tea.KeyBackspace:
		if v.cmd == "" {
			v.mode = vimNormal
		} else {
			r := []rune(v.cmd)
			v.cmd = string(r[:len(r)-1])
		}
	case tea.KeyEnter:
		cmd := strings.TrimSpace(v.cmd)
		v.mode, v.cmd = vimNormal, ""
		return e.runCommand(cmd)
	case tea.KeySpace:
		v.cmd += " "
	case tea.KeyRunes:
		v.cmd += string(msg.Runes)
	}
	return actNone
}

func (e *editor) runCommand(cmd string) editAction {
	switch cmd {
	case "":
		return actNone
	case "w":
		return actSave
	case "w!":
		return actForceSave
	case "wq", "x":
		return actSaveExit
	case "wq!", "x!":
		return actForceSaveExit
	case "q":
		if e.dirty {
			e.vim.msg = "E37: No write since last change (add ! to override)"
			return actNone
		}
		return actExit
	case "q!", "e!":
		return actDiscard
	}
	if n, err := strconv.Atoi(cmd); err == nil {
		e.row = max(0, min(len(e.lines)-1, n-1))
		e.col = firstNonBlank(e.lines[e.row])
		e.clampNormal()
		e.follow()
		return actNone
	}
	e.vim.msg = "E492: Not an editor command: " + cmd
	return actNone
}
