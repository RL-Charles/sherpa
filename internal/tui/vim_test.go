package tui

import "testing"

// run feeds seq to a fresh editor holding body (cursor at 0,0, normal mode).
func run(t *testing.T, body, seq string) (*editor, editAction) {
	t.Helper()
	e := editorWith(body)
	act := actNone
	for _, k := range keys(seq) {
		if a := e.vimKey(k); a != actNone {
			act = a
		}
	}
	return e, act
}

func TestVimEdits(t *testing.T) {
	cases := []struct{ name, body, seq, want string }{
		{"dw", "hello world", "dw", "world"},
		{"cw acts like ce", "hello world", "cwbye<esc>", "bye world"},
		{"dd then p", "a\nb\nc", "ddp", "b\na\nc"},
		{"yyp duplicates", "a\nb", "yyp", "a\na\nb"},
		{"count x", "abcdef", "3x", "def"},
		{"undo", "abcdef", "3xu", "abcdef"},
		{"redo", "abcdef", "3xu<c-r>", "def"},
		{"ciw", "foo bar baz", "wciwX<esc>", "foo X baz"},
		{"A appends", "hi", "A!<esc>", "hi!"},
		{"o continues checklist", "- [x] a", "ob<esc>", "- [x] a\n- [ ] b"},
		{"O opens above", "  b", "Oa<esc>", "  a\n  b"},
		{"2dd", "a\nb\nc", "2dd", "c"},
		{"dj", "a\nb\nc", "dj", "c"},
		{"D", "keep cut", "wD", "keep "},
		{"J joins", "a\n  b", "J", "a b"},
		{"G and gg", "a\nb\nc", "Gx" + "ggx", "\nb"},
		{"dt)", "call(arg)", "f(ldt)", "call()"},
		{"di(", "f(a, b)", "f,di(", "f()"},
		{`ci"`, `say "hi" now`, `f"lci"yo<esc>`, `say "yo" now`},
		{">> and <<", "- a", ">>", "  - a"},
		{"<<", "    x", "<<", "  x"},
		{"r replaces", "cat", "lrX", "cXt"},
		{"~ toggles case", "abc", "2~", "ABc"},
		{"$ and x", "abc", "$x", "ab"},
		{"insert is one undo step", "x", "ihello world<esc>u", "x"},
		{"ctrl+w in insert", "x", "Afoo bar<c-w><esc>", "xfoo "},
		{"ctrl+x toggles checkbox", "- [ ] t", "<c-x>", "- [x] t"},
		{"counted motion", "abcdef", "3lx", "abcef"},
		{"p charwise", "ab", "xp", "ba"},
		{"de", "one two", "de", " two"},
		{"b motion", "one two", "$bD", "one "},
		{"} paragraph", "a\nb\n\nc", "}dd", "a\nb\nc"},
		{":N goes to line", "a\nb\nc", ":3<cr>x", "a\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := run(t, c.body, c.seq)
			if got := e.body(); got != c.want+"\n" {
				t.Fatalf("%q on %q: got %q want %q", c.seq, c.body, got, c.want+"\n")
			}
		})
	}
}

func TestVimModesAndCommands(t *testing.T) {
	e, _ := run(t, "abc", "A")
	if e.vim.mode != vimInsert {
		t.Fatal("A should enter insert mode")
	}
	e, _ = run(t, "abc", "Ax<esc>")
	if e.vim.mode != vimNormal || e.col != 3 {
		t.Fatalf("esc should return to normal with cursor on last char, col=%d", e.col)
	}
	if _, act := run(t, "abc", "ZZ"); act != actSaveExit {
		t.Fatal("ZZ should save and exit")
	}
	if _, act := run(t, "abc", ":w<cr>"); act != actSave {
		t.Fatal(":w should save")
	}
	if e, act := run(t, "abc", "x:q<cr>"); act != actNone || e.vim.msg == "" {
		t.Fatal(":q with unsaved changes should refuse with a message")
	}
	if _, act := run(t, "abc", ":q<cr>"); act != actExit {
		t.Fatal(":q without changes should exit")
	}
	if _, act := run(t, "abc", "x:q!<cr>"); act != actDiscard {
		t.Fatal(":q! should discard")
	}
	if e, _ := run(t, "abc", ":nope<cr>"); e.vim.msg == "" {
		t.Fatal("unknown command should warn")
	}
	e, _ = run(t, "abc", "2d")
	if e.vim.pending() != "2d" {
		t.Fatalf("pending should show 2d, got %q", e.vim.pending())
	}
}
