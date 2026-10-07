package store

import (
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func TestRoundTrip(t *testing.T) {
	in := &model.Item{ID: "T-0001", Kind: model.Ticket, Title: "Hello: world", Status: model.Ready, Parent: "S-001",
		Labels: []string{"cli"}, Body: "## Description\n\n---\n\nhorizontal rule above\n"}
	data, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != in.Title || out.Parent != in.Parent || out.Status != in.Status || out.Body != in.Body {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", in, out)
	}
}

func TestCreateHierarchy(t *testing.T) {
	s, err := Init(t.TempDir(), "SHP", "sherpa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(NewItem{Kind: model.Ticket, Title: "orphan"}); err == nil {
		t.Fatal("ticket without parent should fail")
	}
	e, err := s.Create(NewItem{Kind: model.Epic, Title: "Epic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(NewItem{Kind: model.Ticket, Title: "wrong parent", Parent: e.ID}); err == nil {
		t.Fatal("ticket under an epic should fail")
	}
	st, err := s.Create(NewItem{Kind: model.Story, Title: "Story", Parent: "e1"})
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := s.Create(NewItem{Kind: model.Ticket, Title: "one", Parent: st.ID})
	t2, _ := s.Create(NewItem{Kind: model.Ticket, Title: "two", Parent: st.ID})
	if t1.ID != "T-0001" || t2.ID != "T-0002" || st.Parent != "E-001" {
		t.Fatalf("unexpected ids: %s %s parent=%s", t1.ID, t2.ID, st.Parent)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Kind != model.Epic || len(Children(all, st.ID)) != 2 {
		t.Fatalf("unexpected listing: %d items", len(all))
	}
}

func TestStripWorklog(t *testing.T) {
	body := "## Description\n\nx\n\n## Worklog\n\n- a\n- b\n\n## Notes\n\nkeep\n"
	got, changed := StripWorklog(body)
	if !changed || got != "## Description\n\nx\n\n## Notes\n\nkeep\n" {
		t.Fatalf("got %q", got)
	}
	if _, changed := StripWorklog(got); changed {
		t.Fatal("second strip should be a no-op")
	}
	if got, _ := StripWorklog("## Goal\n\ng\n\n## Worklog\n- a\n"); got != "## Goal\n\ng\n" {
		t.Fatalf("trailing worklog: got %q", got)
	}
}
