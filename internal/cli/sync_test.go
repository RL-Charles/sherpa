package cli

import (
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

// shp sync loads every ticket, then spends minutes on GitHub calls before it
// writes links. An edit made in that window (a ticked check, a Now line)
// must survive the write; before writeLinks the stale copy overwrote it.
func TestSyncLinksKeepEditsMadeWhileSyncRan(t *testing.T) {
	s, err := store.Init(t.TempDir(), "SY", "sync-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(store.NewItem{Kind: model.Epic, Title: "E"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(store.NewItem{Kind: model.Story, Title: "S", Parent: "E-001"}); err != nil {
		t.Fatal(err)
	}
	it, err := s.Create(store.NewItem{Kind: model.Ticket, Title: "T", Parent: "S-001"})
	if err != nil {
		t.Fatal(err)
	}
	// What sync loaded at its start.
	stale, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Meanwhile another session edits the ticket.
	edited, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited.Now = "checks ticked while sync ran"
	if err := s.Save(edited); err != nil {
		t.Fatal(err)
	}
	// Sync then writes the links it derived from the stale copy.
	l := stale.Links
	if l == nil {
		l = &model.Links{}
	}
	l.PR = "https://github.com/o/r/pull/7"
	l.PRNumber = 7
	a := &app{store: s}
	wrote, err := a.writeLinks(it.ID, l)
	if err != nil || !wrote {
		t.Fatalf("writeLinks = %v, %v", wrote, err)
	}
	got, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Now != "checks ticked while sync ran" {
		t.Fatalf("the concurrent edit was lost: Now = %q", got.Now)
	}
	if got.Links == nil || got.Links.PRNumber != 7 {
		t.Fatalf("links not written: %+v", got.Links)
	}
	// Writing the same links again is a no-op.
	if wrote, err := a.writeLinks(it.ID, l); err != nil || wrote {
		t.Fatalf("second writeLinks = %v, %v", wrote, err)
	}
}
