package hooks

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

func project(t *testing.T) (*store.Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Init(root, "HK", "hooks")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []store.NewItem{
		{Kind: model.Epic, Title: "E"},
		{Kind: model.Story, Title: "S", Parent: "E-001"},
		{Kind: model.Ticket, Title: "Do the thing", Parent: "S-001"},
	} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	return s, root
}

func claim(t *testing.T, s *store.Store) {
	t.Helper()
	it, _ := s.Get("T-0001")
	it.Status = model.InProgress
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("T-0001"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(s.Dir, journal.Event{Actor: "agent:claude", Session: "x", Item: "T-0001", Type: "claim"}); err != nil {
		t.Fatal(err)
	}
}

func TestPreEdit(t *testing.T) {
	s, root := project(t)
	in := Input{Agent: "claude", Event: PreEdit, Dir: root, Files: []string{filepath.Join(root, "main.go")}}
	if out := Handle(in); !out.Block || !strings.Contains(out.Reason, "shp start") || !strings.Contains(out.Reason, "main.go") {
		t.Fatalf("edit without a claimed ticket should be blocked with guidance: %+v", out)
	}
	if out := Handle(Input{Event: PreEdit, Dir: root, Files: []string{"/tmp/elsewhere.txt"}}); out.Block {
		t.Fatal("files outside the repo are not policed")
	}
	claim(t, s)
	if out := Handle(in); out.Block {
		t.Fatalf("edit with a claimed ticket should pass: %+v", out)
	}
	if out := Handle(Input{Event: PreEdit, Dir: root, Files: []string{".sherpa/tickets/T-0001.md"}}); !out.Block {
		t.Fatal("hand edits to .sherpa should be blocked")
	}
	if out := Handle(Input{Event: PreEdit, Dir: t.TempDir(), Files: []string{"x.go"}}); out.Block {
		t.Fatal("outside a Sherpa project hooks must fail open")
	}
}

func TestPostEditAndStop(t *testing.T) {
	s, root := project(t)
	claim(t, s)
	if out := Handle(Input{Event: Stop, Dir: root}); out.Block {
		t.Fatal("nothing edited yet: stop should pass")
	}
	time.Sleep(1100 * time.Millisecond) // journal timestamps have second resolution
	Handle(Input{Agent: "claude", Event: PostEdit, Dir: root, Files: []string{filepath.Join(root, "a.go"), "/tmp/outside"}})
	events, _ := journal.ReadAll(s.Dir)
	last := events[len(events)-1]
	if last.Type != "touch" || strings.Join(last.Files, ",") != "a.go" || last.Actor != "agent:claude" {
		t.Fatalf("post-edit should record the in-repo file: %+v", last)
	}
	out := Handle(Input{Event: Stop, Dir: root})
	if !out.Block || !strings.Contains(out.Reason, "T-0001") || !strings.Contains(out.Reason, "shp submit") {
		t.Fatalf("unlogged edits should block stop: %+v", out)
	}
	if out := Handle(Input{Event: Stop, Dir: root, StopActive: true}); out.Block {
		t.Fatal("must never block twice in a row")
	}
	if out := Handle(Input{Event: Stop, Dir: root}); out.Block {
		t.Fatal("already nudged for these edits: a second stop must pass even without stop_hook_active")
	}
	time.Sleep(1100 * time.Millisecond)
	_ = journal.Append(s.Dir, journal.Event{Actor: "agent:claude", Session: "x", Item: "T-0001", Type: "log", Message: "progress"})
	if out := Handle(Input{Event: Stop, Dir: root}); out.Block {
		t.Fatal("after logging, stop should pass")
	}
}

func TestSessionContext(t *testing.T) {
	s, root := project(t)
	it, _ := s.Get("T-0001")
	it.Status = model.Ready
	_ = s.Save(it)
	out := Handle(Input{Event: SessionStart, Dir: root})
	for _, want := range []string{"No ticket is claimed", "Ready to start", "T-0001 Do the thing", "shp submit"} {
		if !strings.Contains(out.Context, want) {
			t.Errorf("context missing %q:\n%s", want, out.Context)
		}
	}
	claim(t, s)
	if out := Handle(Input{Event: SessionStart, Dir: root}); !strings.Contains(out.Context, "Active ticket in this checkout: T-0001") {
		t.Errorf("context should name the active ticket:\n%s", out.Context)
	}
}

func TestPostShellLinksPR(t *testing.T) {
	s, root := project(t)
	out := `{"tool_response":{"stdout":"https://github.com/acme/app/pull/42\n"}}`
	Handle(Input{Agent: "claude", Event: PostShell, Dir: root, Command: "gh pr create --fill", Output: out})
	if it, _ := s.Get("T-0001"); it.Links != nil {
		t.Fatal("no active ticket: nothing should be linked")
	}
	claim(t, s)
	Handle(Input{Agent: "claude", Event: PostShell, Dir: root, Command: "ls -la", Output: out})
	if it, _ := s.Get("T-0001"); it.Links != nil {
		t.Fatal("only gh pr commands link PRs")
	}
	Handle(Input{Agent: "claude", Event: PostShell, Dir: root, Command: "gh pr create --fill", Output: out})
	it, _ := s.Get("T-0001")
	if it.Links == nil || it.Links.PRNumber != 42 || it.Links.PR != "https://github.com/acme/app/pull/42" || it.Links.PRState != "open" {
		t.Fatalf("PR not linked: %+v", it.Links)
	}
}

func TestWorkingHeartbeat(t *testing.T) {
	s, root := project(t)
	Handle(Input{Agent: "claude", Event: Working, Dir: root})
	if got := live.Read(s.Dir, time.Now()); len(got) != 1 || got[0].Agent != "claude" || got[0].Ticket != "" {
		t.Fatalf("prompt before claiming: want claude on no ticket, got %+v", got)
	}
	claim(t, s)
	Handle(Input{Agent: "claude", Event: Working, Dir: root})
	if got := live.Read(s.Dir, time.Now()); len(got) != 1 || got[0].Ticket != "T-0001" {
		t.Fatalf("want claude on T-0001, got %+v", got)
	}
	Handle(Input{Agent: "claude", Event: Stop, Dir: root})
	if got := live.Read(s.Dir, time.Now()); len(got) != 0 {
		t.Fatalf("stop should clear the heartbeat, got %+v", got)
	}
	Handle(Input{Agent: "claude", Event: SessionStart, Dir: root})
	if got := live.Read(s.Dir, time.Now()); len(got) != 0 {
		t.Fatalf("session start is not work, got %+v", got)
	}
}

func TestStopNudgeKeepsHeartbeat(t *testing.T) {
	s, root := project(t)
	claim(t, s)
	Handle(Input{Agent: "claude", Event: Working, Dir: root})
	// An edit after the claim (journal timestamps have second resolution).
	_ = journal.Append(s.Dir, journal.Event{TS: time.Now().Add(2 * time.Second), Actor: "agent:claude", Session: "x",
		Item: "T-0001", Type: "touch", Files: []string{"main.go"}})
	if out := Handle(Input{Agent: "claude", Event: Stop, Dir: root}); !out.Block {
		t.Fatal("expected a stop nudge after an unlogged edit")
	}
	if got := live.Read(s.Dir, time.Now()); len(got) != 1 {
		t.Fatalf("a nudged agent keeps working; heartbeat should remain, got %+v", got)
	}
}
