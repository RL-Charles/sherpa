package tui

import (
	"strings"
	"testing"
)

func TestSplitBlocks(t *testing.T) {
	src := strings.Join([]string{
		"## Description", //  0 heading
		"Some text",      //  1 paragraph
		"over two lines", //  2
		"",               //  3
		"- [ ] first",    //  4 list item
		"  continued",    //  5   continuation
		"- [x] second",   //  6 list item
		"  - nested",     //  7 nested item
		"",               //  8
		"```go",          //  9 fence
		"a := 1",         // 10
		"",               // 11   blank inside fence
		"```",            // 12
		"| a | b |",      // 13 table
		"|---|---|",      // 14
		"> quote",        // 15 quote
		"Setext",         // 16 setext heading
		"---",            // 17
		"",               // 18
		"***",            // 19 rule
	}, "\n")
	got := splitBlocks(strings.Split(src, "\n"))
	want := []block{
		{0, 1, bHeading}, {1, 3, bParagraph}, {4, 6, bList}, {6, 7, bList}, {7, 8, bList},
		{9, 13, bFence}, {13, 15, bTable}, {15, 16, bQuote}, {16, 18, bParagraph}, {19, 20, bRule},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestSplitUnclosedFence(t *testing.T) {
	got := splitBlocks([]string{"```", "code", "", "more"})
	if len(got) != 1 || got[0] != (block{0, 4, bFence}) {
		t.Fatalf("unclosed fence should run to EOF, got %v", got)
	}
}
