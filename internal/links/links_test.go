package links

import "testing"

func TestFromText(t *testing.T) {
	url, n := FromText("Creating pull request...\nhttps://github.com/RL-Charles/sherpa/pull/12\n")
	if url != "https://github.com/RL-Charles/sherpa/pull/12" || n != 12 {
		t.Fatalf("got %q %d", url, n)
	}
	if url, _ := FromText("Create a PR by visiting https://github.com/o/r/pull/new/feature"); url != "" {
		t.Fatalf("a 'new PR' link is not a PR: %q", url)
	}
}

func TestMatch(t *testing.T) {
	prs := []pr{
		{Number: 40, HeadRefName: "agent/app-30-retry", State: "CLOSED"},
		{Number: 35, HeadRefName: "agent/app-30-oauth-login", State: "MERGED"},
		{Number: 36, HeadRefName: "agent/app-3-db-pool", State: "MERGED"},
		{Number: 50, Title: "T-0042: importer", State: "OPEN"},
	}
	if p, ok := Match(prs, []string{"T-0030", "app-30"}); !ok || p.Number != 35 {
		t.Fatalf("want merged PR 35 for app-30, got %+v", p)
	}
	if p, ok := Match(prs, []string{"T-0003", "app-3"}); !ok || p.Number != 36 {
		t.Fatalf("app-3 must not match app-30: %+v", p)
	}
	if p, ok := Match(prs, []string{"T-0042"}); !ok || p.Number != 50 {
		t.Fatalf("title match: %+v", p)
	}
	if _, ok := Match(prs, []string{"T-0099"}); ok {
		t.Fatal("no match expected")
	}
}

func TestMatchRanges(t *testing.T) {
	prs := []pr{
		{Number: 37, Title: "Parser complete (APP-72..78, APP-106) with offline CI", State: "MERGED"},
		{Number: 22, Title: "Dashboard milestone 1 (APP-45–48)", State: "MERGED"},
	}
	for key, want := range map[string]int{"app-75": 37, "app-78": 37, "app-106": 37, "app-46": 22} {
		if p, ok := Match(prs, []string{key}); !ok || p.Number != want {
			t.Errorf("%s: got %+v want PR %d", key, p, want)
		}
	}
	if _, ok := Match(prs, []string{"app-79"}); ok {
		t.Error("app-79 is outside 72..78")
	}
}
