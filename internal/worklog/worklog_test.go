package worklog

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
)

func at(min int) time.Time { return time.Date(2026, 10, 7, 10, min, 0, 0, time.Local) }

func TestBuildAndRender(t *testing.T) {
	events := []journal.Event{
		{TS: at(0), Actor: "agent:claude", Type: "create", Item: "T-1"},
		{TS: at(1), Actor: "agent:claude", Type: "status", To: "ready", From: "backlog"},
		// session 1, legacy shape: status+claim pair, submit as a status change
		{TS: at(2), Actor: "agent:claude", Type: "status", From: "ready", To: "in_progress", Message: "claimed"},
		{TS: at(2), Actor: "agent:claude", Type: "claim"},
		{TS: at(5), Actor: "agent:claude", Type: "log", Message: "old style note", Files: []string{"a/x.go"}},
		{TS: at(9), Actor: "agent:claude", Type: "status", From: "in_progress", To: "review", Message: "done it"},
		// session 2, new shape
		{TS: at(20), Actor: "agent:claude", Type: "claim", From: "review", Commit: "aaa1111"},
		{TS: at(25), Actor: "agent:claude", Type: "log", Kind: "decision", Message: "Use a line splitter",
			Detail: []string{"goldmark lacks positions"}, Files: []string{"blocks.go"}, Commit: "aaa1111", Added: 40, Removed: 2},
		{TS: at(30), Actor: "agent:claude", Type: "submit", Message: "Splitter shipped", Verified: "go test ./...",
			Followups: []string{"visual mode"}, Commit: "bbb2222", Added: 120, Removed: 7},
		{TS: at(40), Actor: "human:charles", Type: "status", From: "review", To: "done"},
	}
	ss := Build(events)
	if len(ss) != 3 || ss[0].N != 0 || ss[1].N != 1 || ss[2].N != 2 {
		t.Fatalf("want planning + 2 sessions, got %+v", ss)
	}
	if len(ss[1].Entries) != 3 {
		t.Fatalf("legacy claim status event should be absorbed: %d entries", len(ss[1].Entries))
	}
	s2 := ss[2]
	if s2.Open || s2.Base != "aaa1111" || s2.Head != "bbb2222" || s2.Added != 120 {
		t.Fatalf("session 2 summary wrong: %+v", s2)
	}

	text := ansi.Strip(strings.Join(Render(ss, 90, false), "\n"))
	for _, want := range []string{
		"Worklog  2 sessions · 9 entries",
		"▾ Session 2 · Oct 7 10:20–10:40 · claude · aaa1111..bbb2222  +120 −7",
		"⚑ Use a line splitter",
		"goldmark lacks positions",
		"blocks.go  +40 −2",
		"◑ submitted for review",
		"✓ verified: go test ./...",
		"→ follow-up: visual mode",
		"● approved · done · charles",
		"▸ Session 1", "▸ Planning",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old style note") {
		t.Error("older sessions should be collapsed by default")
	}
	if all := ansi.Strip(strings.Join(Render(ss, 90, true), "\n")); !strings.Contains(all, "✎ old style note") || !strings.Contains(all, "x.go") {
		t.Errorf("expandAll should show older sessions:\n%s", all)
	}
}

func TestOpenSession(t *testing.T) {
	ss := Build([]journal.Event{{TS: at(0), Actor: "agent:claude", Type: "claim"}})
	text := ansi.Strip(strings.Join(Render(ss, 80, false), "\n"))
	if !strings.Contains(text, "in progress") {
		t.Fatalf("open session should be marked:\n%s", text)
	}
}

func TestTouchedFilesSummarised(t *testing.T) {
	ss := Build([]journal.Event{
		{TS: at(0), Actor: "agent:claude", Type: "claim"},
		{TS: at(1), Actor: "agent:claude", Type: "touch", Files: []string{"a.go"}},
		{TS: at(2), Actor: "agent:claude", Type: "touch", Files: []string{"b.go", "a.go"}},
		{TS: at(3), Actor: "agent:claude", Type: "log", Message: "did it"},
	})
	if len(ss) != 1 || len(ss[0].Entries) != 2 || strings.Join(ss[0].Touched, ",") != "a.go,b.go" {
		t.Fatalf("touch events should be summarised, not listed: %+v", ss)
	}
	if text := ansi.Strip(strings.Join(Render(ss, 80, false), "\n")); !strings.Contains(text, "files touched: a.go · b.go") {
		t.Fatalf("missing touched line:\n%s", text)
	}
}
