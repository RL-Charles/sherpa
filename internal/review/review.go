// Package review launches an independent agent to review submitted work.
//
//	agent: shp submit ──▶ agent_review ──▶ Spawn(reviewer.command)
//	                                            │  fresh session, prompt on stdin
//	                                            ▼
//	                        shp review <id> --pass | --reject
//
// The reviewer runs detached, in its own Sherpa session, so it can never be
// the session that wrote the work. Its output goes to
// .sherpa/.local/reviews/<id>.log.
package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RL-Charles/sherpa/internal/policy"
	"github.com/RL-Charles/sherpa/internal/store"
)

// Prompt is what the reviewer agent is asked to do. verify is the project's
// own check (policy.yaml verify), run on every review when set.
func Prompt(id, verify string) string {
	if verify != "" {
		verify = "\n   Always run the project's verify command too: " + verify
	}
	return strings.NewReplacer("{id}", id, "{verify}", verify).Replace(`You are an independent reviewer for Sherpa ticket {id}. Another agent did
the work; you did not. Do not edit code or tickets beyond the review itself.

1. shp brief {id} for the compact intent, deliverables, next check and code map;
   shp show {id} for the full brief and the submit summary;
   shp activity -i {id} for what was done and which files changed.
2. Read the changed code and the diff. Run the verification the submit
   claims (tests, build), and anything else you need to be convinced.{verify}
3. Check every ticked check against its evidence. Checks marked ✋ are
   manual: a human verifies those, so leave them alone.
4. Finish with exactly one command:
   shp review {id} --pass -m "what you checked and found"
   shp review {id} --reject -m "what is wrong and what to fix"
   Pass only if the work does what the ticket asks and the checks hold up.
   A pass closes the ticket unless .sherpa/policy.yaml sends it to a human
   (shp policy {id} says which), so your review may be the last one.
   Mention anything a human should look at by hand in your notes.
`)
}

// Session is the Sherpa session name a reviewer of id records under.
func Session(id string, now time.Time) string {
	return "review-" + id + "-" + now.UTC().Format("20060102T150405")
}

func dir(s *store.Store) string { return filepath.Join(s.Dir, ".local", "reviews") }

// Running reports whether a reviewer launched for id is still alive.
func Running(s *store.Store, id string) bool {
	data, err := os.ReadFile(filepath.Join(dir(s), id+".pid"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return err == nil && alive(pid)
}

// Self is the shp binary a capped reviewer runs `review --next` with.
var Self = os.Executable

// ErrQueued means the reviewer was not started because max reviewers are
// already running; it waits in the queue for Next.
var ErrQueued = errors.New("reviewer queued")

// Count is how many launched reviewers are still alive, not counting skip
// (the reviewer asking, which is finishing).
func Count(s *store.Store, skip string) int {
	pids, _ := filepath.Glob(filepath.Join(dir(s), "*.pid"))
	n := 0
	for _, p := range pids {
		id := strings.TrimSuffix(filepath.Base(p), ".pid")
		if id != skip && Running(s, id) {
			n++
		}
	}
	return n
}

// Queued lists the waiting reviews, oldest first.
func Queued(s *store.Store) []string {
	files, _ := filepath.Glob(filepath.Join(dir(s), "*.queued"))
	type q struct {
		id string
		t  time.Time
	}
	var qs []q
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			qs = append(qs, q{strings.TrimSuffix(filepath.Base(f), ".queued"), fi.ModTime()})
		}
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].t.Before(qs[j].t) })
	ids := make([]string, len(qs))
	for i, x := range qs {
		ids[i] = x.id
	}
	return ids
}

// Dequeue drops id from the queue.
func Dequeue(s *store.Store, id string) { _ = os.Remove(filepath.Join(dir(s), id+".queued")) }

// Spawn starts command (run by the shell) in the code checkout with the
// review prompt on stdin, detached from the caller. It returns the log path.
// With max > 0 and max reviewers alive it queues id instead (ErrQueued).
// When the command exits, `shp review --next` starts the oldest queued one.
func Spawn(s *store.Store, command, id string, max int) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", errors.New("no reviewer command configured (reviewer.command in .sherpa/config.yaml)")
	}
	if Running(s, id) {
		return "", fmt.Errorf("a reviewer for %s is already running", id)
	}
	if err := os.MkdirAll(dir(s), 0o755); err != nil {
		return "", err
	}
	if max > 0 && Count(s, os.Getenv("SHERPA_REVIEW")) >= max {
		if err := os.WriteFile(filepath.Join(dir(s), id+".queued"), nil, 0o644); err != nil {
			return "", err
		}
		return "", ErrQueued
	}
	Dequeue(s, id)
	if max > 0 {
		if self, err := Self(); err == nil {
			// Start the next queued reviewer however this one ends.
			command = "(" + command + "); " + shellQuote(self) + " review --next"
		}
	}
	logPath := filepath.Join(dir(s), id+".log")
	log, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer log.Close()
	cmd := shell(command)
	cmd.Dir = s.CodeRoot
	p, _ := policy.Load(s.Dir) // a broken policy still gets a review
	cmd.Stdin = strings.NewReader(Prompt(id, p.Verify))
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(env(os.Environ()),
		"SHERPA_ACTOR=agent:reviewer",
		"SHERPA_SESSION="+Session(id, time.Now()),
		"SHERPA_REVIEW="+id,
	)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	_ = os.WriteFile(filepath.Join(dir(s), id+".pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	return logPath, cmd.Process.Release()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// env drops the variables that would make the reviewer look like the author:
// its actor and session are set explicitly, and an agent CLI launched from
// inside another agent's shell may refuse to start as a nested session.
func env(in []string) []string {
	var out []string
	for _, kv := range in {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "SHERPA_ACTOR", "SHERPA_SESSION", "SHERPA_REVIEW", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT":
			continue
		}
		out = append(out, kv)
	}
	return out
}
