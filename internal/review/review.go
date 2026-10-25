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
	"strconv"
	"strings"
	"time"

	"github.com/RL-Charles/sherpa/internal/store"
)

// Prompt is what the reviewer agent is asked to do.
func Prompt(id string) string {
	return strings.ReplaceAll(`You are an independent reviewer for Sherpa ticket {id}. Another agent did
the work; you did not. Do not edit code or tickets beyond the review itself.

1. shp brief {id} for the compact intent, deliverables, next check and code map;
   shp show {id} for the full brief and the submit summary;
   shp activity -i {id} for what was done and which files changed.
2. Read the changed code and the diff. Run the verification the submit
   claims (tests, build), and anything else you need to be convinced.
3. Check every ticked check against its evidence. Checks marked ✋ are
   manual: a human verifies those, so leave them alone.
4. Finish with exactly one command:
   shp review {id} --pass -m "what you checked and found"
   shp review {id} --reject -m "what is wrong and what to fix"
   Pass only if the work does what the ticket asks and the checks hold up.
   Mention anything a human should look at by hand in your notes.
`, "{id}", id)
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

// Spawn starts command (run by the shell) in the code checkout with the
// review prompt on stdin, detached from the caller. It returns the log path.
func Spawn(s *store.Store, command, id string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", errors.New("no reviewer command configured (reviewer.command in .sherpa/config.yaml)")
	}
	if Running(s, id) {
		return "", fmt.Errorf("a reviewer for %s is already running", id)
	}
	if err := os.MkdirAll(dir(s), 0o755); err != nil {
		return "", err
	}
	logPath := filepath.Join(dir(s), id+".log")
	log, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer log.Close()
	cmd := shell(command)
	cmd.Dir = s.CodeRoot
	cmd.Stdin = strings.NewReader(Prompt(id))
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
