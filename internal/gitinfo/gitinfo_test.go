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

func TestCommit(t *testing.T) {
	dir := t.TempDir()
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "init", "-q")
	write("a.go", "a\n")
	write("gone.go", "g\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "init")
	base := Head(dir)

	write("a.go", "A\n")
	write("new.go", "n\n")
	write("other.go", "not mine\n")
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	sha, err := Commit(dir, "Do it\n\nTicket: T-0001", []string{"a.go", "new.go", "gone.go", "never.go"})
	if err != nil || sha == "" || sha == base {
		t.Fatalf("commit: sha %q err %v", sha, err)
	}
	files, _ := git(dir, "show", "--name-only", "--format=", "HEAD")
	if files != "a.go\ngone.go\nnew.go" {
		t.Fatalf("committed files:\n%s", files)
	}
	if msg, _ := git(dir, "log", "-1", "--format=%B"); msg != "Do it\n\nTicket: T-0001" {
		t.Fatalf("message %q", msg)
	}
	if st, _ := git(dir, "status", "--porcelain"); st != "?? other.go" {
		t.Fatalf("other files must stay out of the commit: %q", st)
	}
	if sha, err := Commit(dir, "again", []string{"a.go"}); sha != "" || err != nil {
		t.Fatalf("nothing to commit: %q %v", sha, err)
	}
}
