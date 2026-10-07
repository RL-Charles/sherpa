package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiffStat(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "init", "-q")
	if Head(dir) != "" {
		t.Fatal("no commits yet: Head should be empty")
	}
	write("a.go", "one\ntwo\n")
	if a, r := DiffStat(dir, "", []string{"a.go"}); a != 2 || r != 0 {
		t.Fatalf("before first commit: got +%d -%d", a, r)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "init")
	base := Head(dir)
	if base == "" {
		t.Fatal("Head should be set after a commit")
	}
	write("a.go", "one\nTWO\nthree\n") // 1 changed + 1 added → +2 -1
	write("new.go", "x\ny\nz\n")       // untracked → +3
	a, r := DiffStat(dir, base, []string{"a.go", "new.go", "missing.go"})
	if a != 5 || r != 1 {
		t.Fatalf("got +%d -%d, want +5 -1", a, r)
	}
}
