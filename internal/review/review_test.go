//go:build !windows

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/store"
)

func waitFor(t *testing.T, path string) string {
	t.Helper()
	for range 100 {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
	return ""
}

func TestSpawn(t *testing.T) {
	root := t.TempDir()
	s, err := store.Init(root, "RV", "rv")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHERPA_ACTOR", "agent:claude")
	t.Setenv("CLAUDECODE", "1")
	out := filepath.Join(root, "out")
	cmd := `env | grep -E '^(SHERPA|CLAUDECODE)' | sort > ` + out + `.env; cat > ` + out + `.tmp && mv ` + out + `.tmp ` + out
	if _, err := Spawn(s, cmd, "T-0007", 0); err != nil {
		t.Fatal(err)
	}
	if prompt := waitFor(t, out); !strings.Contains(prompt, "shp review T-0007 --pass") {
		t.Fatalf("prompt on stdin should name the ticket:\n%s", prompt)
	}
	env := waitFor(t, out+".env")
	for _, want := range []string{"SHERPA_ACTOR=agent:reviewer", "SHERPA_REVIEW=T-0007", "SHERPA_SESSION=review-T-0007-"} {
		if !strings.Contains(env, want) {
			t.Fatalf("reviewer env missing %s:\n%s", want, env)
		}
	}
	if strings.Contains(env, "CLAUDECODE") || strings.Contains(env, "agent:claude") {
		t.Fatalf("the author's identity must not leak into the reviewer:\n%s", env)
	}
}

func TestSpawnNeedsCommand(t *testing.T) {
	s, err := store.Init(t.TempDir(), "RV", "rv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(s, " ", "T-0001", 0); err == nil {
		t.Fatal("an empty command must not launch anything")
	}
}

func TestSpawnQueuesPastMax(t *testing.T) {
	root := t.TempDir()
	s, err := store.Init(root, "RV", "rv")
	if err != nil {
		t.Fatal(err)
	}
	hold := filepath.Join(root, "hold")
	next := filepath.Join(root, "next")
	fake := filepath.Join(root, "fake shp")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\n[ \"$*\" = \"review --next\" ] && touch "+next+"\n"), 0o755)
	Self = func() (string, error) { return fake, nil }
	t.Cleanup(func() { Self = os.Executable })
	cmd := `while [ ! -e ` + hold + ` ]; do sleep 0.05; done`
	if _, err := Spawn(s, cmd, "T-0001", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(s, cmd, "T-0002", 1); err != ErrQueued {
		t.Fatalf("second reviewer past max should queue, got %v", err)
	}
	if got := Queued(s); len(got) != 1 || got[0] != "T-0002" {
		t.Fatalf("queue = %v", got)
	}
	if n := Count(s, ""); n != 1 {
		t.Fatalf("running = %d", n)
	}
	if n := Count(s, "T-0001"); n != 0 {
		t.Fatalf("a finishing reviewer must not count itself, got %d", n)
	}
	_ = os.WriteFile(hold, nil, 0o644)
	for range 100 {
		if _, err := os.Stat(next); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a capped reviewer must run `review --next` when it exits")
}
