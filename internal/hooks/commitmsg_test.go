package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddTrailer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Fix the thing\n", "Fix the thing\n\nTicket: T-0001\n"},
		{"Fix\n\nBody text.\n\nCo-Authored-By: A <a@b>\n", "Fix\n\nBody text.\n\nCo-Authored-By: A <a@b>\nTicket: T-0001\n"},
		{"Fix\n# Please enter the commit message\n# Lines starting with '#'\n", "Fix\n\nTicket: T-0001\n\n# Please enter the commit message\n# Lines starting with '#'\n"},
		{"docs: update readme\n", "docs: update readme\n\nTicket: T-0001\n"},
	}
	for _, c := range cases {
		if got := addTrailer(c.in, "Ticket: T-0001"); got != c.want {
			t.Errorf("addTrailer(%q):\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestCommitMsg(t *testing.T) {
	s, root := project(t)
	write := func(msg string) string {
		p := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
		if err := os.WriteFile(p, []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	if err := CommitMsg(root, write("No ticket\n"), true); err == nil || !strings.Contains(err.Error(), "shp start") {
		t.Fatalf("agent commit without ticket should be refused, got %v", err)
	}
	if err := CommitMsg(root, write("No ticket\n"), false); err != nil {
		t.Fatalf("humans may commit without a ticket: %v", err)
	}
	if err := CommitMsg(root, write("Merge branch 'x'\n"), true); err != nil {
		t.Fatalf("merges pass: %v", err)
	}
	if err := CommitMsg(root, write("Named\n\nTicket: T-0009\n"), true); err != nil {
		t.Fatalf("explicit ticket passes: %v", err)
	}
	claim(t, s)
	p := write("Claimed work\n")
	if err := CommitMsg(root, p, true); err != nil || !strings.HasSuffix(read(p), "\nTicket: T-0001\n") {
		t.Fatalf("claimed ticket should be added as a trailer: err=%v msg=%q", err, read(p))
	}
}
