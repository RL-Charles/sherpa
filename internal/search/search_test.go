package search

import (
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func items() []*model.Item {
	mk := func(id, title string, st model.Status, k model.Kind, who, body string) *model.Item {
		return &model.Item{ID: id, Title: title, Status: st, Kind: k, Assignee: who, Body: body}
	}
	return []*model.Item{
		mk("E-001", "Platform", model.InProgress, model.Epic, "", ""),
		mk("S-001", "Auth", model.Ready, model.Story, "", ""),
		mk("T-0030", "Add OAuth login", model.Done, model.Ticket, "claude", "Uses PKCE for the code exchange."),
		{ID: "T-0031", Title: "Rotate refresh tokens", Status: model.Ready, Kind: model.Ticket,
			Body:         "Tokens rotate on every use.",
			Deliverables: []model.Deliverable{{Text: "Each refresh revokes the previous token"}},
			Code:         []model.CodeRef{{Path: "internal/auth/token.go", Symbols: []string{"Rotate"}}},
		},
		mk("T-0032", "Load balancer health checks", model.Backlog, model.Ticket, "codex", ""),
	}
}

func ids(rs []Result) (out []string) {
	for _, r := range rs {
		out = append(out, r.Item.ID)
	}
	return
}

func TestFuzzy(t *testing.T) {
	if _, _, ok := Fuzzy("olg", "Add OAuth login"); !ok {
		t.Fatal("letters in order with gaps should match")
	}
	if _, _, ok := Fuzzy("zz", "Add OAuth login"); ok {
		t.Fatal("missing letters must not match")
	}
	tight, _, _ := Fuzzy("login", "Add OAuth login")
	loose, _, _ := Fuzzy("login", "Load balancer health checks with gin")
	if tight <= loose {
		t.Fatalf("a whole-word match should beat a scattered one: %d vs %d", tight, loose)
	}
	_, hits, _ := Fuzzy("oal", "Add OAuth login")
	if len(hits) != 3 || hits[0] != 4 {
		t.Fatalf("hits should point at the tightest match: %v", hits)
	}
}

func TestSearch(t *testing.T) {
	cases := []struct {
		q     string
		first string
		n     int
	}{
		{"oauth", "T-0030", -1},
		{"rot tok", "T-0031", -1},
		{"30", "T-0030", -1},
		{"t31", "T-0031", -1},
		{"is:ready", "S-001", 2},
		{"is:ticket is:ready", "T-0031", 1},
		{"@codex", "T-0032", 1},
		{"pkce", "T-0030", 1},     // found in the body
		{"token.go", "T-0031", 1}, // found in the code map
		{"revokes", "T-0031", 1},  // found in a deliverable
	}
	for _, c := range cases {
		rs := Search(items(), c.q, 0)
		if len(rs) == 0 || rs[0].Item.ID != c.first {
			t.Errorf("%q: got %v, want %s first", c.q, ids(rs), c.first)
			continue
		}
		if c.n >= 0 && len(rs) != c.n {
			t.Errorf("%q: got %d results %v, want %d", c.q, len(rs), ids(rs), c.n)
		}
	}
	if rs := Search(items(), "pkce", 0); rs[0].Snippet == "" {
		t.Error("body matches should carry a snippet")
	}
	if rs := Search(items(), "oauth", 0); len(rs[0].Hits) == 0 {
		t.Error("title matches should carry highlight positions")
	}
	if rs := Search(items(), "", 3); len(rs) != 3 {
		t.Errorf("limit not applied: %d", len(rs))
	}
}
