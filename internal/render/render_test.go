package render

import (
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

func TestAgeAndAgo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		at  time.Time
		age string
		ago string
	}{
		{now.Add(-10 * time.Second), "just now", "just now"},
		{now.Add(-5 * time.Minute), "5m", "5m ago"},
		{now.Add(-3 * time.Hour), "3h", "3h ago"},
		{now.Add(-48 * time.Hour), "2d", "2d ago"},
		{now.Add(-10 * 24 * time.Hour), "1w", "1w ago"},
	}
	for _, c := range cases {
		if got := Age(c.at); got != c.age {
			t.Errorf("Age(%v) = %q, want %q", c.at, got, c.age)
		}
		if got := Ago(c.at); got != c.ago {
			t.Errorf("Ago(%v) = %q, want %q", c.at, got, c.ago)
		}
	}
	if got := Age(time.Time{}); got != "" {
		t.Errorf("Age(zero) = %q, want empty", got)
	}
}

func TestPreviewHeaderCarriesIdentity(t *testing.T) {
	it := &model.Item{
		ID:       "T-0030",
		Title:    "JWT refresh",
		Kind:     model.Ticket,
		Status:   model.InProgress,
		Assignee: "claude",
		Parent:   "S-004",
		Now:      "client retry is left",
		Updated:  time.Now().Add(-2 * time.Hour),
		Checks:   []model.Check{{Text: "one", Done: true}, {Text: "two"}},
		Links:    &model.Links{PRNumber: 35, PRState: "open"},
	}
	parent := &model.Item{ID: "S-004", Title: "Board", Kind: model.Story}
	got := PreviewHeader(it, parent)
	for _, want := range []string{
		"T-0030 · JWT refresh",
		"in progress",
		"@claude",
		"S-004",
		"1/2 checks",
		"⇡ #35",
		"client retry is left",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PreviewHeader missing %q:\n%s", want, got)
		}
	}
}

func TestReviewCardDoesNotRepeatCheckCount(t *testing.T) {
	it := &model.Item{
		ID:     "T-0046",
		Status: model.Review,
		Checks: []model.Check{{Text: "one"}, {Text: "two"}},
	}
	e := journal.Event{
		Actor:    "agent:claude",
		Message:  "changed the thing",
		Verified: "go test ./...",
	}
	got := ReviewCard(it, e)
	if strings.Contains(got, "checks") {
		t.Errorf("review card should leave the check count to the status strip:\n%s", got)
	}
	for _, want := range []string{"Review", "Changed", "Verified"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReviewCard missing %q:\n%s", want, got)
		}
	}
}

func TestStyleHasSectionRule(t *testing.T) {
	if !strings.Contains(Style("dark").H2.BlockPrefix, "─") {
		t.Fatal("H2 should render a section rule")
	}
}

func TestSectionsDeliverablesAndCodeMap(t *testing.T) {
	it := &model.Item{
		ID:     "T-0030",
		Kind:   model.Ticket,
		Title:  "JWT refresh",
		Status: model.InProgress,
		Deliverables: []model.Deliverable{
			{Text: "Refresh accepts an expired token", Done: true, Evidence: "abc1234"},
			{Text: "Web client retries once"},
		},
		Checks: []model.Check{
			{Text: "unit: go test ./auth", Done: true, Evidence: "abc1234", For: 1},
			{Text: "e2e: two expiries", For: 1},
			{Text: "unlinked check"},
			{Text: "bad link", For: 9},
		},
		Code: []model.CodeRef{
			{Path: "internal/auth/token.go", Change: "modify", Symbols: []string{"Rotate", "Clock"}, Deliverable: 1, Status: "planned", Note: "rotation"},
			{Path: "api/auth/handlers.go", Change: "modify", Symbols: []string{"handleRefresh"}, Deliverable: 2},
		},
	}
	got := Sections(it, nil, nil)
	for _, want := range []string{
		"## Deliverables · 1/2",
		"### 1. Refresh accepts an expired token ✓ · 1/2 checks",
		"_evidence: `abc1234`_",
		"unit: go test ./auth",
		"e2e: two expiries",
		"### 2. Web client retries once ○",
		"_No checks yet",
		"## Other checks · 0/2",
		"unlinked check",
		"bad link",
		"_for deliverable 9_",
		"## Where",
		"internal/auth",
		"token.go",
		"Rotate, Clock",
		"deliver 1",
		"rotation",
		"api/auth",
		"handlers.go",
		"deliver 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Sections missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "unit: go test ./auth") != 1 {
		t.Errorf("linked check should render once:\n%s", got)
	}
}

func TestCodeMapGroupsByDirectory(t *testing.T) {
	it := &model.Item{Code: []model.CodeRef{
		{Path: "web/src/session.ts", Change: "add", Symbols: []string{"retryOnce"}, Status: "done"},
		{Path: "internal/auth/token.go", Change: "modify", Symbols: []string{"Rotate"}, Deliverable: 1},
	}}
	got := CodeMap(it)
	if !strings.Contains(got, "web/src\n") || !strings.Contains(got, "internal/auth\n") {
		t.Fatalf("directories not grouped:\n%s", got)
	}
	if strings.Index(got, "internal/auth") > strings.Index(got, "web/src") {
		t.Errorf("code map should be path-sorted:\n%s", got)
	}
	if !strings.Contains(got, "add") || !strings.Contains(got, "done") || !strings.Contains(got, "deliver 1") {
		t.Errorf("change/state/deliverable missing:\n%s", got)
	}
}

func TestPreviewHeaderShowsBriefCounts(t *testing.T) {
	it := &model.Item{
		ID: "T-0030", Title: "JWT refresh", Kind: model.Ticket, Status: model.InProgress,
		Deliverables: []model.Deliverable{{Text: "one", Done: true}, {Text: "two"}},
		Checks:       []model.Check{{Text: "a", Done: true}, {Text: "b"}},
		Code:         []model.CodeRef{{Path: "a.go"}, {Path: "b.go"}},
	}
	got := PreviewHeader(it, nil)
	for _, want := range []string{"1/2 checks", "1/2 deliverables", "2 files"} {
		if !strings.Contains(got, want) {
			t.Errorf("PreviewHeader missing %q:\n%s", want, got)
		}
	}
}
