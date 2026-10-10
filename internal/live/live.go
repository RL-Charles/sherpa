// Package live tracks which agents are working right now, so the TUI can
// animate them. Each checkout (main or worktree) has one heartbeat file:
//
//	prompt, tool call, shp start/log ──▶ Touch  (.sherpa/.local/working/<checkout>.json)
//	stop hook, shp submit            ──▶ Clear
//
// A heartbeat older than Stale is ignored: agents interrupted with Ctrl-C
// never run their stop hook.
package live

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RL-Charles/sherpa/internal/store"
)

const Stale = 2 * time.Minute

type Pulse struct {
	Agent  string    `json:"agent"`
	Ticket string    `json:"ticket,omitempty"`
	TS     time.Time `json:"ts"`
}

func dir(sherpaDir string) string { return filepath.Join(sherpaDir, ".local", "working") }

func path(s *store.Store) string { return filepath.Join(dir(s.Dir), s.CheckoutKey()+".json") }

// Touch records that agent is working in this checkout, on ticket if known.
// An empty ticket keeps the one already recorded.
func Touch(s *store.Store, agent, ticket string) error {
	p := Pulse{Agent: agent, Ticket: ticket, TS: time.Now().UTC()}
	if ticket == "" {
		if prev, ok := read(path(s)); ok && prev.Agent == agent {
			p.Ticket = prev.Ticket
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir(s.Dir), 0o755); err != nil {
		return err
	}
	tmp := path(s) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(s))
}

// Clear records that the agent in this checkout has stopped.
func Clear(s *store.Store) error {
	if err := os.Remove(path(s)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func read(p string) (Pulse, bool) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Pulse{}, false
	}
	var out Pulse
	return out, json.Unmarshal(data, &out) == nil
}

// Read returns every heartbeat fresher than Stale at now, oldest first.
func Read(sherpaDir string, now time.Time) []Pulse {
	entries, err := os.ReadDir(dir(sherpaDir))
	if err != nil {
		return nil
	}
	var out []Pulse
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if p, ok := read(filepath.Join(dir(sherpaDir), e.Name())); ok && now.Sub(p.TS) < Stale {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}
